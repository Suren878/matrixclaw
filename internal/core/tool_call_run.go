package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func (c *Core) executeToolWithGrant(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput) (tools.Result, error) {
	result, execErr := c.executePreparedTool(ctx, prepared, input.Approved, input.Args, input.Client, input.ExternalKey)
	if result.Approval != nil && !input.Approved {
		autoApproved, err := c.autoApprovesTool(ctx, prepared, result)
		if err != nil {
			return tools.Result{}, err
		}
		if !autoApproved {
			return result, execErr
		}
		result, execErr = c.executePreparedTool(ctx, prepared, true, input.Args, input.Client, input.ExternalKey)
	}
	if execErr != nil || result.Approval != nil {
		return result, execErr
	}
	return c.keepLargeOutput(prepared.SessionID, result), nil
}

func (c *Core) executePreparedTool(ctx context.Context, prepared preparedToolCall, approved bool, args []byte, client string, externalKey string) (tools.Result, error) {
	return c.tools.Execute(ctx, prepared.ToolName, tools.Call{
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallID:  prepared.ToolCallID,
		Client:      client,
		ExternalKey: externalKey,
		WorkingDir:  prepared.WorkingDir,
		Approved:    approved,
		Args:        args,
	})
}
