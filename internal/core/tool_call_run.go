package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func (c *Core) executeToolWithGrant(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput) (tools.Result, error) {
	result, execErr := c.executePreparedTool(ctx, prepared, input.Approved, input.Args)
	if result.Approval == nil || input.Approved {
		return result, execErr
	}
	autoApproved, err := c.autoApprovesTool(ctx, prepared, result)
	if err != nil {
		return tools.Result{}, err
	}
	if autoApproved {
		return c.executePreparedTool(ctx, prepared, true, input.Args)
	}
	return result, execErr
}

func (c *Core) executePreparedTool(ctx context.Context, prepared preparedToolCall, approved bool, args []byte) (tools.Result, error) {
	return c.tools.Execute(ctx, prepared.ToolName, tools.Call{
		SessionID:  prepared.SessionID,
		RunID:      prepared.RunID,
		ToolCallID: prepared.ToolCallID,
		WorkingDir: prepared.WorkingDir,
		Approved:   approved,
		Args:       args,
	})
}
