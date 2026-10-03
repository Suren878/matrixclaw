package core

import (
	"context"
	"errors"
	"sync"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// nativeTurn is what the per-step prompt and tool list of a native run depend
// on; Continues lists the runs this one continues, latest first.
type nativeTurn struct {
	RunID              string
	Continues          []string
	SessionID          string
	WorkingDir         string
	Subagent           bool
	Readonly           bool
	ClientCapabilities ClientCapabilities
	ToolUse            bool
}

// coreTools is the Tools port of one native run. authorized holds the verdict
// Authorize reached for a call until its Execute takes it; it ends with the run.
type coreTools struct {
	c          *Core
	turn       nativeTurn
	authorized *sync.Map
}

func (t coreTools) Specs(ctx context.Context) []tools.Spec {
	specs := t.c.nativeToolSpecs(t.turn)
	for i := range specs {
		if specs[i].ID == agentToolName {
			specs[i].Description = t.c.agentToolDescription(ctx, specs[i].Description)
		}
	}
	return specs
}

// Authorize rejects invalid calls and those a deny rule blocks; Execute acts on
// the verdict it kept for the call. A mutating call is a barrier unless a rule or
// the mode allows it, as it then cannot ask; a child writing the parent's
// directory still can. A blocking child's time is its own.
func (t coreTools) Authorize(ctx context.Context, name string, call tools.Call) (agent.Decision, error) {
	// A call ID owned by another session fails the run with a clear error instead of
	// a primary-key conflict on the first journal write.
	if _, err := t.c.isNewToolCallMessage(ctx, call.SessionID, call.ToolCallID); err != nil {
		return agent.Decision{}, err
	}
	session, spec, err := t.c.checkToolCall(ctx, call.SessionID, name)
	if errors.Is(err, ErrInvalidInput) {
		return agent.Decision{Reason: err.Error()}, nil
	}
	if err != nil {
		return agent.Decision{}, err
	}
	if call.WorkingDir = normalizeWorkingDir(call.WorkingDir); call.WorkingDir == "" {
		call.WorkingDir = session.WorkingDir
	}
	t.authorized.Delete(call.ToolCallID)
	check, err := t.c.checkPermission(ctx, call.SessionID, spec, call)
	if err != nil {
		return agent.Decision{}, err
	}
	if check.verdict.Effect == permission.Deny {
		return agent.Decision{Reason: blockedResult(check.verdict.Rule).Content}, nil
	}
	if call.ToolCallID != "" {
		t.authorized.Store(call.ToolCallID, check)
	}
	decision := agent.Decision{Allowed: true, Barrier: spec.Mutates() && check.verdict.Effect != permission.Allow, Key: t.c.tools.ConcurrencyKey(spec.ID, call)}
	if spec.ID == agentToolName {
		input := parseAgentInput(call.Args)
		decision.Barrier = agentCallWritesSharedDir(input)
		decision.Delegated = !input.Background
	}
	return decision, nil
}

func (t coreTools) Execute(ctx context.Context, name string, call tools.Call) (tools.Result, error) {
	session, spec, err := t.c.checkToolCall(ctx, call.SessionID, name)
	if err != nil {
		return tools.Result{}, err
	}
	workingDir := normalizeWorkingDir(call.WorkingDir)
	if workingDir == "" {
		workingDir = session.WorkingDir
	}
	prepared := preparedToolCall{SessionID: call.SessionID, RunID: call.RunID, ToolName: name, Spec: spec, ToolCallID: call.ToolCallID, WorkingDir: workingDir}
	input := ExecuteToolInput{Client: call.Client, ExternalKey: call.ExternalKey, Approved: call.Approved, Args: call.Args}
	var check *callPermission
	if kept, ok := t.authorized.LoadAndDelete(call.ToolCallID); ok {
		authorized := kept.(callPermission)
		check = &authorized
	}
	result, execErr := t.c.executeToolWithGrant(ctx, prepared, input, check)
	if result.Approval != nil && !call.Approved {
		return result, nil
	}
	if execErr != nil {
		result = t.c.toolFailure(call.SessionID, execErr)
	}
	return result, nil
}

func (t coreTools) Finish(ctx context.Context, name string, call tools.Call, result tools.Result, message transcript.Message) error {
	return t.c.recordSubagentResultMessage(ctx, result.Metadata, message.ID)
}

// nativeToolSpecs lists the tools a native run may see; nil without a registry.
func (c *Core) nativeToolSpecs(turn nativeTurn) []tools.Spec {
	if c.tools == nil {
		return nil
	}
	specs := c.tools.List()
	out := make([]tools.Spec, 0, len(specs))
	for _, spec := range specs {
		if turn.Subagent && !subagentToolAllowed(spec) || turn.Readonly && spec.Mutates() {
			continue
		}
		if spec.ID == "text_to_speech" && !clientSupportsVoiceDelivery(turn.ClientCapabilities) {
			continue
		}
		if spec.ID == "send_file" && !clientSupportsDocumentDelivery(turn.ClientCapabilities) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func clientSupportsVoiceDelivery(capabilities ClientCapabilities) bool {
	return capabilities.SupportsVoiceDelivery
}

func clientSupportsDocumentDelivery(capabilities ClientCapabilities) bool {
	return capabilities.SupportsDocumentDelivery
}
