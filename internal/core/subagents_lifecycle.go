package core

import (
	"context"
	"errors"
	"sort"
	"time"
)

const subagentActivityHeartbeatInterval = 5 * time.Second

func (c *Core) recordSubagentResultMessage(ctx context.Context, metadata any, resultMessageID string) error {
	resultMessageID = normalizeText(resultMessageID)
	if resultMessageID == "" {
		return nil
	}
	task, ok := metadata.(SubagentTask)
	if !ok {
		if taskPtr, ptrOK := metadata.(*SubagentTask); ptrOK && taskPtr != nil {
			task = *taskPtr
			ok = true
		}
	}
	if !ok || normalizeText(task.ID) == "" || task.Mode != SubagentTaskModeAsync {
		return nil
	}
	current, err := c.store.GetSubagentTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if current.ResultMessageID == resultMessageID {
		return nil
	}
	_, err = c.updateSubagentTaskRecordWith(ctx, current, func(task *SubagentTask) {
		task.ResultMessageID = resultMessageID
		task.UpdatedAt = c.now().UTC()
	})
	return err
}

func (c *Core) touchSubagentTaskActivity(ctx context.Context, childRunID string, at time.Time) error {
	childRunID = normalizeText(childRunID)
	if childRunID == "" || c == nil || c.store == nil {
		return nil
	}
	task, err := c.store.GetSubagentTaskByChildRun(ctx, childRunID)
	if err != nil {
		return nil
	}
	if taskStatusTerminal(task.Status) {
		return nil
	}
	if at.IsZero() {
		at = c.now().UTC()
	}
	if !task.UpdatedAt.IsZero() && at.Sub(task.UpdatedAt) < subagentActivityHeartbeatInterval {
		return nil
	}
	_, err = c.touchSubagentTaskRecord(ctx, task, at)
	return err
}

func (c *Core) afterRunExecution(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	if runID == "" || c == nil || c.store == nil {
		return nil
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		if ignoreMissing(err) {
			return nil
		}
		return err
	}
	if task, err := c.store.GetSubagentTaskByChildRun(ctx, runID); err == nil {
		switch task.Mode {
		case SubagentTaskModeAsync:
			if syncErr := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		case SubagentTaskModeBlocking:
			if syncErr := c.syncBlockingSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		}
	} else if !ignoreMissing(err) {
		return err
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		if ignoreMissing(err) {
			return nil
		}
		return err
	}
	if subagentRunStatusTerminal(run.Status) {
		if err := c.queuePendingSteersForRun(ctx, session.ID, run.ID); err != nil {
			return err
		}
		startedInput, err := c.startNextPendingSessionInput(ctx, session.ID)
		if err != nil || startedInput {
			return err
		}
	}
	if !isSubagentSession(session) && subagentRunStatusTerminal(run.Status) {
		return c.deliverPendingSubagentCompletionsForParent(ctx, session.ID)
	}
	return nil
}

func ignoreMissing(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func (c *Core) syncAsyncSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	if taskStatusTerminal(task.Status) {
		return nil
	}
	switch run.Status {
	case RunStatusWaitingApproval:
		_, err := c.mirrorPendingSubagentApproval(ctx, task)
		return err
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
	default:
		return nil
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, nil)
	status := TaskStatusCompleted
	errText := ""
	if run.Status == RunStatusCanceled {
		status = TaskStatusCanceled
		errText = summary
	} else if failed {
		status = TaskStatusFailed
		errText = summary
	}
	task, err := c.finishSubagentTaskRecord(ctx, task, status, summary, errText, true)
	if err != nil {
		return err
	}
	c.publishSubagentToolUpdate(task)
	return c.deliverPendingSubagentCompletionsForParent(ctx, task.ParentSessionID)
}

