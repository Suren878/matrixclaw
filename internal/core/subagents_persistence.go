package core

import (
	"context"
)

type subagentTaskMutator func(*SubagentTask)

func (c *Core) createSubagentTaskRecord(ctx context.Context, task SubagentTask) error {
	if err := c.store.CreateSubagentTask(ctx, task); err != nil {
		return err
	}
	c.publishSubagentTaskUpdated(task)
	return nil
}

func (c *Core) updateSubagentTaskRecord(ctx context.Context, task SubagentTask) error {
	if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
		return err
	}
	c.publishSubagentTaskUpdated(task)
	return nil
}

func (c *Core) updateSubagentTaskRecordWith(ctx context.Context, task SubagentTask, mutate subagentTaskMutator) (SubagentTask, error) {
	if mutate != nil {
		mutate(&task)
	}
	return task, c.updateSubagentTaskRecord(ctx, task)
}

func (c *Core) markSubagentTaskRunning(ctx context.Context, task SubagentTask) (SubagentTask, error) {
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		task.Status = TaskStatusRunning
		task.UpdatedAt = c.now().UTC()
	})
}

func (c *Core) markSubagentTaskWaitingApproval(ctx context.Context, task SubagentTask) (SubagentTask, error) {
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		task.Status = TaskStatusWaitingApproval
		task.UpdatedAt = c.now().UTC()
	})
}

// finishSubagentTaskRecord ends the task; without queueCompletion its parent
// is not told it finished, as it already knows.
func (c *Core) finishSubagentTaskRecord(ctx context.Context, task SubagentTask, status TaskStatus, summary string, errText string, queueCompletion bool) (SubagentTask, error) {
	now := c.now().UTC()
	if !queueCompletion {
		if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, "", now); err != nil {
			return SubagentTask{}, err
		}
		task.DeliveredAt = &now
	}
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		task.Status = status
		task.Summary = summary
		task.Error = errText
		task.UpdatedAt = now
		finishedAt := now
		task.FinishedAt = &finishedAt
	})
}

func (c *Core) publishSubagentTaskUpdated(task SubagentTask) {
	c.publishEvent(Event{
		Type:      EventSubagentUpdated,
		SessionID: task.ParentSessionID,
		RunID:     task.ParentRunID,
		Payload:   task,
	})
}
