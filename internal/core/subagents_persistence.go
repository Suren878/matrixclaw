package core

import (
	"context"
	"time"
)

type subagentTaskMutator func(*SubagentTask)

func (c *Core) createSubagentTaskRecord(ctx context.Context, task SubagentTask) error {
	if err := c.store.CreateSubagentTask(ctx, task); err != nil {
		return err
	}
	c.saveSubagentWorkJob(ctx, task)
	c.publishSubagentTaskUpdated(task)
	return nil
}

func (c *Core) updateSubagentTaskRecord(ctx context.Context, task SubagentTask) error {
	if err := c.store.UpdateSubagentTask(ctx, task); err != nil {
		return err
	}
	c.saveSubagentWorkJob(ctx, task)
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
		task.Status = SubagentTaskStatusRunning
		task.UpdatedAt = c.now().UTC()
	})
}

func (c *Core) markSubagentTaskWaitingApproval(ctx context.Context, task SubagentTask) (SubagentTask, error) {
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		task.Status = SubagentTaskStatusWaitingApproval
		task.UpdatedAt = c.now().UTC()
	})
}

func (c *Core) touchSubagentTaskRecord(ctx context.Context, task SubagentTask, at time.Time) (SubagentTask, error) {
	if at.IsZero() {
		at = c.now().UTC()
	}
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		task.UpdatedAt = at.UTC()
	})
}

func (c *Core) finishSubagentTaskRecord(ctx context.Context, task SubagentTask, status SubagentTaskStatus, summary string, errText string, queueCompletion bool) (SubagentTask, error) {
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		now := c.now().UTC()
		task.Status = status
		task.Summary = summary
		task.Error = errText
		task.UpdatedAt = now
		finishedAt := now
		task.FinishedAt = &finishedAt
		if queueCompletion && task.CompletionQueuedAt == nil {
			queuedAt := now
			task.CompletionQueuedAt = &queuedAt
		}
	})
}

func (c *Core) queueSubagentCompletionRecord(ctx context.Context, task SubagentTask) (SubagentTask, error) {
	if task.CompletionQueuedAt != nil {
		return task, nil
	}
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		now := c.now().UTC()
		task.UpdatedAt = now
		queuedAt := now
		task.CompletionQueuedAt = &queuedAt
	})
}

func (c *Core) markSubagentCompletionDelivered(ctx context.Context, task SubagentTask, at time.Time, runID string) (SubagentTask, error) {
	if at.IsZero() {
		at = c.now().UTC()
	}
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		deliveredAt := at.UTC()
		task.CompletionDeliveredAt = &deliveredAt
		task.CompletionAutoResumeRunID = runID
		task.UpdatedAt = deliveredAt
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
