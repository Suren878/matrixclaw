package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// requestApproval stores a pending approval for the call and announces it; it
// leaves the run's checkpoint to the engine. A subagent's approval is announced
// to its parent's session and chat; a read-only subagent's is refused at once.
func (c *Core) requestApproval(ctx context.Context, prepared preparedToolCall, request tools.ApprovalRequest) (Approval, error) {
	paramsRaw, err := marshalJSONRaw(request.Params)
	if err != nil {
		return Approval{}, err
	}
	task, subagent, err := c.subagentTaskOf(ctx, prepared.SessionID)
	if err != nil {
		return Approval{}, err
	}
	approval := Approval{
		ID:          c.newID("approval"),
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		TaskID:      task.ID,
		AgentName:   task.AgentName,
		ToolCallRef: prepared.ToolCallID,
		ToolName:    prepared.ToolName,
		Description: request.Description,
		Params:      paramsRaw,
		Path:        request.Path,
		Suggestion:  request.Suggestion,
		State:       ApprovalStatePending,
		RequestedAt: c.now().UTC(),
	}
	if subagent && task.Readonly {
		approval.State, approval.DecidedAt = ApprovalStateRejected, &approval.RequestedAt
		approval.Reason = "read-only subagent cannot run " + firstNonEmpty(prepared.ToolName, "this tool")
		return approval, c.store.CreateApproval(ctx, approval)
	}
	if err := c.store.CreateApproval(ctx, approval); err != nil {
		return Approval{}, err
	}
	audience, audienceRun := approval.SessionID, approval.RunID
	if subagent {
		audience, audienceRun = task.SessionID, task.RunID
	}
	c.publishEvent(Event{
		Type:      EventApprovalRequest,
		SessionID: audience,
		RunID:     audienceRun,
		Payload: PermissionRequest{
			ID:          approval.ID,
			SessionID:   approval.SessionID,
			TaskID:      approval.TaskID,
			AgentName:   approval.AgentName,
			ToolCallID:  prepared.ToolCallID,
			ToolName:    approval.ToolName,
			Description: approval.Description,
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
	if subagent {
		return approval, c.deliverSubagentApproval(ctx, approval, task)
	}
	return approval, nil
}
