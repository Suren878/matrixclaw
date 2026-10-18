package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// runToolCall runs a call as its permission says (check, or evaluated now when
// nil): a deny rule or a hook blocks it; a call that waits for approval returns
// the tool's preview as its request instead of a result; any other call runs,
// with the arguments its hooks rewrote, and may recheck the rules for subjects
// it reaches later. What hooks and checks after it say ends its result.
func (c *Core) runToolCall(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, check *callPermission) (tools.Result, *tools.ApprovalRequest, error) {
	call := prepared.call(input)
	if check == nil {
		evaluated, err := c.permitCall(ctx, prepared.ToolName, prepared.Spec, call)
		if err != nil {
			return tools.Result{}, nil, err
		}
		check = &evaluated
	}
	call = check.input(call)
	if refusal, refused := check.refusal(); refused {
		return tools.Result{Content: refusal, Status: tools.ResultStatusError}, nil, nil
	}
	if check.ask {
		request, refused := c.tools.Preview(ctx, prepared.ToolName, call)
		if refused != nil {
			return *refused, nil, nil
		}
		if check.secret {
			request.Description += "\nThis path may hold secrets."
		}
		switch {
		case check.hook.asked != "":
			request.Description += "\nAsked by hook: " + check.hook.asked
		case check.verdict.Effect == permission.Ask:
			request.Description += "\nAsked by rule " + check.verdict.Rule.String() + "."
		default:
			request.Suggestion = check.suggestion()
		}
		return tools.Result{}, &request, nil
	}
	result, err := c.tools.Execute(ctx, prepared.ToolName, check.guard(call))
	if err != nil {
		return result, nil, err
	}
	result = c.keepLargeOutput(prepared.SessionID, result)
	return c.afterToolUse(ctx, *check, call, prepared.ToolName, result), nil, nil
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
