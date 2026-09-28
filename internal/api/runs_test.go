package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRunStepsEndpointListsStepsInOrder(t *testing.T) {
	server, st := newAPITestServer(t)
	ctx := context.Background()
	run := core.Run{ID: "r1", SessionID: "s1", UserMessageID: "u1", Status: core.RunStatusCompleted, StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}
	if err := st.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"tool_use", "end_turn"} {
		if err := st.SaveRunStep(ctx, core.RunStep{RunID: "r1", StopReason: reason, PromptTokens: 10, CreatedAt: apiTestEpoch}); err != nil {
			t.Fatal(err)
		}
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/runs/r1/steps", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response core.RunStepsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Steps) != 2 || response.Steps[0].Step != 1 || response.Steps[0].StopReason != "tool_use" || response.Steps[1].Step != 2 {
		t.Fatalf("steps=%+v", response.Steps)
	}

	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/runs/r1", nil))
	var runResponse core.RunResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &runResponse); err != nil || runResponse.Run.ID != "r1" {
		t.Fatalf("run lookup broke: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRunProgressCountsBudgetStepsAndLiveBackgroundTasks(t *testing.T) {
	server, st := newAPITestServer(t)
	ctx := context.Background()
	run := core.Run{ID: "r1", SessionID: "s1", UserMessageID: "u1", Status: core.RunStatusRunning, StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}
	if err := st.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"tool_use", "compact", "tool_use"} {
		if err := st.SaveRunStep(ctx, core.RunStep{RunID: "r1", StopReason: reason, CreatedAt: apiTestEpoch}); err != nil {
			t.Fatal(err)
		}
	}
	for id, status := range map[string]core.TaskStatus{"t1": core.TaskStatusRunning, "t2": core.TaskStatusPending, "t3": core.TaskStatusCompleted} {
		task := core.Task{ID: id, SessionID: "s1", RunID: "r1", Kind: core.TaskKindShell, Status: status, Command: "sleep 9", Background: true, StartedAt: apiTestEpoch}
		if err := st.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	steps := 7
	if err := st.SaveSessionBudget(ctx, "s1", core.SessionBudget{Steps: &steps}, apiTestEpoch); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/runs/r1/progress", nil))
	var response core.RunProgressResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if want := (core.RunProgress{Steps: 2, StepLimit: 7, Tasks: 2}); response.Progress != want {
		t.Fatalf("progress = %+v, want %+v", response.Progress, want)
	}
}
