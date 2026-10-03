package controlplane

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func backgroundTaskDaemon(t *testing.T) (*fakeDaemon, *[]string) {
	code := 1
	finished := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	tasks := []core.Task{
		{ID: "task_dev", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true},
		{ID: "task_test", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusFailed, Command: "go test ./...", ExitCode: &code, Background: true, FinishedAt: &finished},
		{ID: "task_other", SessionID: "s2", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true},
	}
	var canceled []string
	daemon := newFakeDaemon(t).
		on("GET /v1/sessions/{id}/tasks", func(r *http.Request) any {
			var out []core.Task
			for _, task := range tasks {
				if task.SessionID == r.PathValue("id") {
					out = append(out, task)
				}
			}
			return core.SessionTasksResponse{Tasks: out}
		}).
		on("GET /v1/tasks/{id}", func(r *http.Request) any {
			index := slices.IndexFunc(tasks, func(task core.Task) bool { return task.ID == r.PathValue("id") })
			if index < 0 {
				return core.ErrNotFound
			}
			return core.TaskDetailResponse{Task: tasks[index], OutputTail: "listening on :3000\n"}
		}).
		on("POST /v1/tasks/{id}/cancel", func(r *http.Request) any {
			canceled = append(canceled, r.PathValue("id"))
			return core.TaskResponse{Task: core.Task{ID: r.PathValue("id"), Kind: core.TaskKindShell, Command: "npm run dev", Status: core.TaskStatusCanceled}}
		}).
		on("GET /v1/automation/jobs", func(*http.Request) any { return map[string]any{"jobs": []any{}} })
	return daemon, &canceled
}

func TestTasksListsTheSessionsBackgroundTasks(t *testing.T) {
	daemon, _ := backgroundTaskDaemon(t)

	result := daemon.run("/tasks")

	if result.Picker == nil || len(result.Picker.Items) != 3 {
		t.Fatalf("result = %+v", result)
	}
	first, second := result.Picker.Items[0], result.Picker.Items[1]
	if first.Title != "npm run dev" || first.Info != "running" || first.Command != "/tasks bg task_dev" || second.Info != "failed, exit 1" || result.Picker.Items[2].ID != "archive" {
		t.Fatalf("items = %+v", result.Picker.Items)
	}
}

func TestBackgroundTaskShowsOutputAndStopsAfterConfirmation(t *testing.T) {
	daemon, canceled := backgroundTaskDaemon(t)

	actions := daemon.run("/tasks bg task_dev")
	if actions.Picker == nil || len(actions.Picker.Items) != 2 || actions.Picker.Items[1].Command != "/tasks bg task_dev stop" {
		t.Fatalf("actions = %+v", actions)
	}
	if output := daemon.run("/tasks bg task_dev output"); output.Info == nil || output.Info.Text != "listening on :3000" {
		t.Fatalf("output = %+v", output)
	}
	asked := daemon.run("/tasks bg task_dev stop")
	if asked.Confirm == nil || len(*canceled) != 0 {
		t.Fatalf("asked = %+v", asked)
	}
	if stopped := daemon.run(asked.Confirm.ConfirmCommand); !strings.Contains(stopped.Text, "canceled") || len(*canceled) != 1 || (*canceled)[0] != "task_dev" {
		t.Fatalf("stopped = %+v, canceled %v", stopped, *canceled)
	}

	if finished := daemon.run("/tasks bg task_test"); finished.Picker == nil || len(finished.Picker.Items) != 1 {
		t.Fatalf("a finished task offers %+v", finished)
	}
	if other := daemon.run("/tasks bg task_other stop confirm"); other.Text != "Task not found." || len(*canceled) != 1 {
		t.Fatalf("another session's task = %+v", other)
	}
}
