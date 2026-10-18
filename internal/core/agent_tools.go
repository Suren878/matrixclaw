package core

import (
	"context"
	"errors"
	"sync"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
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
	// Model names the provider and model the run talks to.
	Model string
}

// coreTools is the Tools port of one native run. authorized holds the verdict
// Authorize reached for a call until its Execute takes it; it ends with the run.
type coreTools struct {
	c          *Core
	turn       nativeTurn
	authorized *sync.Map
	batch      *batchSnapshot
}

func (t coreTools) Specs(ctx context.Context) []tools.Spec {
	specs := t.c.nativeToolSpecs(t.turn)
	for i := range specs {
		if specs[i].ID == agentToolName {
			specs[i] = t.c.agentToolSpec(ctx, specs[i])
		}
	}
	return specs
}

// Authorize rejects invalid calls and those a deny rule blocks; Execute acts on
// the verdict it kept for the call. A mutating call that waits for approval is
// a barrier, and so is a child writing the parent's directory. A blocking
// child's time is its own. The batch's first call that may change files takes
// its file checkpoint before any call of the batch starts.
func (t coreTools) Authorize(ctx context.Context, name string, call tools.Call) (agent.Decision, error) {
	// A call ID owned by another session fails the run with a clear error instead of
	// a primary-key conflict on the first journal write.
	if _, err := t.c.isNewToolCallMessage(ctx, call.SessionID, call.ToolCallID); err != nil {
		return agent.Decision{}, err
	}
	session, spec, err := t.c.checkToolCall(ctx, name, call)
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
	check, err := t.c.permitCall(ctx, name, spec, call)
	if err != nil {
		return agent.Decision{}, err
	}
	if refusal, refused := check.refusal(); refused {
		return agent.Decision{Reason: refusal}, nil
	}
	if call.ToolCallID != "" {
		t.authorized.Store(call.ToolCallID, check)
	}
	call = check.input(call)
	t.checkpointBatch(ctx, spec, call)
	decision := agent.Decision{Allowed: true, Barrier: spec.Mutates() && check.ask, Key: t.c.tools.ConcurrencyKey(spec.ID, call)}
	if spec.ID == agentToolName {
		input := parseAgentInput(call.Args)
		decision.Barrier = agentCallWritesSharedDir(input)
		decision.Delegated = !input.Background
	}
	return decision, nil
}

func (t coreTools) Execute(ctx context.Context, name string, call tools.Call) (tools.Result, *tools.ApprovalRequest, error) {
	session, spec, err := t.c.checkToolCall(ctx, name, call)
	if err != nil {
		return tools.Result{}, nil, err
	}
	workingDir := normalizeWorkingDir(call.WorkingDir)
	if workingDir == "" {
		workingDir = session.WorkingDir
	}
	prepared := preparedToolCall{SessionID: call.SessionID, RunID: call.RunID, ToolName: name, Spec: spec, ToolCallID: call.ToolCallID, WorkingDir: workingDir}
	input := ExecuteToolInput{Client: call.Client, ExternalKey: call.ExternalKey, Args: call.Args}
	var check *callPermission
	if kept, ok := t.authorized.LoadAndDelete(call.ToolCallID); ok {
		authorized := kept.(callPermission)
		check = &authorized
	}
	result, ask, execErr := t.c.runToolCall(ctx, prepared, input, check)
	if execErr != nil {
		result = t.c.toolFailure(call.SessionID, execErr)
	}
	return result, ask, nil
}

// nativeToolSpecs lists the tools a native run may see; nil without a registry.
func (c *Core) nativeToolSpecs(turn nativeTurn) []tools.Spec {
	if c.tools == nil {
		return nil
	}
	specs := c.tools.List()
	out := make([]tools.Spec, 0, len(specs))
	for _, spec := range specs {
		if turn.Subagent && !subagentToolAllowed(spec) {
			continue
		}
		if turn.Readonly && spec.Mutates() {
			readOnly, ok := tools.ReadOnlySpec(spec)
			if !ok {
				continue
			}
			spec = readOnly
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
