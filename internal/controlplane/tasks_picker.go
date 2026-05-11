package controlplane

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func (d *Dispatcher) tasksPicker(ctx context.Context) (Result, error) {
	jobs, err := d.automation.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	active, closed := splitAutomationJobs(jobs)
	items := make([]PickerItem, 0, len(active)+3)
	for _, job := range active {
		items = append(items, PickerItem{
			ID:    "open:" + job.ID,
			Title: taskListTitle(job),
			Info:  taskListInfo(job),
		})
	}
	archiveTitle := "Archive"
	archiveInfo := "Completed tasks"
	if len(closed) > 0 {
		archiveTitle = "Archive"
		archiveInfo = fmt.Sprintf("%d completed", len(closed))
	}
	items = append(items,
		PickerItem{ID: "archive", Title: archiveTitle, Info: archiveInfo},
		CloseItem(),
	)
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerTasks, "Tasks").HideBack(true).Items(items...).Ptr(),
	}, nil
}

func (d *Dispatcher) tasksArchivePicker(ctx context.Context) (Result, error) {
	jobs, err := d.automation.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	_, closed := splitAutomationJobs(jobs)
	items := make([]PickerItem, 0, len(closed)+3)
	for _, job := range closed {
		items = append(items, PickerItem{
			ID:    "closed:" + job.ID,
			Title: taskListTitle(job),
			Info:  string(job.Status),
		})
	}
	if len(closed) > 0 {
		items = append(items, PickerItem{ID: "delete_closed", Title: "Delete Completed", Role: PickerItemRoleDanger})
	}
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerTaskArchive, "Task Archive").Back("/tasks").Items(items...).Ptr(),
	}, nil
}

func (d *Dispatcher) taskActionsPicker(ctx context.Context, jobID string) (Result, error) {
	jobs, err := d.automation.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	job, ok := findAutomationJob(jobs, jobID)
	if !ok {
		return Result{Handled: true, Text: "Task not found."}, nil
	}
	items := []PickerItem{}
	if job.Status == automation.JobStatusActive {
		items = append(items,
			PickerItem{ID: "run", Title: "Run", Info: nextAutomationLabel(job)},
			PickerItem{ID: "archive", Title: "Done"},
			PickerItem{ID: "delete", Title: "Delete", Role: PickerItemRoleDanger},
		)
	} else {
		items = append(items,
			PickerItem{ID: "delete", Title: "Delete", Info: string(job.Status), Role: PickerItemRoleDanger},
		)
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerTaskActions, taskListTitle(job)).
			Context(job.ID).
			Back("/tasks").
			Items(items...).
			Ptr(),
	}, nil
}

func (d *Dispatcher) deleteClosedTasks(ctx context.Context) (Result, error) {
	jobs, err := d.automation.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	_, closed := splitAutomationJobs(jobs)
	deleted := 0
	for _, job := range closed {
		if _, err := d.automation.DeleteAutomationJob(ctx, job.ID); err != nil {
			return Result{}, err
		}
		deleted++
	}
	return Result{Handled: true, Text: fmt.Sprintf("Deleted closed tasks: %d", deleted)}, nil
}
