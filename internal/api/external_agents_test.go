package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/setup"
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
	if len(agents.Agents[0].Aliases) != 1 || agents.Agents[0].Aliases[0] != "codex" {
		t.Fatalf("external agent aliases = %#v, want codex", agents.Agents[0].Aliases)
	}
	if !agents.Agents[0].Capabilities.StartSession || !agents.Agents[0].Capabilities.StreamingEvents {
		t.Fatalf("external agent capabilities = %#v, want start and streaming", agents.Agents[0].Capabilities)
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
	if sessionResp.Session.RuntimeID != core.SessionRuntimeExternalAgent {
		t.Fatalf("session runtime = %q, want external_agent", sessionResp.Session.RuntimeID)
	}
	attachment, err := sqliteStore.GetExternalAgentSession(context.Background(), sessionResp.Session.ID)
	if err != nil {
		t.Fatalf("GetExternalAgentSession() error = %v", err)
	}
	if attachment.ExternalThreadID != "thread_1" {
		t.Fatalf("attachment thread = %q, want thread_1", attachment.ExternalThreadID)
	}
}

func TestExternalAgentsAPIPatchPathPreservesEnabledState(t *testing.T) {
	t.Parallel()

	setupStore := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	setupService := setup.NewService(setupStore)
	if err := setupStore.Save(setup.Config{
		Daemon:  setup.DaemonConfig{HTTPAddr: "127.0.0.1:18081", DBPath: filepath.Join(t.TempDir(), "matrixclaw.db")},
		Modules: setup.ModulesConfig{ExternalAgents: map[string]setup.ExternalAgentConfig{"codex-app": {Enabled: true}}},
	}); err != nil {
		t.Fatalf("Save setup() error = %v", err)
	}
	runtime := &apiExternalRuntimeStub{}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	app := newTestCore(t).WithExternalAgents(registry, nil)
	httpServer := newSetupProviderHTTPServer(t, app, setupService, nil)

	req, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/v1/external-agents/codex-app", strings.NewReader(`{"path":"/opt/bin/codex"}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH external agent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH external agent status = %d, want 200", resp.StatusCode)
	}

	cfg, err := setupService.Load()
	if err != nil {
		t.Fatalf("Load setup() error = %v", err)
	}
	agent := cfg.ExternalAgentConfig("codex-app")
	if !agent.Enabled || agent.Path != "/opt/bin/codex" {
		t.Fatalf("external agent config = %#v, want enabled with updated path", agent)
	}
}

func TestExternalAgentsAPIPatchAliasUsesCanonicalSetupKey(t *testing.T) {
	t.Parallel()

	setupStore := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	setupService := setup.NewService(setupStore)
	if err := setupStore.Save(setup.Config{
		Daemon: setup.DaemonConfig{HTTPAddr: "127.0.0.1:18081", DBPath: filepath.Join(t.TempDir(), "matrixclaw.db")},
	}); err != nil {
		t.Fatalf("Save setup() error = %v", err)
	}
	runtime := &apiExternalRuntimeStub{}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	app := newTestCore(t).WithExternalAgents(registry, nil)
	httpServer := newSetupProviderHTTPServer(t, app, setupService, nil)

	req, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/v1/external-agents/codex", strings.NewReader(`{"enabled":true}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH external agent alias: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH external agent alias status = %d, want 200", resp.StatusCode)
	}

	cfg, err := setupService.Load()
	if err != nil {
		t.Fatalf("Load setup() error = %v", err)
	}
	if !cfg.ExternalAgentConfig("codex-app").Enabled {
		t.Fatalf("canonical codex-app config = %#v, want enabled", cfg.Modules.ExternalAgents)
	}
	if _, ok := cfg.Modules.ExternalAgents["codex"]; ok {
		t.Fatalf("alias key should not be stored: %#v", cfg.Modules.ExternalAgents)
	}
}

func TestExternalAgentsAPIPatchMigratesLegacyAliasConfig(t *testing.T) {
	t.Parallel()

	setupStore := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	legacyConfig, err := json.Marshal(setup.Config{
		Version: setup.CurrentVersion,
		Daemon:  setup.DaemonConfig{HTTPAddr: "127.0.0.1:18081", DBPath: filepath.Join(t.TempDir(), "matrixclaw.db")},
		Modules: setup.ModulesConfig{ExternalAgents: map[string]setup.ExternalAgentConfig{"codex": {Enabled: true}}},
	})
	if err != nil {
		t.Fatalf("Marshal legacy config: %v", err)
	}
	if err := os.WriteFile(setupStore.Path(), legacyConfig, 0o600); err != nil {
		t.Fatalf("Write legacy setup() error = %v", err)
	}
	setupService := setup.NewService(setupStore)
	runtime := &apiExternalRuntimeStub{}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	app := newTestCore(t).WithExternalAgents(registry, nil)
	httpServer := newSetupProviderHTTPServer(t, app, setupService, nil)

	req, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/v1/external-agents/codex-app", strings.NewReader(`{"path":"/opt/bin/codex"}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH external agent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH external agent status = %d, want 200", resp.StatusCode)
	}

	cfg, err := setupService.Load()
	if err != nil {
		t.Fatalf("Load setup() error = %v", err)
	}
	agent := cfg.ExternalAgentConfig("codex-app")
	if !agent.Enabled || agent.Path != "/opt/bin/codex" {
		t.Fatalf("canonical external agent config = %#v, want enabled with updated path", agent)
	}
	if _, ok := cfg.Modules.ExternalAgents["codex"]; ok {
		t.Fatalf("legacy alias key should be migrated: %#v", cfg.Modules.ExternalAgents)
	}
}

type apiExternalRuntimeStub struct{}

func (s *apiExternalRuntimeStub) ID() string { return "codex-app" }

func (s *apiExternalRuntimeStub) DisplayName() string { return "Codex" }

func (s *apiExternalRuntimeStub) Aliases() []string { return []string{"codex"} }

func (s *apiExternalRuntimeStub) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true, Mode: "test"}
}

func (s *apiExternalRuntimeStub) Capabilities() externalagents.Capabilities {
	return externalagents.Capabilities{StartSession: true, ResumeSession: true, StreamingEvents: true, ConfigurablePath: true}
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
