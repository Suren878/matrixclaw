package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
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
