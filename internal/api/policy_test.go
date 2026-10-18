package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// routePolicies is who may use each route; see the policies in role.go.
var routePolicies = map[string]string{
	"GET /v1/health":                       "open",
	"GET /v1/server/status":                "open",
	"POST /v1/admin/reload":                "owner",
	"POST /v1/admin/restart":               "owner",
	"POST /v1/admin/stop":                  "owner",
	"GET /v1/setup/providers":              "open",
	"PATCH /v1/setup/providers/{id}":       "owner",
	"DELETE /v1/setup/providers/{id}":      "owner",
	"POST /v1/setup/providers/{id}/models": "owner",
	"GET /v1/session-providers":            "open",
	"GET /v1/external-agents":              "open",
	"PATCH /v1/external-agents/{id}":       "owner",

	"GET /v1/automation/jobs":                                 "open",
	"POST /v1/automation/jobs":                                "session(body)",
	"GET /v1/automation/jobs/{id}":                            "open",
	"DELETE /v1/automation/jobs/{id}":                         "session(job)",
	"POST /v1/automation/jobs/{id}/{action}":                  "session(job)",
	"GET /v1/client-deliveries":                               "open",
	"POST /v1/client-deliveries/{id}/ack":                     "open",
	"POST /v1/client-deliveries/{id}/fail":                    "open",
	"GET /v1/sessions":                                        "open",
	"POST /v1/sessions":                                       "open",
	"GET /v1/sessions/{id}":                                   "open",
	"PATCH /v1/sessions/{id}":                                 "session(id)",
	"DELETE /v1/sessions/{id}":                                "session(id)",
	"PATCH /v1/sessions/{id}/llm":                             "session(id)",
	"GET /v1/sessions/{id}/models":                            "open",
	"PATCH /v1/sessions/{id}/permissions":                     "owner",
	"GET /v1/sessions/{id}/permission-rules":                  "open",
	"POST /v1/sessions/{id}/permission-rules":                 "session(id)",
	"GET /v1/sessions/{id}/context":                           "open",
	"GET /v1/sessions/{id}/usage":                             "open",
	"GET /v1/sessions/{id}/budget":                            "open",
	"PUT /v1/sessions/{id}/budget":                            "session(id)",
	"GET /v1/sessions/{id}/todo":                              "open",
	"DELETE /v1/sessions/{id}/todo":                           "session(id)",
	"GET /v1/sessions/{id}/hooks":                             "open",
	"POST /v1/sessions/{id}/hooks/trust":                      "owner",
	"GET /v1/sessions/{id}/tasks":                             "open",
	"POST /v1/sessions/{id}/compact":                          "session(id)",
	"POST /v1/sessions/{id}/clear":                            "session(id)",
	"POST /v1/sessions/{id}/system-message":                   "session(id)",
	"GET /v1/sessions/{id}/checkpoints":                       "open",
	"POST /v1/sessions/{id}/checkpoints/{checkpoint}/restore": "owner",
	"GET /v1/bindings/current":                                "open",
	"POST /v1/bindings/use":                                   "session(body)",
	"GET /v1/snapshot":                                        "open",
	"GET /v1/events":                                          "open",
	"GET /v1/messages":                                        "open",
	"POST /v1/messages":                                       "session(body)",
	"GET /v1/search":                                          "open",
	"GET /v1/memory":                                          "open",

	"GET /v1/runs/{id}":                            "open",
	"GET /v1/runs/{id}/steps":                      "open",
	"GET /v1/runs/{id}/progress":                   "open",
	"POST /v1/runs/{id}/cancel":                    "session(run)",
	"GET /v1/tasks/{id}":                           "open",
	"POST /v1/tasks/{id}/cancel":                   "session(task)",
	"GET /v1/approvals":                            "open",
	"POST /v1/approvals/{id}/resolve":              "session(approval)",
	"DELETE /v1/permission-rules/{id}":             "open",
	"GET /v1/tools":                                "open",
	"POST /v1/tools/execute":                       "session(body)",
	"GET /v1/modules":                              "open",
	"GET /v1/settings/{module}":                    "open",
	"POST /v1/settings/{module}":                   "owner",
	"GET /v1/modules/storage/files":                "open",
	"POST /v1/modules/storage/files":               "open",
	"GET /v1/modules/storage/files/{path...}":      "open",
	"DELETE /v1/modules/storage/files/{path...}":   "owner",
	"GET /v1/modules/storage/temp":                 "open",
	"POST /v1/modules/storage/temp":                "open",
	"POST /v1/modules/storage/temp/cleanup":        "owner",
	"PATCH /v1/modules/storage/temp/settings":      "owner",
	"GET /v1/modules/storage/temp/{path...}":       "open",
	"DELETE /v1/modules/storage/temp/{path...}":    "owner",
	"POST /v1/modules/storage/temp/{path}/promote": "open",
	"POST /v1/modules/voice/tts":                   "open",
	"POST /v1/modules/voice/stt":                   "open",
	"GET /v1/modules/voice/realtime_voice":         "open",
	"POST /v1/realtime-voice/sessions":             "session(body)",
	"GET /v1/realtime-voice/sessions/{id}":         "open",
	"DELETE /v1/realtime-voice/sessions/{id}":      "open",
	"GET /v1/realtime-voice/sessions/{id}/stream":  "open",
	"GET /v1/modules/mcp":                          "open",
	"PATCH /v1/modules/mcp":                        "owner",
	"POST /v1/modules/mcp/servers":                 "owner",
	"PATCH /v1/modules/mcp/{id}/server":            "owner",
	"DELETE /v1/modules/mcp/{id}/server":           "owner",
	"GET /v1/modules/skills":                       "open",
	"POST /v1/modules/skills":                      "owner",
	"POST /v1/modules/skills/drafts":               "owner",
	"GET /v1/modules/skills/{id}":                  "open",
	"PATCH /v1/modules/skills/{id}":                "owner",
	"DELETE /v1/modules/skills/{id}":               "owner",
	"PUT /v1/modules/skills/{id}/body":             "owner",
	"POST /v1/modules/skills/{id}/{action}":        "owner",
	"GET /v1/sessions/{id}/skills":                 "open",
	"POST /v1/sessions/{id}/skills/{skill}":        "session(id)",
	"DELETE /v1/sessions/{id}/skills/{skill}":      "session(id)",
}

