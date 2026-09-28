package core

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// nativeTurn is what the per-step prompt and tool list of a native run depend on.
type nativeTurn struct {
	RunID              string
	SessionID          string
	WorkingDir         string
	Subagent           bool
	ClientCapabilities ClientCapabilities
	ToolUse            bool
}

// coreTools is the Tools port of one native run.
type coreTools struct {
	c    *Core
	turn nativeTurn
}

func (t coreTools) Specs(ctx context.Context) []tools.Spec {
	specs := t.c.nativeToolSpecs(t.turn)
	for i := range specs {
		if specs[i].ID == delegateTaskToolName || specs[i].ID == spawnSubagentToolName {
			specs[i].Description = t.c.delegateTaskToolDescription(ctx, specs[i].Description)
		}
	}
	return specs
}

// Authorize rejects invalid calls and those a deny rule blocks; the rest of the
// permission check runs with the call in Execute.
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
	check, err := t.c.checkPermission(ctx, call.SessionID, spec, call)
	if err != nil {
		return agent.Decision{}, err
	}
	if check.verdict.Effect == permission.Deny {
		return agent.Decision{Reason: blockedResult(check.verdict.Rule).Content}, nil
	}
	return agent.Decision{Allowed: true, Barrier: spec.Mutates()}, nil
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
	result, execErr := t.c.executeToolWithGrant(ctx, prepared, input)
	if result.Approval != nil && !call.Approved {
		return result, nil
	}
	if execErr != nil {
		result = t.c.toolFailure(call.SessionID, execErr)
	}
	return result, nil
}

func (t coreTools) Finish(ctx context.Context, name string, call tools.Call, result tools.Result, message transcript.Message) error {
	prepared := preparedToolCall{SessionID: call.SessionID, RunID: call.RunID, ToolName: name, ToolCallID: call.ToolCallID}
	if err := t.c.saveFileVersionSnapshot(ctx, prepared, result, message.CreatedAt); err != nil {
		return err
	}
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
		if turn.Subagent && !subagentToolAllowed(spec) {
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
