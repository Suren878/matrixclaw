package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// maxPickedTasks bounds the background tasks /tasks lists.
const maxPickedTasks = 10

// backgroundTaskItems lists the bound session's background tasks for /tasks.
func (d *Dispatcher) backgroundTaskItems(ctx context.Context) ([]PickerItem, error) {
	sessionID, err := d.currentSessionID(ctx)
	if err != nil || sessionID == "" {
		return nil, err
	}
	tasks, err := d.daemon.SessionTasks(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, 0, min(len(tasks), maxPickedTasks))
	for _, task := range tasks[:min(len(tasks), maxPickedTasks)] {
		items = append(items, PickerItem{
			ID:      "bg:" + task.ID,
			Title:   backgroundTaskTitle(task),
			Info:    backgroundTaskStatus(task),
			Command: tasksBackground.command(task.ID),
		})
	}
	return items, nil
}

// handleBackgroundTask serves the screens of one background task of the
// bound session.
func (d *Dispatcher) handleBackgroundTask(ctx context.Context, action Verb, taskID string) (Result, error) {
	detail, err := d.daemon.TaskDetail(ctx, taskID)
	if errors.Is(err, core.ErrNotFound) {
		return textResult("Task not found."), nil
	}
	if err != nil {
		return Result{}, err
	}
	sessionID, err := d.currentSessionID(ctx)
	if err != nil {
		return Result{}, err
	}
	task := detail.Task
	if task.SessionID != sessionID {
		return textResult("Task not found."), nil
	}
	switch action {
	case tasksBackgroundOutput:
		return Result{Screen: backgroundTaskInfo(detail)}, nil
	case tasksBackgroundStop:
		return Result{Screen: ConfirmData{
			Message:        "Stop " + backgroundTaskTitle(task) + "?",
			ConfirmLabel:   "Stop",
			CancelLabel:    "Close",
			ConfirmCommand: tasksBackgroundStopNow.command(task.ID),
			CancelCommand:  tasksBackground.command(task.ID),
			ConfirmDanger:  true,
		}}, nil
	case tasksBackgroundStopNow:
		stopped, err := d.daemon.CancelTask(ctx, task.ID)
		if err != nil {
			return Result{}, err
		}
		return textResult(fmt.Sprintf("%s: %s.", backgroundTaskTitle(stopped), backgroundTaskStatus(stopped))), nil
	}
	items := []PickerItem{{ID: "output", Title: "Output", Info: backgroundTaskStatus(task), Command: tasksBackgroundOutput.command(task.ID)}}
	if !backgroundTaskFinished(task) {
		items = append(items, PickerItem{ID: "stop", Title: "Stop", Command: tasksBackgroundStop.command(task.ID), Role: PickerItemRoleDanger})
	}
	return Result{Screen: NewPickerData(PickerTaskActions, backgroundTaskTitle(task)).Back(tasksRoot.command()).Items(items...).Build()}, nil
}

func backgroundTaskTitle(task core.Task) string {
	if task.Kind == core.TaskKindSubagent {
		return "Subagent " + textutil.FirstNonEmpty(task.AgentName, task.ID)
	}
	return truncateTaskText(textutil.FirstNonEmpty(task.Description, task.Command), 48)
}

func backgroundTaskStatus(task core.Task) string {
	status := string(task.Status)
	if task.ExitCode != nil && *task.ExitCode != 0 {
		status += fmt.Sprintf(", exit %d", *task.ExitCode)
	}
	return status
}

func backgroundTaskFinished(task core.Task) bool {
	return task.FinishedAt != nil
}

func backgroundTaskInfo(detail core.TaskDetailResponse) InfoData {
	task := detail.Task
	rows := []InfoRow{
		{Label: "Task", Value: task.ID},
		{Label: "Status", Value: backgroundTaskStatus(task)},
		{Label: "Command", Value: task.Command},
	}
	if task.OutputPath != "" {
		rows = append(rows, InfoRow{Label: "Output file", Value: task.OutputPath})
	}
	text := strings.TrimSpace(detail.OutputTail)
	if task.Kind == core.TaskKindSubagent {
		text = textutil.FirstNonEmpty(task.Summary, task.Error)
	}
	if text == "" {
		text = "No output yet."
	}
	return InfoData{Title: backgroundTaskTitle(task), Text: text, Rows: rows, CloseCommand: tasksBackground.command(task.ID)}
}
