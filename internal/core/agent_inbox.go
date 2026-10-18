package core

import (
	"context"
	"slices"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// coreInbox is the Inbox port of one native run.
type coreInbox struct {
	c       *Core
	session Session
	runID   string
}

// Steers lists the run's pending steer input in arrival order.
func (in coreInbox) Steers(ctx context.Context) ([]agent.Steer, error) {
	inputs, err := in.c.store.ListPendingSteerInputs(ctx, in.session.ID, in.runID)
	if err != nil {
		return nil, err
	}
	var out []agent.Steer
	for _, input := range inputs {
		images := slices.DeleteFunc(slices.Clone(input.Parts), func(part transcript.MessagePart) bool { return part.Kind != transcript.MessagePartKindImage })
		if input.Text != "" || len(images) > 0 {
			out = append(out, agent.Steer{ID: input.ID, Text: input.Text, Images: images})
		}
	}
	return out, nil
}

// ConsumeSteers marks the run's pending steers with the given IDs consumed by it.
func (in coreInbox) ConsumeSteers(ctx context.Context, ids []string) error {
	inputs, err := in.c.store.ListPendingSteerInputs(ctx, in.session.ID, in.runID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if !slices.Contains(ids, input.ID) {
			continue
		}
		consumedAt := in.c.now().UTC()
		input.Status = SessionInputStatusConsumed
		input.ConsumedRunID = in.runID
		input.ConsumedAt = &consumedAt
		input.UpdatedAt = consumedAt
		if err := in.c.store.UpdateSessionInput(ctx, input); err != nil {
			return err
		}
		in.c.publishSessionInputUpdated(input)
	}
	return nil
}

// Events are the session's task events.
func (in coreInbox) Events(ctx context.Context) ([]agent.TaskEvent, error) {
	tasks, err := in.c.taskEvents(ctx, in.session.ID)
	if err != nil {
		return nil, err
	}
	out := make([]agent.TaskEvent, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, agent.TaskEvent{TaskID: task.ID, Text: taskEventText(task)})
	}
	return out, nil
}

// ConsumeEvents marks the tasks' ends delivered to the run.
func (in coreInbox) ConsumeEvents(ctx context.Context, taskIDs []string) error {
	return in.c.store.MarkTasksDelivered(ctx, taskIDs, in.runID, in.c.now().UTC())
}

// Decisions lists where the newest approval of each of the run's calls
// stands, oldest first.
func (in coreInbox) Decisions(ctx context.Context) ([]agent.Decision, error) {
	approvals, err := in.c.store.ListRunApprovals(ctx, in.session.ID, in.runID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []agent.Decision
	for _, approval := range approvals {
		callID := approval.ToolCallRef
		if callID == "" || seen[callID] {
			continue
		}
		seen[callID] = true
		out = append(out, agent.Decision{
			ToolCallID: callID,
			Pending:    approval.State == ApprovalStatePending,
			Denied:     approval.State == ApprovalStateRejected,
			Reason:     approval.Reason,
		})
	}
	slices.Reverse(out)
	return out, nil
}

func (in coreInbox) Finished(ctx context.Context, taskIDs []string) (bool, error) {
	return in.c.anyTaskFinished(ctx, taskIDs)
}

// coreApprovals is the Approvals port of one native run.
type coreApprovals struct {
	c *Core
}

func (a coreApprovals) Request(ctx context.Context, p agent.Pending) error {
	prepared := preparedToolCall{SessionID: p.SessionID, RunID: p.RunID, ToolName: p.ToolName, ToolCallID: p.ToolCallID}
	_, err := a.c.requestApproval(ctx, prepared, p.Request)
	return err
}
