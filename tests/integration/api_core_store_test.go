package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/core"
	goworkflows "github.com/Suren878/matrixclaw/internal/orchestration/go_workflows"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestAPICoreStoreFlow(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	app := core.New(sqliteStore).WithSessionLLMs(newSessionLLMRegistryStub(newProviderStub()))
	workflowRuntime, err := goworkflows.New(dbPath, app)
	if err != nil {
		t.Fatalf("goworkflows.New() error = %v", err)
	}
	defer workflowRuntime.Close()

	app.WithRunStarter(workflowRuntime)
	server := api.New(app)

	sessionResp := postJSON(t, server.Handler(), "/v1/sessions", map[string]any{
		"title": "Docs",
	})
	if sessionResp.Code != http.StatusCreated {
		t.Fatalf("POST /v1/sessions status = %d, want %d", sessionResp.Code, http.StatusCreated)
	}

	var created struct {
		Session core.Session `json:"session"`
	}
	decodeJSON(t, sessionResp, &created)
	if created.Session.ID == "" {
		t.Fatalf("created session id is empty")
	}

	useResp := postJSON(t, server.Handler(), "/v1/bindings/use", map[string]any{
		"client":       "terminal",
		"external_key": "local",
		"session_id":   created.Session.ID,
	})
	if useResp.Code != http.StatusOK {
		t.Fatalf("POST /v1/bindings/use status = %d, want %d", useResp.Code, http.StatusOK)
	}

	messageResp := postJSON(t, server.Handler(), "/v1/messages", map[string]any{
		"client":       "terminal",
		"external_key": "local",
		"text":         "hello",
	})
	if messageResp.Code != http.StatusAccepted {
		t.Fatalf("POST /v1/messages status = %d, want %d", messageResp.Code, http.StatusAccepted)
	}

	var accepted struct {
		SessionID   string       `json:"session_id"`
		UserMessage core.Message `json:"user_message"`
		Run         core.Run     `json:"run"`
	}
	decodeJSON(t, messageResp, &accepted)
	if accepted.Run.ID == "" {
		t.Fatalf("accepted run id is empty")
	}
	if accepted.UserMessage.ID == "" {
		t.Fatalf("accepted user message id is empty")
	}
	if accepted.Run.Status != core.RunStatusAccepted {
		t.Fatalf("run status = %q, want %q", accepted.Run.Status, core.RunStatusAccepted)
	}

	run := waitForRun(t, server.Handler(), accepted.Run.ID, core.RunStatusCompleted)
	if run.Status != core.RunStatusCompleted {
		t.Fatalf("run status = %q, want %q", run.Status, core.RunStatusCompleted)
	}

	getMessages := httptest.NewRequest(http.MethodGet, "/v1/messages?session_id="+created.Session.ID+"&limit=10", nil)
	getMessagesRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(getMessagesRec, getMessages)
	if getMessagesRec.Code != http.StatusOK {
		t.Fatalf("GET /v1/messages status = %d, want %d", getMessagesRec.Code, http.StatusOK)
	}

	var listed struct {
		Messages []core.Message `json:"messages"`
	}
	decodeJSON(t, getMessagesRec, &listed)
	if len(listed.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(listed.Messages))
	}
}

func TestAPISessionRenameAndDelete(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	app := core.New(sqliteStore).WithSessionLLMs(newSessionLLMRegistryStub(newProviderStub()))
	server := api.New(app)

	sessionResp := postJSON(t, server.Handler(), "/v1/sessions", map[string]any{
		"title": "Docs",
	})
	if sessionResp.Code != http.StatusCreated {
		t.Fatalf("POST /v1/sessions status = %d, want %d", sessionResp.Code, http.StatusCreated)
	}

	var created struct {
		Session core.Session `json:"session"`
	}
	decodeJSON(t, sessionResp, &created)

	renameResp := postJSONMethod(t, server.Handler(), http.MethodPatch, "/v1/sessions/"+created.Session.ID, map[string]any{
		"title": "Renamed Docs",
	})
	if renameResp.Code != http.StatusOK {
		t.Fatalf("PATCH /v1/sessions/{id} status = %d, want %d", renameResp.Code, http.StatusOK)
	}

	var renamed struct {
		Session core.Session `json:"session"`
	}
	decodeJSON(t, renameResp, &renamed)
	if renamed.Session.Title != "Renamed Docs" {
		t.Fatalf("renamed title = %q, want %q", renamed.Session.Title, "Renamed Docs")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v1/sessions/"+created.Session.ID, nil)
	deleteRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /v1/sessions/{id} status = %d, want %d", deleteRec.Code, http.StatusNoContent)
	}

	getSessionsReq := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	getSessionsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(getSessionsRec, getSessionsReq)
	if getSessionsRec.Code != http.StatusOK {
		t.Fatalf("GET /v1/sessions status = %d, want %d", getSessionsRec.Code, http.StatusOK)
	}

	var listed struct {
		Sessions []core.Session `json:"sessions"`
	}
	decodeJSON(t, getSessionsRec, &listed)
	if len(listed.Sessions) != 0 {
		t.Fatalf("len(sessions) = %d, want 0 after delete", len(listed.Sessions))
	}
}

