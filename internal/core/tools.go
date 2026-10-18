package core

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// ExecuteTool runs one tool call outside the agent loop.
func (c *Core) ExecuteTool(ctx context.Context, input ExecuteToolInput) (ExecuteToolResult, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.ToolName = strings.TrimSpace(input.ToolName)
	input.ToolCallID = strings.TrimSpace(input.ToolCallID)
	prepared, scope, err := c.prepareToolCall(ctx, input)
	if err != nil {
		return ExecuteToolResult{}, err
	}

	var toolResult tools.Result
	var ask *tools.ApprovalRequest
	check, execErr := c.permitCall(ctx, scope, prepared.ToolName, prepared.Spec, prepared.call(input))
	if execErr == nil {
		toolResult, ask, execErr = c.runToolCall(ctx, prepared, input, check)
	}
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
