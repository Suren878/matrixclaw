package controlplane

import "context"

var (
	tasksRoot                = verb(CommandTasks, "", 0)
	tasksAdd                 = valueVerb(CommandTasks, "add", 0)
	tasksArchive             = verb(CommandTasks, "archive", 0)
	tasksMenu                = verb(CommandTasks, "menu", 1)
	tasksPause               = verb(CommandTasks, "pause", 1)
	tasksResume              = verb(CommandTasks, "resume", 1)
	tasksComplete            = verb(CommandTasks, "complete", 1)
	tasksDelete              = verb(CommandTasks, "delete", 1)
	tasksDeleteConfirm       = verb(CommandTasks, "delete-confirm", 1)
	tasksDeleteClosed        = verb(CommandTasks, "delete-closed", 0)
	tasksDeleteClosedConfirm = verb(CommandTasks, "delete-closed-confirm", 0)
	tasksRun                 = verb(CommandTasks, "run", 1)
	tasksBackground          = verb(CommandTasks, "bg", 1)
	tasksBackgroundOutput    = verb(CommandTasks, "bg-output", 1)
	tasksBackgroundStop      = verb(CommandTasks, "bg-stop", 1)
	tasksBackgroundStopNow   = verb(CommandTasks, "bg-stop-confirm", 1)
)

const tasksUsage = "Usage:\n/tasks\n/tasks bg <id>\n/tasks add once YYYY-MM-DD HH:MM -- prompt\n/tasks add cron \"0 10 1 * *\" -- prompt\n/tasks complete <id>\n/tasks delete <id>\n/tasks run <id>"

func (d *Dispatcher) handleTasks(ctx context.Context, r Route) (Result, error) {
	jobID := r.arg(0)
	switch r.Verb {
	case tasksRoot:
		if r.Value != "" {
			return textResult(tasksUsage), nil
		}
		return d.tasksPicker(ctx)
	case tasksAdd:
		return d.handleTaskAdd(ctx, r.Value)
	case tasksArchive:
		return d.tasksArchivePicker(ctx)
	case tasksDeleteClosed:
		return Result{Screen: deleteConfirmData("Delete completed tasks?", tasksDeleteClosedConfirm.command(), tasksArchive.command())}, nil
	case tasksDeleteClosedConfirm:
		return d.deleteClosedTasks(ctx)
	}
	if jobID == "" {
		return textResult(tasksUsage), nil
	}
	switch r.Verb {
	case tasksBackground, tasksBackgroundOutput, tasksBackgroundStop, tasksBackgroundStopNow:
		return d.handleBackgroundTask(ctx, r.Verb, jobID)
	case tasksMenu:
		return d.taskActionsPicker(ctx, jobID)
	case tasksPause:
		job, err := d.daemon.PauseAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return textResult("Paused: " + formatAutomationJob(job)), nil
	case tasksResume:
		job, err := d.daemon.ResumeAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return textResult("Resumed: " + formatAutomationJob(job)), nil
	case tasksComplete:
		job, err := d.daemon.CompleteAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return textResult("Archived: " + formatAutomationJob(job)), nil
	case tasksDelete:
		return Result{Screen: deleteConfirmData("Delete task?", tasksDeleteConfirm.command(jobID), tasksMenu.command(jobID))}, nil
	case tasksDeleteConfirm:
		job, err := d.daemon.DeleteAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return textResult("Deleted: " + formatAutomationJob(job)), nil
	case tasksRun:
		fire, err := d.daemon.RunAutomationJobNow(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return textResult("Task started: " + fire.RunID), nil
	}
	return textResult(tasksUsage), nil
}