func TestEveryRouteHasItsPolicy(t *testing.T) {
	routes := New(Deps{}).routeTable()
	if len(routes) != len(routePolicies) {
		t.Errorf("%d routes, %d expected policies", len(routes), len(routePolicies))
	}
	for _, route := range routes {
		if want, ok := routePolicies[route.pattern]; !ok || route.policy.name != want {
			t.Errorf("%s: policy %q, want %q", route.pattern, route.policy.name, want)
		}
	}
}

// unattendedServer is a test server whose only session s1 runs full_auto,
// with a run, a task and an approval in it.
func unattendedServer(t *testing.T) *Server {
	t.Helper()
	server, st := newAPITestServer(t)
	if got := serveAs(server, core.RoleOwner, "PATCH", "/v1/sessions/s1/permissions", `{"permission_mode":"full_auto"}`); got.Code != http.StatusOK {
		t.Fatalf("full_auto: status %d", got.Code)
	}
	ctx := context.Background()
	run := core.Run{ID: "r1", SessionID: "s1", UserMessageID: "m1", Status: core.RunStatusRunning, StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}
	user := transcript.Message{ID: "m1", SessionID: "s1", RunID: "r1", Role: transcript.MessageRoleUser, Content: "hi", CreatedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}
	if err := st.AcceptMessage(ctx, user, run); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateTask(ctx, core.Task{ID: "t1", SessionID: "s1", RunID: "r1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "sleep 9", StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateApproval(ctx, core.Approval{ID: "a1", SessionID: "s1", RunID: "r1", ToolName: "bash", State: core.ApprovalStatePending, RequestedAt: apiTestEpoch}); err != nil {
		t.Fatal(err)
	}
	return server
}

func TestMembersAndGuestsActInNoUnattendedSession(t *testing.T) {
	server := unattendedServer(t)
	ids := map[string]string{"session(id)": "s1", "session(run)": "r1", "session(task)": "t1", "session(approval)": "a1"}
	checked := 0
	for _, route := range server.routeTable() {
		id, ok := ids[route.policy.name]
		if route.policy.name == "session(body)" {
			ok = route.pattern != "POST /v1/automation/jobs" || server.Automation != nil
		}
		if !ok {
			continue
		}
		method, path, _ := strings.Cut(route.pattern, " ")
		path = strings.NewReplacer("{id}", id, "{skill}", "review").Replace(path)
		for _, role := range []core.Role{core.RoleMember, core.RoleGuest} {
			got := serveAs(server, role, method, path, `{"session_id":"s1","text":"hi","tool_name":"read"}`)
			if got.Code != http.StatusForbidden {
				t.Errorf("%s %s: status %d %s, want 403", role, route.pattern, got.Code, got.Body.String())
			}
		}
		checked++
	}
	if checked < 15 {
		t.Fatalf("checked %d session routes", checked)
	}
}

func TestAMemberIsNotBoundToTheOnlySessionWhenItRunsUnattended(t *testing.T) {
	server := unattendedServer(t)
	message := `{"client":"telegram","external_key":"guest:q","text":"hi","allow_auto_bind_one":true}`
	if got := serveAs(server, core.RoleGuest, "POST", "/v1/messages", message); got.Code != http.StatusForbidden {
		t.Fatalf("guest message: status %d %s, want 403", got.Code, got.Body.String())
	}
	if _, err := server.Core.CurrentBinding(context.Background(), "telegram", "guest:q"); err == nil {
		t.Fatal("the guest was bound to the full_auto session")
	}
}

func TestErrorsAnswerTheirSentinelsStatus(t *testing.T) {
	server, _ := newAPITestServer(t)
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/v1/sessions/nope", http.StatusNotFound},
		{"POST", "/v1/runs/nope/cancel", http.StatusNotFound},
		{"GET", "/v1/memory?limit=x", http.StatusBadRequest},
		{"GET", "/v1/search?q=a&limit=x", http.StatusBadRequest},
	} {
		if got := serveAs(server, core.RoleOwner, tc.method, tc.path, ""); got.Code != tc.want {
			t.Errorf("%s %s: status %d %s, want %d", tc.method, tc.path, got.Code, got.Body.String(), tc.want)
		}
	}
}
