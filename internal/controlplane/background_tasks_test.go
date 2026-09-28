package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

type backgroundTaskRuntime struct {
	tokenReportRuntime
	tasks    []core.Task
	canceled []string
}

func (r *backgroundTaskRuntime) SessionTasks(_ context.Context, sessionID string) ([]core.Task, error) {
	var out []core.Task
	for _, task := range r.tasks {
		if task.SessionID == sessionID {
			out = append(out, task)
		}
	}
	return out, nil
}

func (r *backgroundTaskRuntime) TaskDetail(_ context.Context, taskID string) (core.TaskDetailResponse, error) {
	for _, task := range r.tasks {
		if task.ID == taskID {
			return core.TaskDetailResponse{Task: task, OutputTail: "listening on :3000\n"}, nil
		}
	}
	return core.TaskDetailResponse{}, core.ErrNotFound
}

func (r *backgroundTaskRuntime) CancelTask(_ context.Context, taskID string) (core.Task, error) {
	r.canceled = append(r.canceled, taskID)
	return core.Task{ID: taskID, Kind: core.TaskKindShell, Command: "npm run dev", Status: core.TaskStatusCanceled}, nil
}

func newBackgroundTaskRuntime() *backgroundTaskRuntime {
	code := 1
	finished := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	return &backgroundTaskRuntime{tasks: []core.Task{
		{ID: "task_dev", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true},
		{ID: "task_test", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusFailed, Command: "go test ./...", ExitCode: &code, Background: true, FinishedAt: &finished},
		{ID: "task_other", SessionID: "s2", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true},
	}}
}

func TestTasksListsTheSessionsBackgroundTasks(t *testing.T) {
	result, err := New(newBackgroundTaskRuntime(), "").Handle(context.Background(), "key", "/tasks")

	if err != nil || result.Picker == nil || len(result.Picker.Items) != 2 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	first, second := result.Picker.Items[0], result.Picker.Items[1]
	if first.Title != "npm run dev" || first.Info != "running" || first.Command != "/tasks bg task_dev" || second.Info != "failed, exit 1" {
		t.Fatalf("items = %+v", result.Picker.Items)
	}
}

func TestBackgroundTaskShowsOutputAndStopsAfterConfirmation(t *testing.T) {
	runtime := newBackgroundTaskRuntime()
	dispatcher := New(runtime, "")

	actions, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_dev")
	if err != nil || actions.Picker == nil || len(actions.Picker.Items) != 2 || actions.Picker.Items[1].Command != "/tasks bg task_dev stop" {
		t.Fatalf("actions = %+v, %v", actions, err)
	}
	output, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_dev output")
	if err != nil || output.Info == nil || output.Info.Text != "listening on :3000" {
		t.Fatalf("output = %+v, %v", output, err)
	}
	asked, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_dev stop")
	if err != nil || asked.Confirm == nil || len(runtime.canceled) != 0 {
		t.Fatalf("asked = %+v, %v", asked, err)
	}
	stopped, err := dispatcher.Handle(context.Background(), "key", asked.Confirm.ConfirmCommand)
	if err != nil || !strings.Contains(stopped.Text, "canceled") || len(runtime.canceled) != 1 || runtime.canceled[0] != "task_dev" {
		t.Fatalf("stopped = %+v, %v, canceled %v", stopped, err, runtime.canceled)
	}

	finished, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_test")
	if err != nil || finished.Picker == nil || len(finished.Picker.Items) != 1 {
		t.Fatalf("a finished task offers %+v, %v", finished, err)
	}
	other, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_other stop confirm")
	if err != nil || other.Text != "Task not found." || len(runtime.canceled) != 1 {
		t.Fatalf("another session's task = %+v, %v", other, err)
	}
}
