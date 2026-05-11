package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func (c *Core) ExecuteTool(ctx context.Context, input ExecuteToolInput) (ExecuteToolResult, error) {
	prepared, err := c.prepareToolCall(ctx, input)
	if err != nil {
		return ExecuteToolResult{}, err
	}

	toolResult, execErr := c.executeToolWithGrant(ctx, prepared, input)
	toolResult, execErr, approval, pending := c.createPendingApproval(ctx, prepared, input, toolResult, execErr)
	if pending {
		return ExecuteToolResult{
			ToolCallMessage: prepared.Message,
			Approval:        approval,
		}, nil
	}

	finalResult := toolResult
	if execErr != nil {
		finalResult = tools.Result{
			Content: execErr.Error(),
			IsError: true,
		}
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
