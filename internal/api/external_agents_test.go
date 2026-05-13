package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestExternalAgentsAPIListsAndCreatesSession(t *testing.T) {
	t.Parallel()

	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer sqliteStore.Close()

	runtime := &apiExternalRuntimeStub{}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	app := core.New(sqliteStore).WithExternalAgents(registry, sqliteStore)
	server := httptest.NewServer(New(app).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/external-agents")
	if err != nil {
		t.Fatalf("GET external agents: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET external agents status = %d, want 200", resp.StatusCode)
	}
	var agents core.ExternalAgentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		t.Fatalf("Decode external agents: %v", err)
	}
	if len(agents.Agents) != 1 || agents.Agents[0].ID != "codex-app" || !agents.Agents[0].Enabled {
		t.Fatalf("external agents = %#v, want enabled codex-app", agents.Agents)
	}

	body := strings.NewReader(`{"title":"Codex","runtime_id":"codex","working_dir":"/tmp","model_id":"gpt-5.4"}`)
	resp, err = http.Post(server.URL+"/v1/sessions", "application/json", body)
	if err != nil {
		t.Fatalf("POST session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST session status = %d, want 201", resp.StatusCode)
	}
	var sessionResp core.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		t.Fatalf("Decode session: %v", err)
	}
	if sessionResp.Session.Kind != core.SessionKindExternalAgent {
		t.Fatalf("session kind = %q, want external_agent", sessionResp.Session.Kind)
	}
	if sessionResp.Session.RuntimeID != core.SessionRuntimeCodex {
		t.Fatalf("session runtime = %q, want codex", sessionResp.Session.RuntimeID)
	}
	attachment, err := sqliteStore.GetExternalAgentSession(context.Background(), sessionResp.Session.ID)
	if err != nil {
		t.Fatalf("GetExternalAgentSession() error = %v", err)
	}
	if attachment.ExternalThreadID != "thread_1" {
		t.Fatalf("attachment thread = %q, want thread_1", attachment.ExternalThreadID)
	}
}

type apiExternalRuntimeStub struct{}

func (s *apiExternalRuntimeStub) ID() string { return "codex-app" }

func (s *apiExternalRuntimeStub) DisplayName() string { return "Codex" }

func (s *apiExternalRuntimeStub) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true, Mode: "test"}
}

func (s *apiExternalRuntimeStub) StartSession(context.Context, externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{
		AgentID:           "codex-app",
		ExternalThreadID:  "thread_1",
		ExternalSessionID: "external_session_1",
		CWD:               "/tmp",
		Model:             "gpt-5.4",
	}, nil
}

func (s *apiExternalRuntimeStub) ResumeSession(context.Context, externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{}, nil
}

func (s *apiExternalRuntimeStub) Send(context.Context, externalagents.ExternalSession, externalagents.Input) (<-chan externalagents.Event, error) {
	return nil, nil
}

func (s *apiExternalRuntimeStub) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}

func (s *apiExternalRuntimeStub) Close() error { return nil }