func TestAPIToolsAndApprovalsFlow(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	app := core.New(sqliteStore).WithSessionLLMs(newSessionLLMRegistryStub(newProviderStub())).WithTools(newCoreCodingRegistry())
	server := api.New(app)
	workdir := t.TempDir()

	sessionResp := postJSON(t, server.Handler(), "/v1/sessions", map[string]any{
		"title": "Tool Docs",
	})
	if sessionResp.Code != http.StatusCreated {
		t.Fatalf("POST /v1/sessions status = %d, want %d", sessionResp.Code, http.StatusCreated)
	}
	var created struct {
		Session core.Session `json:"session"`
	}
	decodeJSON(t, sessionResp, &created)

	req := httptest.NewRequest(http.MethodGet, "/v1/tools", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/tools status = %d, want %d", rec.Code, http.StatusOK)
	}

	writeArgs, _ := json.Marshal(map[string]any{
		"file_path": "notes.txt",
		"content":   "hello\nworld\n",
	})
	firstExecute := postJSON(t, server.Handler(), "/v1/tools/execute", map[string]any{
		"session_id":  created.Session.ID,
		"tool_name":   "write",
		"working_dir": workdir,
		"args":        json.RawMessage(writeArgs),
	})
	if firstExecute.Code != http.StatusOK {
		t.Fatalf("POST /v1/tools/execute status = %d, want %d", firstExecute.Code, http.StatusOK)
	}
	var pending struct {
		Result core.ExecuteToolResult `json:"result"`
	}
	decodeJSON(t, firstExecute, &pending)
	if pending.Result.Approval == nil {
		t.Fatal("first tool execute should request approval")
	}

	listApprovalsReq := httptest.NewRequest(http.MethodGet, "/v1/approvals?session_id="+created.Session.ID+"&state=pending", nil)
	listApprovalsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(listApprovalsRec, listApprovalsReq)
	if listApprovalsRec.Code != http.StatusOK {
		t.Fatalf("GET /v1/approvals status = %d, want %d", listApprovalsRec.Code, http.StatusOK)
	}
	var approvals struct {
		Approvals []core.Approval `json:"approvals"`
	}
	decodeJSON(t, listApprovalsRec, &approvals)
	if len(approvals.Approvals) != 1 {
		t.Fatalf("len(approvals) = %d, want 1", len(approvals.Approvals))
	}

	resolve := postJSON(t, server.Handler(), "/v1/approvals/"+pending.Result.Approval.ID+"/resolve", map[string]any{
		"approved": true,
	})
	if resolve.Code != http.StatusOK {
		t.Fatalf("POST /v1/approvals/{id}/resolve status = %d, want %d", resolve.Code, http.StatusOK)
	}

	if got, err := os.ReadFile(filepath.Join(workdir, "notes.txt")); err != nil || string(got) != "hello\nworld\n" {
		t.Fatalf("approved tool replay wrote %q, err=%v", string(got), err)
	}

	rejectedExecute := postJSON(t, server.Handler(), "/v1/tools/execute", map[string]any{
		"session_id":   created.Session.ID,
		"tool_name":    "write",
		"tool_call_id": pending.Result.ToolCallMessage.ID,
		"working_dir":  workdir,
		"approved":     true,
		"args":         json.RawMessage(writeArgs),
	})
	if rejectedExecute.Code != http.StatusBadRequest {
		t.Fatalf("approved POST /v1/tools/execute status = %d, want %d", rejectedExecute.Code, http.StatusBadRequest)
	}
}

func waitForRun(t *testing.T, handler http.Handler, runID string, want core.RunStatus) core.Run {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/v1/runs/"+runID, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			var payload struct {
				Run core.Run `json:"run"`
			}
			decodeJSON(t, rec, &payload)
			if payload.Run.Status == want {
				return payload.Run
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/"+runID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/runs/{id} status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		Run core.Run `json:"run"`
	}
	decodeJSON(t, rec, &payload)
	t.Fatalf("run status = %q, want %q", payload.Run.Status, want)
	return core.Run{}
}

func postJSON(t *testing.T, handler http.Handler, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()

	return postJSONMethod(t, handler, http.MethodPost, path, payload)
}

func postJSONMethod(t *testing.T, handler http.Handler, method string, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()

	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
}