func (c *Core) syncBlockingSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	switch run.Status {
	case RunStatusWaitingApproval:
		parentRunID := normalizeText(task.ParentRunID)
		if parentRunID == "" {
			return nil
		}
		parentRun, err := c.store.GetRun(ctx, parentRunID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if parentRun.Status == RunStatusRunning && c.runIsActive(parentRun.ID) {
			return nil
		}
		_, err = c.mirrorPendingSubagentApproval(ctx, task)
		return err
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
	default:
		return nil
	}
	parentRunID := normalizeText(task.ParentRunID)
	if parentRunID == "" {
		return nil
	}
	parentRun, err := c.store.GetRun(ctx, parentRunID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if subagentRunStatusTerminal(parentRun.Status) {
		return nil
	}
	if c.runIsActive(parentRun.ID) {
		// The live blocking delegate call will consume the child result when
		// ExecuteRun returns to it.
		return nil
	}
	if _, err := c.finishOrBridgeSubagentTask(ctx, task, nil); err != nil {
		return err
	}
	messages, err := c.store.ListMessages(ctx, parentRun.SessionID, 0)
	if err != nil {
		return err
	}
	for _, interrupted := range incompleteToolCallsForRun(messages, parentRun.ID) {
		if normalizeText(interrupted.Call.ID) != normalizeText(task.ParentToolCallID) {
			continue
		}
		if err := c.replayInterruptedTool(ctx, parentRun, interrupted); err != nil {
			return err
		}
		break
	}
	if err := c.setRunStatus(ctx, &parentRun, RunStatusAccepted, ""); err != nil {
		return err
	}
	return c.startRun(ctx, parentRunID)
}

func (c *Core) deliverPendingSubagentCompletionsForParent(ctx context.Context, parentSessionID string) error {
	parentSessionID = normalizeText(parentSessionID)
	if parentSessionID == "" {
		return nil
	}
	if ready, err := c.parentReadyForSubagentAutoResume(ctx, parentSessionID); err != nil || !ready {
		return err
	}
	tasks, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
		ParentSessionID: parentSessionID,
		Mode:            SubagentTaskModeAsync,
		Statuses: []TaskStatus{
			TaskStatusCompleted,
			TaskStatusFailed,
			TaskStatusCanceled,
		},
		Limit: 50,
	})
	if err != nil {
		return err
	}
	pending := make([]SubagentTask, 0, len(tasks))
	for _, task := range tasks {
		if task.DeliveredAt == nil {
			pending = append(pending, task)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	triggerID := subagentCompletionTriggerID(parentSessionID, pending)
	result, err := c.AcceptTriggeredRun(ctx, HandleTriggeredRunInput{
		TriggerID: triggerID,
		SessionID: parentSessionID,
		Text:      subagentCompletionPrompt(pending),
	})
	if err != nil {
		return err
	}
	now := c.now().UTC()
	for _, task := range pending {
		if _, err := c.markSubagentCompletionDelivered(ctx, task, now, result.Run.ID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) parentReadyForSubagentAutoResume(ctx context.Context, parentSessionID string) (bool, error) {
	pendingInputs, err := c.store.ListPendingSessionInputs(ctx, parentSessionID)
	if err != nil {
		return false, err
	}
	if len(pendingInputs) > 0 {
		return false, nil
	}
	messages, err := c.store.ListMessages(ctx, parentSessionID, 0)
	if err != nil {
		return false, err
	}
	if len(messages) == 0 {
		return true, nil
	}
	for i := len(messages) - 1; i >= 0; i-- {
		runID := normalizeText(messages[i].RunID)
		if runID == "" {
			continue
		}
		run, err := c.store.GetRun(ctx, runID)
		if errors.Is(err, ErrNotFound) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return subagentRunStatusTerminal(run.Status), nil
	}
	return true, nil
}

// publishSubagentToolUpdate tells clients the parent's spawn_subagent call finished;
// the result itself reaches the parent through the completion run.
func (c *Core) publishSubagentToolUpdate(task SubagentTask) {
	resultMessageID := normalizeText(task.ResultMessageID)
	if resultMessageID == "" {
		return
	}
	c.publishToolUpdate(task.ParentSessionID, task.ParentRunID, ToolUpdate{
		ToolCallID:      task.ParentToolCallID,
		ToolName:        spawnSubagentToolName,
		State:           subagentTaskToolLifecycleState(task),
		ResultStatus:    string(subagentTaskToolResultStatus(task)),
		RunID:           task.ParentRunID,
		SessionID:       task.ParentSessionID,
		ResultMessageID: resultMessageID,
		Error:           task.Error,
	})
}

func (c *Core) RecoverSubagentTasks(ctx context.Context) error {
	active, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
		Statuses: activeTaskStatuses(),
		Limit:    200,
	})
	if err != nil {
		return err
	}
	for _, task := range active {
		run, err := c.store.GetRun(ctx, task.ChildRunID)
		if err != nil {
			continue
		}
		switch task.Mode {
		case SubagentTaskModeAsync:
			if subagentRunStatusTerminal(run.Status) || run.Status == RunStatusWaitingApproval {
				if err := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); err != nil {
					return err
				}
				continue
			}
			if err := c.startRun(ctx, task.ChildRunID); err != nil {
				return err
			}
		case SubagentTaskModeBlocking:
			if subagentRunStatusTerminal(run.Status) || run.Status == RunStatusWaitingApproval {
				if err := c.syncBlockingSubagentTaskAfterRun(ctx, task, run); err != nil {
					return err
				}
				continue
			}
			if run.Status == RunStatusAccepted {
				if err := c.startRun(ctx, task.ChildRunID); err != nil {
					return err
				}
			}
		}
	}
	pending, err := c.store.ListPendingSubagentCompletionTasks(ctx, 200)
	if err != nil {
		return err
	}
	seenParents := map[string]struct{}{}
	for _, task := range pending {
		if _, ok := seenParents[task.ParentSessionID]; ok {
			continue
		}
		seenParents[task.ParentSessionID] = struct{}{}
		if err := c.deliverPendingSubagentCompletionsForParent(ctx, task.ParentSessionID); err != nil {
			return err
		}
	}
	return nil
}

func activeTaskStatuses() []TaskStatus {
	return []TaskStatus{
		TaskStatusPending,
		TaskStatusRunning,
		TaskStatusWaitingApproval,
	}
}
