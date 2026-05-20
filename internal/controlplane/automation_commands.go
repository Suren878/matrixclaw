package controlplane

import "context"

func (d *Dispatcher) handleTasks(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.automation == nil {
		return unsupportedRuntime("tasks"), nil
	}
	step, rest := firstCommandStep(args)
	if step == "" {
		return d.tasksPicker(ctx)
	}
	switch step {
	case "add":
		return d.handleTaskAdd(ctx, externalKey, rest)
	case "archive":
		return d.tasksArchivePicker(ctx)
	case "menu":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return d.tasksPicker(ctx)
		}
		return d.taskActionsPicker(ctx, jobID)
	case "pause":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks pause <id>"}, nil
		}
		job, err := d.automation.PauseAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Paused: " + formatAutomationJob(job)}, nil
	case "resume":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks resume <id>"}, nil
		}
		job, err := d.automation.ResumeAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Resumed: " + formatAutomationJob(job)}, nil
	case "complete":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks complete <id>"}, nil
		}
		job, err := d.automation.CompleteAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Archived: " + formatAutomationJob(job)}, nil
	case "delete":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks delete <id>"}, nil
		}
		return Result{
			Handled: true,
			Confirm: deleteConfirmData("Delete task?", taskDeleteConfirmCommand(jobID), taskMenuCommand(jobID)),
		}, nil
	case "delete-confirm":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks delete-confirm <id>"}, nil
		}
		job, err := d.automation.DeleteAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Deleted: " + formatAutomationJob(job)}, nil
	case "delete-closed":
		return Result{
			Handled: true,
			Confirm: deleteConfirmData("Delete completed tasks?", tasksDeleteClosedConfirmCommand(), tasksArchiveCommand()),
		}, nil
	case "delete-closed-confirm":
		return d.deleteClosedTasks(ctx)
	case "run":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks run <id>"}, nil
		}
		fire, err := d.automation.RunAutomationJobNow(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Task started: " + fire.RunID}, nil
	default:
		return Result{Handled: true, Text: "Usage:\n/tasks\n/tasks add once 2026-04-26 16:00 -- prompt\n/tasks add cron \"0 10 1 * *\" -- prompt\n/tasks complete <id>\n/tasks delete <id>\n/tasks run <id>"}, nil
	}
}
