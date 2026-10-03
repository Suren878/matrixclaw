package core

import "context"

func (c *Core) ExecuteTool(ctx context.Context, input ExecuteToolInput) (ExecuteToolResult, error) {
	prepared, err := c.prepareToolCall(ctx, input)
	if err != nil {
		return ExecuteToolResult{}, err
	}

	toolResult, ask, execErr := c.runToolCall(ctx, prepared, input, nil)
	if ask != nil {
		approval, err := c.requestApproval(ctx, prepared, *ask)
		if err != nil {
			return ExecuteToolResult{}, err
		}
		return ExecuteToolResult{ToolCallMessage: prepared.Message, Approval: &approval}, nil
	}

	finalResult := toolResult
	if execErr != nil {
		finalResult = c.toolFailure(prepared.SessionID, execErr)
	}

	toolCallMessage, resultMessage, err := c.finishToolCall(ctx, prepared, input, finalResult)
	if err != nil {
		return ExecuteToolResult{}, err
	}
	if execErr != nil {
		return ExecuteToolResult{
			ToolCallMessage:   toolCallMessage,
			ToolResultMessage: resultMessage,
		}, execErr
	}
	return ExecuteToolResult{
		ToolCallMessage:   toolCallMessage,
		ToolResultMessage: resultMessage,
	}, nil
}
