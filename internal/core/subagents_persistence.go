package core

import (
	"context"
	"errors"
)

// findTask is the one task the filter selects; ErrNotFound when none does.
func (c *Core) findTask(ctx context.Context, filter TaskFilter) (Task, error) {
	filter.Limit = 1
	tasks, err := c.store.ListTasks(ctx, filter)
	if err != nil {
		return Task{}, err
	}
	if len(tasks) == 0 {
		return Task{}, ErrNotFound
	}
	return tasks[0], nil
}

// subagentTaskOfCall is the subagent task a parent's call started.
func (c *Core) subagentTaskOfCall(ctx context.Context, sessionID string, runID string, callID string) (Task, error) {
	return c.findTask(ctx, TaskFilter{SessionID: sessionID, RunID: runID, ParentToolCallID: callID, Kind: TaskKindSubagent})
}

func (c *Core) createSubagentTaskRecord(ctx context.Context, task Task) error {
	if err := c.store.CreateTask(ctx, task); err != nil {
		return err
	}
	c.publishTaskUpdated(task)
	return nil
}

// setSubagentTaskStatus moves a subagent task that has not ended to status.
func (c *Core) setSubagentTaskStatus(ctx context.Context, task Task, status TaskStatus) (Task, error) {
	if err := c.store.SetTaskStatus(ctx, task.ID, status, c.now().UTC()); err != nil {
		return Task{}, err
	}
	task, err := c.store.GetTask(ctx, task.ID)
	if err != nil {
		return Task{}, err
	}
	c.publishTaskUpdated(task)
	return task, nil
}

// finishSubagentTaskRecord ends the task unless it ended already and returns
// it as stored; without queueCompletion its parent is not told it finished, as
// it already knows.
func (c *Core) finishSubagentTaskRecord(ctx context.Context, task Task, status TaskStatus, summary string, errText string, queueCompletion bool) (Task, error) {
	ended, err := c.store.FinishTask(ctx, task.ID, TaskEnd{Status: status, Summary: summary, Error: errText, Delivered: !queueCompletion, At: c.now().UTC()})
	if err != nil {
		return Task{}, err
	}
	stored, err := c.store.GetTask(ctx, task.ID)
	if errors.Is(err, ErrNotFound) && !ended {
		return task, nil
	}
	if err != nil {
		return Task{}, err
	}
	if ended {
		c.publishTaskUpdated(stored)
	}
	return stored, nil
}
