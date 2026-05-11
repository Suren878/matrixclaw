package controlplane

import (
	"context"
	"strings"
)

func (d *Dispatcher) handleTasks(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.automation == nil {
		return unsupportedRuntime("tasks"), nil
	}
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return d.tasksPicker(ctx)
	}
	switch strings.ToLower(fields[0]) {
	case "add":
		return d.handleTaskAdd(ctx, externalKey, strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
	case "archive":
		return d.tasksArchivePicker(ctx)
	case "menu":
		if len(fields) < 2 {
			return d.tasksPicker(ctx)
		}
		return d.taskActionsPicker(ctx, fields[1])
	case "pause":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /tasks pause <id>"}, nil
		}
		job, err := d.automation.PauseAutomationJob(ctx, fields[1])
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Paused: " + formatAutomationJob(job)}, nil
	case "resume":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /tasks resume <id>"}, nil
		}
		job, err := d.automation.ResumeAutomationJob(ctx, fields[1])
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Resumed: " + formatAutomationJob(job)}, nil
	case "complete":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /tasks complete <id>"}, nil
		}
		job, err := d.automation.CompleteAutomationJob(ctx, fields[1])
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Archived: " + formatAutomationJob(job)}, nil
	case "delete":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /tasks delete <id>"}, nil
		}
		return Result{
			Handled: true,
			Confirm: deleteConfirmData("Delete task?", "/tasks delete-confirm "+fields[1], "/tasks menu "+fields[1]),
		}, nil
	case "delete-confirm":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /tasks delete-confirm <id>"}, nil
		}
		job, err := d.automation.DeleteAutomationJob(ctx, fields[1])
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Deleted: " + formatAutomationJob(job)}, nil
	case "delete-closed":
		return Result{
			Handled: true,
			Confirm: deleteConfirmData("Delete completed tasks?", "/tasks delete-closed-confirm", "/tasks archive"),
		}, nil
	case "delete-closed-confirm":
		return d.deleteClosedTasks(ctx)
	case "run":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /tasks run <id>"}, nil
		}
		fire, err := d.automation.RunAutomationJobNow(ctx, fields[1])
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Task started: " + fire.RunID}, nil
	default:
		return Result{Handled: true, Text: "Usage:\n/tasks\n/tasks add once 2026-04-26 16:00 -- prompt\n/tasks add cron \"0 10 1 * *\" -- prompt\n/tasks complete <id>\n/tasks delete <id>\n/tasks run <id>"}, nil
	}
}
