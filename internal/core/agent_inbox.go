package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// coreInbox is the Inbox port of one native run.
type coreInbox struct {
	c       *Core
	session Session
}

func (in coreInbox) Peek(ctx context.Context, runID string, kind agent.InputKind) ([]agent.Input, error) {
	switch kind {
	case agent.InputSteer:
		return in.steers(ctx, runID)
	case agent.InputDecided:
		return in.decided(ctx, runID)
	case agent.InputEvent:
		return in.events(ctx)
	default:
		return nil, fmt.Errorf("core: unknown inbox input %q", kind)
	}
}

func (in coreInbox) Finished(ctx context.Context, taskIDs []string) (bool, error) {
	return in.c.anyTaskFinished(ctx, taskIDs)
}

func (in coreInbox) Canceled(ctx context.Context, runID string) (bool, error) {
	return in.c.isRunCanceled(ctx, runID)
}

// steers lists the run's pending steer input in arrival order.
func (in coreInbox) steers(ctx context.Context, runID string) ([]agent.Input, error) {
	inputs, err := in.c.store.ListPendingSteerInputs(ctx, in.session.ID, runID)
	if err != nil {
		return nil, err
	}
	var out []agent.Input
	for _, input := range inputs {
		if text := normalizeText(input.Text); text != "" {
			out = append(out, agent.Input{Kind: agent.InputSteer, ID: input.ID, Text: text})
		}
	}
	return out, nil
}

// Consume marks the run's pending steers and the events with the given IDs
// consumed by it.
func (in coreInbox) Consume(ctx context.Context, runID string, ids []string) error {
	if err := in.c.store.MarkTasksDelivered(ctx, ids, runID, in.c.now().UTC()); err != nil {
		return err
	}
	inputs, err := in.c.store.ListPendingSteerInputs(ctx, in.session.ID, runID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if !slices.Contains(ids, input.ID) {
			continue
		}
		consumedAt := in.c.now().UTC()
		input.Status = SessionInputStatusConsumed
		input.ConsumedRunID = runID
		input.ConsumedAt = &consumedAt
		input.UpdatedAt = consumedAt
		if err := in.c.store.UpdateSessionInput(ctx, input); err != nil {
			return err
		}
		in.c.publishSessionInputUpdated(input)
	}
	return nil
}

// decided returns the run's decided approvals whose call has no result yet, oldest
// first; a call's newest approval decides it. A bridged call resumes, whichever way
// its child's approval was decided, only once the subagent is done.
func (in coreInbox) decided(ctx context.Context, runID string) ([]agent.Input, error) {
	approvals, err := in.c.store.ListApprovals(ctx, in.session.ID, "")
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var out []agent.Input
	for _, approval := range approvalsForRun(approvals, runID) {
		callID := strings.TrimSpace(approval.ToolCallRef)
		if callID == "" {
			continue
		}
		if _, ok := seen[callID]; ok {
			continue
		}
		seen[callID] = struct{}{}
		if approval.State == ApprovalStatePending {
			continue
		}
		done, err := in.c.store.HasToolResult(ctx, in.session.ID, callID)
		if err != nil {
			return nil, err
		}
		if done {
			continue
		}
		waiting, err := in.c.bridgedCallWaitsForSubagent(ctx, approval)
		if err != nil {
			return nil, err
		}
		if waiting {
			continue
		}
		toolCall, err := in.c.sessionToolCallMessage(ctx, in.session.ID, callID)
		if err != nil {
			return nil, err
		}
		args, found := toolCallArgs(toolCall)
		if !found {
			return nil, fmt.Errorf("%w: tool call %s", ErrNotFound, toolCall.ID)
		}
		var spec tools.Spec
		if in.c.tools != nil {
			spec, _ = in.c.tools.Spec(approval.ToolName)
		}
		_, bridged := decodeSubagentApprovalBridge(approval)
		out = append(out, agent.Input{
			Kind:       agent.InputDecided,
			ToolCallID: callID,
			ToolName:   approval.ToolName,
			WorkingDir: workingDirForApprovalResume(in.session.WorkingDir, spec, approval.Path),
			Args:       args,
			Denied:     approval.State == ApprovalStateRejected && !bridged,
			Reason:     approval.Reason,
		})
	}
	slices.Reverse(out)
	return out, nil
}

// coreApprovals is the Approvals port of one native run.
type coreApprovals struct {
	c         *Core
	sessionID string
}

func (a coreApprovals) Request(ctx context.Context, p agent.Pending) error {
	prepared := preparedToolCall{SessionID: p.SessionID, RunID: p.RunID, ToolName: p.ToolName, ToolCallID: p.ToolCallID}
	_, err := a.c.requestApproval(ctx, prepared, p.Request)
	return err
}

func (a coreApprovals) Pending(ctx context.Context, runID string) (bool, error) {
	return a.c.runHasPendingApprovals(ctx, a.sessionID, runID)
}

// events are the session's task events.
func (in coreInbox) events(ctx context.Context) ([]agent.Input, error) {
	tasks, err := in.c.taskEvents(ctx, in.session.ID)
	if err != nil {
		return nil, err
	}
	out := make([]agent.Input, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, agent.Input{Kind: agent.InputEvent, ID: task.ID, Text: taskEventText(task)})
	}
	return out, nil
}
