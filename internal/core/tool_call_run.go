package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// runToolCall runs a call as its permission says (check, or evaluated now when
// nil): a deny rule blocks it; a call that waits for approval returns the
// tool's preview as its request instead of a result; any other call runs, and
// may recheck the rules for subjects it reaches later.
func (c *Core) runToolCall(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, check *callPermission) (tools.Result, *tools.ApprovalRequest, error) {
	call := prepared.call(input)
	if check == nil {
		evaluated, err := c.checkPermission(ctx, prepared.SessionID, prepared.Spec, call)
		if err != nil {
			return tools.Result{}, nil, err
		}
		check = &evaluated
	}
	switch {
	case check.verdict.Effect == permission.Deny:
		return blockedResult(check.verdict.Rule), nil, nil
	case check.ask:
		request, refused := c.tools.Preview(ctx, prepared.ToolName, call)
		if refused != nil {
			return *refused, nil, nil
		}
		if check.secret {
			request.Description += "\nThis path may hold secrets."
		}
		if check.verdict.Effect == permission.Ask {
			request.Description += "\nAsked by rule " + check.verdict.Rule.String() + "."
		} else if suggestion, ok := permission.Suggest(check.request, check.root); ok {
			request.Suggestion = &suggestion
		}
		return tools.Result{}, &request, nil
	}
	result, err := c.tools.Execute(ctx, prepared.ToolName, check.guard(call))
	if err != nil {
		return result, nil, err
	}
	return c.keepLargeOutput(prepared.SessionID, result), nil, nil
}

func (prepared preparedToolCall) call(input ExecuteToolInput) tools.Call {
	return tools.Call{
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallID:  prepared.ToolCallID,
		Client:      input.Client,
		ExternalKey: input.ExternalKey,
		WorkingDir:  prepared.WorkingDir,
		Args:        input.Args,
	}
}
