package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/sessionllm"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/store"
)

func serveAs(server *Server, role core.Role, method string, path string, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if role != "" {
		request.Header.Set(core.RoleHeader, string(role))
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestOnlyTheOwnerChangesModesAndSettings(t *testing.T) {
	server, _ := newAPITestServer(t)
	for _, role := range []core.Role{core.RoleMember, core.RoleGuest} {
		for _, route := range []struct{ method, path, body string }{
			{"PATCH", "/v1/sessions/s1/permissions", `{"permission_mode":"full_auto"}`},
			{"POST", "/v1/admin/reload", ""},
			{"POST", "/v1/admin/stop", ""},
			{"PATCH", "/v1/setup/providers/openai", `{}`},
			{"PATCH", "/v1/modules/mcp", `{"enabled":true}`},
			{"POST", "/v1/modules/skills", `{"path":"/tmp/x"}`},
			{"PATCH", "/v1/external-agents/codex", `{}`},
		} {
			if got := serveAs(server, role, route.method, route.path, route.body); got.Code != http.StatusForbidden {
				t.Errorf("%s %s %s: status %d, want 403", role, route.method, route.path, got.Code)
			}
		}
	}
	if got := serveAs(server, "", "PATCH", "/v1/sessions/s1/permissions", `{"permission_mode":"accept_edits"}`); got.Code != http.StatusOK {
		t.Fatalf("no role header: status %d %s, want the owner's 200", got.Code, got.Body.String())
	}
	if got := serveAs(server, "admin", "GET", "/v1/sessions", ""); got.Code != http.StatusBadRequest {
		t.Fatalf("unknown role: status %d, want 400", got.Code)
	}
}

func TestRulesByRole(t *testing.T) {
	server, _ := newAPITestServer(t)
	rule := func(scope string) string {
		return `{"tool":"mcp","pattern":"github__*","effect":"allow","scope":"` + scope + `"}`
	}
	for _, tc := range []struct {
		role  core.Role
		scope string
		want  int
	}{
		{core.RoleOwner, "global", http.StatusOK},
		{core.RoleMember, "session", http.StatusOK},
		{core.RoleMember, "global", http.StatusForbidden},
		{core.RoleGuest, "session", http.StatusForbidden},
	} {
		if got := serveAs(server, tc.role, "POST", "/v1/sessions/s1/permission-rules", rule(tc.scope)); got.Code != tc.want {
			t.Errorf("%s adds a %s rule: status %d, want %d", tc.role, tc.scope, got.Code, tc.want)
		}
	}

	global := serveAs(server, core.RoleOwner, "POST", "/v1/sessions/s1/permission-rules", rule("global"))
	var added core.PermissionRuleResponse
	if err := json.Unmarshal(global.Body.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	if got := serveAs(server, core.RoleMember, "DELETE", "/v1/permission-rules/"+added.Rule.ID, ""); got.Code != http.StatusForbidden {
		t.Fatalf("member deletes a global rule: status %d, want 403", got.Code)
	}
	if got := serveAs(server, core.RoleOwner, "DELETE", "/v1/permission-rules/"+added.Rule.ID, ""); got.Code != http.StatusNoContent {
		t.Fatalf("owner deletes a global rule: status %d, want 204", got.Code)
	}
}

func TestOnlyTheOwnerReachesAnUnattendedSession(t *testing.T) {
	server, _ := newAPITestServer(t)
	if got := serveAs(server, core.RoleOwner, "PATCH", "/v1/sessions/s1/permissions", `{"permission_mode":"full_auto"}`); got.Code != http.StatusOK {
		t.Fatalf("full_auto: status %d", got.Code)
	}
	message := `{"client":"telegram","external_key":"7","session_id":"s1","text":"hi"}`
	if got := serveAs(server, core.RoleMember, "POST", "/v1/messages", message); got.Code != http.StatusForbidden {
		t.Fatalf("member message to a full_auto session: status %d %s, want 403", got.Code, got.Body.String())
	}
}

func TestOnlyTheOwnerCreatesASessionWithAPermissionMode(t *testing.T) {
	server, _ := newAPITestServer(t)
	if got := serveAs(server, core.RoleMember, "POST", "/v1/sessions", `{"title":"x","permission_mode":"accept_edits"}`); got.Code != http.StatusForbidden {
		t.Fatalf("member accept_edits session: status %d %s, want 403", got.Code, got.Body.String())
	}
	if got := serveAs(server, core.RoleMember, "POST", "/v1/sessions", `{"title":"x"}`); got.Code != http.StatusCreated {
		t.Fatalf("member default session: status %d %s", got.Code, got.Body.String())
	}
	if got := serveAs(server, core.RoleOwner, "POST", "/v1/sessions", `{"title":"x","permission_mode":"full_auto"}`); got.Code != http.StatusCreated {
		t.Fatalf("owner full_auto session: status %d %s", got.Code, got.Body.String())
	}
}

func TestOnlyTheOwnerRunsToolsOrVoiceInAnUnattendedSession(t *testing.T) {
	server, _ := newAPITestServer(t)
	server.Realtime = realtime.NewManager(server.Core, realtime.ProviderSpec{ID: realtime.ProviderGemini, Name: "Gemini", DefaultModel: "m"})
	voice := setup.Config{Modules: setup.ModulesConfig{RealtimeVoice: setup.VoiceModuleConfig{Enabled: true, ProviderID: realtime.ProviderGemini}}}
	if err := server.Realtime.Apply(context.Background(), voice); err != nil {
		t.Fatal(err)
	}
	if got := serveAs(server, core.RoleOwner, "PATCH", "/v1/sessions/s1/permissions", `{"permission_mode":"full_auto"}`); got.Code != http.StatusOK {
		t.Fatalf("full_auto: status %d", got.Code)
	}
	if got := serveAs(server, core.RoleMember, "POST", "/v1/tools/execute", `{"session_id":"s1","tool_name":"bash","args":{"command":"true"}}`); got.Code != http.StatusForbidden {
		t.Fatalf("member tool in a full_auto session: status %d %s, want 403", got.Code, got.Body.String())
	}
	if got := serveAs(server, core.RoleMember, "POST", "/v1/realtime-voice/sessions", `{"session_id":"s1"}`); got.Code != http.StatusForbidden {
		t.Fatalf("member voice in a full_auto session: status %d %s, want 403", got.Code, got.Body.String())
	}
}

func TestMCPEnvIsMaskedForAllButTheOwner(t *testing.T) {
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	cfg := setup.Config{Version: setup.CurrentVersion, Modules: setup.ModulesConfig{MCP: setup.MCPConfig{Servers: []setup.MCPServerConfig{{ID: "gh", Command: "gh-mcp", Env: map[string]string{"TOKEN": "ghp_secret1234"}}}}}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	server := New(Deps{Setup: setup.NewService(store)})
	if got := serveAs(server, core.RoleMember, "GET", "/v1/modules/mcp", ""); got.Code != http.StatusOK || strings.Contains(got.Body.String(), "ghp_secret") {
		t.Fatalf("member mcp config: status %d %s", got.Code, got.Body.String())
	}
	if got := serveAs(server, core.RoleOwner, "GET", "/v1/modules/mcp", ""); !strings.Contains(got.Body.String(), "ghp_secret1234") {
		t.Fatalf("owner mcp config: %s", got.Body.String())
	}
}

func TestMemberModelSwitchLeavesSetupAlone(t *testing.T) {
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateSession(context.Background(), core.Session{ID: "s1", Title: "s1", Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}); err != nil {
		t.Fatal(err)
	}
	provider := setup.ProviderConfig{ID: "fake", Name: "Fake", Type: "openai-compatible", APIKey: "sk-test", BaseURL: "http://127.0.0.1:1", Model: "m1"}
	setupStore := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := setupStore.Save(setup.Config{Version: setup.CurrentVersion, ActiveProviderID: "fake", Providers: []setup.ProviderConfig{provider}}); err != nil {
		t.Fatal(err)
	}
	llms := sessionllm.New("fake", []sessionllm.ProviderSpec{{ID: "fake", CatalogID: "fake", Name: "Fake", Type: provider.Type, APIKey: provider.APIKey, BaseURL: provider.BaseURL, Model: provider.Model}})
	server := New(Deps{Core: core.New(st).WithSessionLLMs(llms), Setup: setup.NewService(setupStore), Reload: func(context.Context) error { return nil }})

	if got := serveAs(server, core.RoleMember, "PATCH", "/v1/sessions/s1/llm", `{"model_id":"m2"}`); got.Code != http.StatusOK {
		t.Fatalf("member model switch: status %d %s", got.Code, got.Body.String())
	}
	if saved, _ := setupStore.Load(); saved.Providers[0].Model != "m1" {
		t.Fatalf("member's switch saved model %q to setup", saved.Providers[0].Model)
	}
	if got := serveAs(server, core.RoleOwner, "PATCH", "/v1/sessions/s1/llm", `{"model_id":"m3"}`); got.Code != http.StatusOK {
		t.Fatalf("owner model switch: status %d %s", got.Code, got.Body.String())
	}
	if saved, _ := setupStore.Load(); saved.Providers[0].Model != "m3" {
		t.Fatalf("owner's switch saved model %q, want m3", saved.Providers[0].Model)
	}
}
