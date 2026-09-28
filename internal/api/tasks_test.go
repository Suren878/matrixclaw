package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestTaskEndpointsListShowAndCancel(t *testing.T) {
	server, st := newAPITestServer(t)
	for _, task := range []core.Task{
		{ID: "task_bg", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true, StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch},
		{ID: "task_inline", SessionID: "s1", Kind: core.TaskKindSubagent, Status: core.TaskStatusRunning, Command: "Look", StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch},
	} {
		if err := st.CreateTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	serve := func(method, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder
	}

	var listed core.SessionTasksResponse
	if got := serve(http.MethodGet, "/v1/sessions/s1/tasks"); got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &listed) != nil || len(listed.Tasks) != 1 || listed.Tasks[0].ID != "task_bg" {
		t.Fatalf("list status=%d body=%s", got.Code, got.Body.String())
	}
	var detail core.TaskDetailResponse
	if got := serve(http.MethodGet, "/v1/tasks/task_bg"); got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &detail) != nil || detail.Task.Command != "npm run dev" {
		t.Fatalf("detail status=%d body=%s", got.Code, got.Body.String())
	}
	var canceled core.TaskResponse
	if got := serve(http.MethodPost, "/v1/tasks/task_bg/cancel"); got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &canceled) != nil || canceled.Task.Status != core.TaskStatusCanceled {
		t.Fatalf("cancel status=%d body=%s", got.Code, got.Body.String())
	}
	for path, want := range map[string]int{"/v1/tasks/task_gone": http.StatusNotFound, "/v1/sessions/gone/tasks": http.StatusNotFound} {
		if got := serve(http.MethodGet, path); got.Code != want {
			t.Errorf("GET %s status=%d, want %d", path, got.Code, want)
		}
	}
	if got := serve(http.MethodGet, "/v1/tasks/task_bg/cancel"); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET cancel status=%d", got.Code)
	}
}
