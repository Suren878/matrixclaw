package core

import (
	"context"
	"errors"
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

// syncSubagentTask brings the task of a subagent's run up to date with it.
func (c *Core) syncSubagentTask(ctx context.Context, run Run) error {
	task, err := c.store.GetSubagentTaskByChildRun(ctx, run.ID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if run.Status.Terminal() {
		if err := c.rejectChildApprovalCopies(ctx, task); err != nil {
			return err
		}
	}
	if task.Mode == SubagentTaskModeAsync {
		return c.syncAsyncSubagentTaskAfterRun(ctx, task, run)
	}
	return c.syncBlockingSubagentTaskAfterRun(ctx, task, run)
}

func ignoreMissing(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func (c *Core) syncAsyncSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	if task.Status.Terminal() {
		return nil
	}
	if run.Status == RunStatusWaitingApproval {
		_, err := c.mirrorPendingSubagentApproval(ctx, task)
		return err
	}
	if !run.Status.Terminal() {
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
	finished, err := c.store.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	c.taskFinished(ctx, finished)
	return nil
}

func (c *Core) syncBlockingSubagentTaskAfterRun(ctx context.Context, task SubagentTask, run Run) error {
	switch {
	case run.Status == RunStatusWaitingApproval:
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
	case !run.Status.Terminal():
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
	if parentRun.Status.Terminal() {
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
	calls, err := c.incompleteToolCalls(ctx, parentRun.SessionID, parentRun.ID)
	if err != nil {
		return err
	}
	for _, interrupted := range calls {
		if normalizeText(interrupted.Call.ID) != normalizeText(task.ParentToolCallID) {
			continue
		}
		if err := c.replayInterruptedTool(ctx, parentRun, interrupted); err != nil {
			return err
		}
		break
	}
	// A parent parked on approvals starts once none of them is pending.
	if parentRun.Status == RunStatusWaitingApproval {
		return c.resumeDecidedRun(ctx, parentRun.SessionID, parentRun.ID)
	}
	if waiting, err := c.runWaitsForBlockingChild(ctx, parentRun.SessionID, parentRun.ID); err != nil || waiting {
		return err
	}
	if err := c.transition(ctx, &parentRun, runChange{To: RunStatusAccepted}); err != nil {
		return err
	}
	return c.startRun(ctx, parentRunID)
}

// runWaitsForBlockingChild reports whether a call of the run has no result yet
// while its blocking subagent still works on it.
func (c *Core) runWaitsForBlockingChild(ctx context.Context, sessionID string, runID string) (bool, error) {
	calls, err := c.incompleteToolCalls(ctx, sessionID, runID)
	if err != nil {
		return false, err
	}
	for _, interrupted := range calls {
		task, err := c.store.GetSubagentTaskByParentToolCall(ctx, sessionID, runID, interrupted.Call.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if task.Mode != SubagentTaskModeBlocking || task.Status.Terminal() {
			continue
		}
		terminal, err := c.subagentTaskTerminal(ctx, task)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil || !terminal {
			return err == nil, err
		}
	}
	return false, nil
}

// publishSubagentToolUpdate tells clients the parent's agent call finished;
// the result itself reaches the parent through the completion run.
func (c *Core) publishSubagentToolUpdate(task SubagentTask) {
	resultMessageID := normalizeText(task.ResultMessageID)
	if resultMessageID == "" {
		return
	}
	c.publishToolUpdate(task.ParentSessionID, task.ParentRunID, ToolUpdate{
		ToolCallID:      task.ParentToolCallID,
		ToolName:        agentToolName,
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
			if run.Status.Terminal() || run.Status == RunStatusWaitingApproval {
				if err := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); err != nil {
					return err
				}
				continue
			}
			if err := c.startRun(ctx, task.ChildRunID); err != nil {
				return err
			}
		case SubagentTaskModeBlocking:
			if run.Status.Terminal() || run.Status == RunStatusWaitingApproval {
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
	return nil
}

func activeTaskStatuses() []TaskStatus {
	return []TaskStatus{
		TaskStatusPending,
		TaskStatusRunning,
		TaskStatusWaitingApproval,
	}
}
