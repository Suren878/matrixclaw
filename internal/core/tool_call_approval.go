package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// createPendingApproval records the approval a call outside an active engine
// asked for and marks its run's checkpoint as waiting for it.
func (c *Core) createPendingApproval(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, result tools.Result, execErr error) (tools.Result, *Approval, bool, error) {
	if result.Approval == nil || input.Approved {
		return result, nil, false, execErr
	}
	approval, err := c.requestApproval(ctx, prepared, *result.Approval)
	if err != nil {
		return tools.Result{}, nil, false, err
	}
	if err := c.saveRunCheckpoint(ctx, prepared.RunID, RunCheckpointPhaseWaitingApproval, prepared.ToolCallID, prepared.ToolName); err != nil {
		return tools.Result{}, nil, false, err
	}
	return result, &approval, true, execErr
}

// requestApproval stores a pending approval for the call and announces it; it
// leaves the run's checkpoint to the engine.
func (c *Core) requestApproval(ctx context.Context, prepared preparedToolCall, request tools.ApprovalRequest) (Approval, error) {
	paramsRaw, err := marshalJSONRaw(request.Params)
	if err != nil {
		return Approval{}, err
	}
	approval := Approval{
		ID:          c.newID("approval"),
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallRef: prepared.ToolCallID,
		ToolName:    prepared.ToolName,
		Description: request.Description,
		Action:      request.Action,
		Params:      paramsRaw,
		Path:        request.Path,
		Suggestion:  request.Suggestion,
		State:       ApprovalStatePending,
		RequestedAt: c.now().UTC(),
	}
	if err := c.store.CreateApproval(ctx, approval); err != nil {
		return Approval{}, err
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
			Suggestion:  approval.Suggestion,
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
	return approval, nil
}
