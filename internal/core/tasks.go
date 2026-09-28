package core

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/shelltask"
)

// maxListedTasks bounds how many of a session's tasks ListSessionTasks returns.
const maxListedTasks = 30

// taskOutputTail is how much output TaskDetail shows.
const taskOutputTail = 3000

// ListSessionTasks lists the session's background tasks, newest first.
func (c *Core) ListSessionTasks(ctx context.Context, sessionID string) ([]Task, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return nil, err
	}
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: session.ID, Background: true, Limit: maxListedTasks})
	if tasks == nil && err == nil {
		tasks = []Task{}
	}
	return tasks, err
}

// TaskDetail returns a task and the end of its output.
func (c *Core) TaskDetail(ctx context.Context, taskID string) (TaskDetailResponse, error) {
	task, err := c.store.GetTask(ctx, normalizeText(taskID))
	if err != nil {
		return TaskDetailResponse{}, err
	}
	detail := TaskDetailResponse{Task: task}
	if task.OutputPath != "" {
		if tail, err := shelltask.Tail(task.OutputPath, taskOutputTail); err == nil {
			detail.OutputTail = tail
		}
	}
	return detail, nil
}

// CancelTask stops a task for the user; its session reads that at its next step.
func (c *Core) CancelTask(ctx context.Context, taskID string) (Task, error) {
	task, err := c.store.GetTask(ctx, normalizeText(taskID))
	if err != nil {
		return Task{}, err
	}
	if !task.Background {
		return Task{}, fmt.Errorf("%w: task %s is not a background task", ErrInvalidInput, task.ID)
	}
	return c.cancelTask(ctx, task, "stopped by the user")
}
