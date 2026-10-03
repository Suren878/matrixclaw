package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
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
			Command: tasksCommand("bg", task.ID),
		})
	}
	return items, nil
}

// handleBackgroundTask serves /tasks bg <id> [output | stop [confirm]].
func (d *Dispatcher) handleBackgroundTask(ctx context.Context, args string) (Result, error) {
	taskID, action := firstCommandToken(args)
	if taskID == "" {
		return Result{Handled: true, Text: "Usage: /tasks bg <id> [output|stop]"}, nil
	}
	detail, err := d.daemon.TaskDetail(ctx, taskID)
	if errors.Is(err, core.ErrNotFound) {
		return Result{Handled: true, Text: "Task not found."}, nil
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
		return Result{Handled: true, Text: "Task not found."}, nil
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "":
		items := []PickerItem{{ID: "output", Title: "Output", Info: backgroundTaskStatus(task), Command: tasksCommand("bg", task.ID, "output")}}
		if !backgroundTaskFinished(task) {
			items = append(items, PickerItem{ID: "stop", Title: "Stop", Command: tasksCommand("bg", task.ID, "stop"), Role: PickerItemRoleDanger})
		}
		return Result{Handled: true, Picker: NewPickerData(PickerTaskActions, backgroundTaskTitle(task)).Back(tasksCommand()).Items(items...).Ptr()}, nil
	case "output":
		info := backgroundTaskInfo(detail)
		return Result{Handled: true, Info: &info}, nil
	case "stop":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Stop " + backgroundTaskTitle(task) + "?",
			ConfirmLabel:   "Stop",
			CancelLabel:    "Close",
			ConfirmCommand: tasksCommand("bg", task.ID, "stop", "confirm"),
			CancelCommand:  tasksCommand("bg", task.ID),
			ConfirmDanger:  true,
		}}, nil
	case "stop confirm":
		stopped, err := d.daemon.CancelTask(ctx, task.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: fmt.Sprintf("%s: %s.", backgroundTaskTitle(stopped), backgroundTaskStatus(stopped))}, nil
	default:
		return Result{Handled: true, Text: "Usage: /tasks bg <id> [output|stop]"}, nil
	}
}

func backgroundTaskTitle(task core.Task) string {
	if task.Kind == core.TaskKindSubagent {
		return "Subagent " + firstNonEmptyTrimmed(task.AgentName, task.ID)
	}
	return truncateTaskText(firstNonEmptyTrimmed(task.Description, task.Command), 48)
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
		text = firstNonEmptyTrimmed(task.Summary, task.Error)
	}
	if text == "" {
		text = "No output yet."
	}
	return InfoData{Title: backgroundTaskTitle(task), Text: text, Rows: rows, CloseCommand: tasksCommand("bg", task.ID)}
}
