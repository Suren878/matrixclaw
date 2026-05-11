package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func (c *Core) createPendingApproval(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, result tools.Result, execErr error) (tools.Result, error, *Approval, bool) {
	if result.Approval == nil || input.Approved {
		return result, execErr, nil, false
	}
	paramsRaw, err := marshalJSONRaw(result.Approval.Params)
	if err != nil {
		return tools.Result{}, err, nil, false
	}
	approval := Approval{
		ID:          c.newID("approval"),
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallRef: prepared.ToolCallID,
		ToolName:    prepared.ToolName,
		Description: result.Approval.Description,
		Action:      result.Approval.Action,
		Params:      paramsRaw,
		Path:        result.Approval.Path,
		State:       ApprovalStatePending,
		RequestedAt: c.now().UTC(),
	}
	if err := c.store.CreateApproval(ctx, approval); err != nil {
		return tools.Result{}, err, nil, false
	}
	c.publishEvent(Event{
		Type:      EventApprovalRequest,
		SessionID: prepared.SessionID,
		RunID:     approval.RunID,
		Payload: PermissionRequest{
			ID:          approval.ID,
			SessionID:   approval.SessionID,
			ToolCallID:  prepared.ToolCallID,
			ToolName:    approval.ToolName,
			Description: approval.Description,
			Action:      approval.Action,
			Params:      approval.Params,
			Path:        approval.Path,
		},
	})
	c.publishToolUpdate(prepared.SessionID, approval.RunID, ToolUpdate{
		ToolCallID: prepared.ToolCallID,
		ToolName:   prepared.ToolName,
		State:      ToolLifecycleWaitingApproval,
		RunID:      approval.RunID,
		SessionID:  prepared.SessionID,
		ApprovalID: approval.ID,
	})
	return result, execErr, &approval, true
}
