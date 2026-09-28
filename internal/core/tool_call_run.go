package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// executeToolWithGrant runs a call as its permission says: a deny rule blocks
// it; a grant, an allow rule or the mode preset runs it approved; an ask rule
// requests approval; otherwise the tool's own dry run decides.
func (c *Core) executeToolWithGrant(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput) (tools.Result, error) {
	check, err := c.checkPermission(ctx, prepared.SessionID, prepared.Spec, prepared.call(input, input.Approved))
	if err != nil {
		return tools.Result{}, err
	}
	verdict := check.verdict
	var result tools.Result
	var execErr error
	switch {
	case verdict.Effect == permission.Deny:
		return blockedResult(verdict.Rule), nil
	case input.Approved || verdict.Effect == permission.Allow:
		result, execErr = c.tools.Execute(ctx, prepared.ToolName, prepared.call(input, true))
	case verdict.Effect == permission.Ask && !prepared.Spec.RequiresApproval():
		result = askedByRule(prepared, input, verdict.Rule)
	default:
		result, execErr = c.tools.Execute(ctx, prepared.ToolName, prepared.call(input, false))
	}
	if result.Approval != nil && result.Approval.Suggestion == nil && verdict.Effect != permission.Ask {
		if suggestion, ok := permission.Suggest(check.request); ok {
			result.Approval.Suggestion = &suggestion
		}
	}
	if execErr != nil || result.Approval != nil {
		return result, execErr
	}
	return c.keepLargeOutput(prepared.SessionID, result), nil
}

func (prepared preparedToolCall) call(input ExecuteToolInput, approved bool) tools.Call {
	return tools.Call{
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallID:  prepared.ToolCallID,
		Client:      input.Client,
		ExternalKey: input.ExternalKey,
		WorkingDir:  prepared.WorkingDir,
		Approved:    approved,
		Args:        input.Args,
	}
}
