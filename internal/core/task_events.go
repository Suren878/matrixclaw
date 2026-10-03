package core

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/shelltask"
)

// taskEventTail is how much of a finished command's output its event shows.
const taskEventTail = 2000

// taskEvents lists the session's background tasks that finished unseen, in the
// order they finished.
func (c *Core) taskEvents(ctx context.Context, sessionID string) ([]Task, error) {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Undelivered: true})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(tasks, func(a, b Task) int { return a.FinishedAt.Compare(*b.FinishedAt) })
	return tasks, nil
}

// taskEventText tells the model how a background task ended.
func taskEventText(task Task) string {
	if task.Kind == TaskKindSubagent {
		return fmt.Sprintf("Subagent %s (%s) finished: %s.\nGoal: %s\nResult: %s", cmp.Or(task.AgentName, task.ID), task.ID, task.Status, task.Command, cmp.Or(task.Summary, task.Error, "none"))
	}
	var head string
	switch task.Status {
	case TaskStatusLost:
		head = fmt.Sprintf("Background task %s was lost: %s.", task.ID, task.Error)
	case TaskStatusCanceled:
		head = fmt.Sprintf("Background task %s was stopped: %s.", task.ID, task.Error)
	default:
		head = fmt.Sprintf("Background task %s finished with exit code %d", task.ID, exitCodeOf(task))
		if task.Error != "" {
			head += " (" + task.Error + ")"
		}
		head += "."
	}
	lines := []string{head, "Command: " + task.Command}
	if tail, err := shelltask.Tail(task.OutputPath, taskEventTail); err == nil && strings.TrimSpace(tail) != "" {
		lines = append(lines, "Last output:", strings.TrimRight(tail, "\n"))
	}
	lines = append(lines, "Its whole output is in "+task.OutputPath+"; task_output reads what you have not seen.")
	return strings.Join(lines, "\n")
}

func exitCodeOf(task Task) int {
	if task.ExitCode == nil {
		return -1
	}
	return *task.ExitCode
}

// runningTasksPrompt lists the session's background tasks that still run, for
// the context note; "" when none does.
func (c *Core) runningTasksPrompt(ctx context.Context, sessionID string) string {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Statuses: []TaskStatus{TaskStatusPending, TaskStatusRunning, TaskStatusWaitingApproval}, Background: true})
	if err != nil {
		return ""
	}
	lines := []string{"Background tasks still running (task_output reads them, task_kill stops them):"}
	for i := len(tasks) - 1; i >= 0; i-- {
		task := tasks[i]
		label := task.ID
		if task.Kind == TaskKindSubagent {
			label += " (subagent " + cmp.Or(task.AgentName, task.Description) + ")"
		}
		lines = append(lines, "- "+label+": "+truncateForTitle(task.Command, 200))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}
