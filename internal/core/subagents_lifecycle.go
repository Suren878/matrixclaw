package core

import (
	"context"
	"errors"
	"sort"
	"time"
)

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

func (c *Core) touchAsyncSubagentTaskActivity(ctx context.Context, childRunID string, at time.Time) error {
	childRunID = normalizeText(childRunID)
	if childRunID == "" || c == nil || c.store == nil {
		return nil
	}
	task, err := c.store.GetSubagentTaskByChildRun(ctx, childRunID)
	if err != nil {
		return nil
	}
	if task.Mode != SubagentTaskModeAsync || subagentTaskTerminalStatus(task.Status) {
		return nil
	}
	if at.IsZero() {
		at = c.now().UTC()
	}
	if !task.UpdatedAt.IsZero() && at.Sub(task.UpdatedAt) < time.Second {
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
	switch run.Status {
	case RunStatusWaitingApproval:
		return c.mirrorPendingSubagentApproval(ctx, task)
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
	default:
		return nil
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, nil)
	status := SubagentTaskStatusCompleted
	errText := ""
	if run.Status == RunStatusCanceled {
		status = SubagentTaskStatusCanceled
		errText = summary
	} else if failed {
		status = SubagentTaskStatusFailed
		errText = summary
	}
	task, err := c.finishSubagentTaskRecord(ctx, task, status, summary, errText, true)
	if err != nil {
		return err
	}
	if err := c.updateSubagentResultMessage(ctx, task); err != nil {
		return err
	}
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
		if parentRun.Status == RunStatusRunning {
			return nil
		}
		return c.mirrorPendingSubagentApproval(ctx, task)
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
		Statuses: []SubagentTaskStatus{
			SubagentTaskStatusCompleted,
			SubagentTaskStatusFailed,
			SubagentTaskStatusCanceled,
		},
		Limit: 50,
	})
	if err != nil {
		return err
	}
	pending := make([]SubagentTask, 0, len(tasks))
	for _, task := range tasks {
		if task.CompletionQueuedAt != nil && task.CompletionDeliveredAt == nil {
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

func (c *Core) updateSubagentResultMessage(ctx context.Context, task SubagentTask) error {
	resultMessageID := normalizeText(task.ResultMessageID)
	if resultMessageID == "" {
		return nil
	}
	messages, err := c.store.ListMessages(ctx, task.ParentSessionID, 0)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.ID != resultMessageID {
			continue
		}
		content := subagentFinishedResultContent(task)
		metadataRaw, err := marshalJSONRaw(task)
		if err != nil {
			return err
		}
		message.Content = normalizeToolContent(content)
		message.UpdatedAt = c.now().UTC()
		for i := range message.Parts {
			if message.Parts[i].ToolResult == nil {
				continue
			}
			message.Parts[i].ToolResult.Content = content
			message.Parts[i].ToolResult.Metadata = metadataRaw
			message.Parts[i].ToolResult.Status = string(subagentTaskToolResultStatus(task))
			message.Parts[i].ToolResult.IsError = subagentTaskFailed(task)
		}
		if err := c.store.UpdateMessage(ctx, message); err != nil {
			return err
		}
		c.publishEvent(Event{
			Type:      EventMessageUpdated,
			SessionID: message.SessionID,
			RunID:     message.RunID,
			Payload:   message,
		})
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
		return nil
	}
	return nil
}

func (c *Core) RecoverSubagentTasks(ctx context.Context) error {
	active, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
		Statuses: activeSubagentTaskStatuses(),
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

func activeSubagentTaskStatuses() []SubagentTaskStatus {
	return []SubagentTaskStatus{
		SubagentTaskStatusPending,
		SubagentTaskStatusRunning,
		SubagentTaskStatusWaitingApproval,
	}
}
