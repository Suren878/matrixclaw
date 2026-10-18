package controlplane

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/automation"
)

// tasksPicker lists the bound session's background tasks, then the scheduled tasks.
func (d *Dispatcher) tasksPicker(ctx context.Context) (Result, error) {
	items, err := d.backgroundTaskItems(ctx)
	if err != nil {
		return Result{}, err
	}
	jobs, err := d.daemon.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	active, closed := splitAutomationJobs(jobs)
	for _, job := range active {
		items = append(items, PickerItem{
			ID:      "open:" + job.ID,
			Title:   taskListTitle(job),
			Info:    taskListInfo(job),
			Command: tasksMenu.command(job.ID),
		})
	}
	archiveInfo := "Completed tasks"
	if len(closed) > 0 {
		archiveInfo = fmt.Sprintf("%d completed", len(closed))
	}
	items = append(items, PickerItem{ID: "archive", Title: "Archive", Info: archiveInfo, Command: tasksArchive.command()})
	return Result{
		Screen: NewPickerData(PickerTasks, "Tasks").Command(tasksRoot.command()).Items(items...).Build(),
	}, nil
}

func (d *Dispatcher) tasksArchivePicker(ctx context.Context) (Result, error) {
	jobs, err := d.daemon.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	_, closed := splitAutomationJobs(jobs)
	items := make([]PickerItem, 0, len(closed)+3)
	for _, job := range closed {
		items = append(items, PickerItem{
			ID:      "closed:" + job.ID,
			Title:   taskListTitle(job),
			Info:    string(job.Status),
			Command: tasksMenu.command(job.ID),
		})
	}
	if len(closed) > 0 {
		items = append(items, PickerItem{ID: "delete_closed", Title: "Delete Completed", Command: tasksDeleteClosed.command(), Role: PickerItemRoleDanger})
	}
	return Result{
		Screen: NewPickerData(PickerTaskArchive, "Task Archive").Command(tasksArchive.command()).Back(tasksRoot.command()).Items(items...).Build(),
	}, nil
}

func (d *Dispatcher) taskActionsPicker(ctx context.Context, jobID string) (Result, error) {
	jobs, err := d.daemon.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	job, ok := findAutomationJob(jobs, jobID)
	if !ok {
		return textResult("Task not found."), nil
	}
	items := []PickerItem{}
	if job.Status == automation.JobStatusActive {
		items = append(items,
			PickerItem{ID: "run", Title: "Run", Info: nextAutomationLabel(job), Command: tasksRun.command(job.ID)},
			PickerItem{ID: "archive", Title: "Done", Command: tasksComplete.command(job.ID)},
			PickerItem{ID: "delete", Title: "Delete", Command: tasksDelete.command(job.ID), Role: PickerItemRoleDanger},
		)
	} else {
		items = append(items,
			PickerItem{ID: "delete", Title: "Delete", Info: string(job.Status), Command: tasksDelete.command(job.ID), Role: PickerItemRoleDanger},
		)
	}
	return Result{
		Screen: NewPickerData(PickerTaskActions, taskListTitle(job)).
			Back(tasksRoot.command()).
			Items(items...).
			Build(),
	}, nil
}

func (d *Dispatcher) deleteClosedTasks(ctx context.Context) (Result, error) {
	jobs, err := d.daemon.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	_, closed := splitAutomationJobs(jobs)
	deleted := 0
	for _, job := range closed {
		if _, err := d.daemon.DeleteAutomationJob(ctx, job.ID); err != nil {
			return Result{}, err
		}
		deleted++
	}
	return textResult(fmt.Sprintf("Deleted closed tasks: %d", deleted)), nil
}
