package api

import (
	"net/http/httptest"
	"testing"
)

// clientRoutes are the requests the clients send: daemonclient (terminal,
// Telegram, CLI), the telephony gateway and the Swift package. Each must
// reach the named route.
var clientRoutes = []struct {
	method, path, pattern string
}{
	// Server and setup.
	{"GET", "/v1/health", "GET /v1/health"},
	{"GET", "/v1/server/status", "GET /v1/server/status"},
	{"POST", "/v1/admin/reload", "POST /v1/admin/reload"},
	{"POST", "/v1/admin/restart", "POST /v1/admin/restart"},
	{"POST", "/v1/admin/stop", "POST /v1/admin/stop"},
	{"GET", "/v1/setup/providers?client=telegram", "GET /v1/setup/providers"},
	{"PATCH", "/v1/setup/providers/openai?client=ios", "PATCH /v1/setup/providers/{id}"},
	{"DELETE", "/v1/setup/providers/openai", "DELETE /v1/setup/providers/{id}"},
	{"POST", "/v1/setup/providers/openai/models", "POST /v1/setup/providers/{id}/models"},
	{"GET", "/v1/session-providers", "GET /v1/session-providers"},
	{"GET", "/v1/external-agents", "GET /v1/external-agents"},
	{"PATCH", "/v1/external-agents/codex", "PATCH /v1/external-agents/{id}"},

	// Automation and deliveries.
	{"GET", "/v1/automation/jobs", "GET /v1/automation/jobs"},
	{"POST", "/v1/automation/jobs", "POST /v1/automation/jobs"},
	{"DELETE", "/v1/automation/jobs/j1", "DELETE /v1/automation/jobs/{id}"},
	{"POST", "/v1/automation/jobs/j1/pause", "POST /v1/automation/jobs/{id}/{action}"},
	{"POST", "/v1/automation/jobs/j1/run-now", "POST /v1/automation/jobs/{id}/{action}"},
	{"GET", "/v1/client-deliveries?client=telegram&status=pending", "GET /v1/client-deliveries"},
	{"POST", "/v1/client-deliveries/d1/ack", "POST /v1/client-deliveries/{id}/ack"},
	{"POST", "/v1/client-deliveries/d1/fail", "POST /v1/client-deliveries/{id}/fail"},

	// Sessions.
	{"GET", "/v1/sessions", "GET /v1/sessions"},
	{"POST", "/v1/sessions", "POST /v1/sessions"},
	{"GET", "/v1/sessions/s1", "GET /v1/sessions/{id}"},
	{"PATCH", "/v1/sessions/s1", "PATCH /v1/sessions/{id}"},
	{"DELETE", "/v1/sessions/s1", "DELETE /v1/sessions/{id}"},
	{"PATCH", "/v1/sessions/s1/llm", "PATCH /v1/sessions/{id}/llm"},
	{"GET", "/v1/sessions/s1/models", "GET /v1/sessions/{id}/models"},
	{"PATCH", "/v1/sessions/s1/permissions", "PATCH /v1/sessions/{id}/permissions"},
	{"GET", "/v1/sessions/s1/permission-rules", "GET /v1/sessions/{id}/permission-rules"},
	{"POST", "/v1/sessions/s1/permission-rules", "POST /v1/sessions/{id}/permission-rules"},
	{"GET", "/v1/sessions/s1/context", "GET /v1/sessions/{id}/context"},
	{"GET", "/v1/sessions/s1/usage", "GET /v1/sessions/{id}/usage"},
	{"GET", "/v1/sessions/s1/budget", "GET /v1/sessions/{id}/budget"},
	{"PUT", "/v1/sessions/s1/budget", "PUT /v1/sessions/{id}/budget"},
	{"GET", "/v1/sessions/s1/todo", "GET /v1/sessions/{id}/todo"},
	{"DELETE", "/v1/sessions/s1/todo", "DELETE /v1/sessions/{id}/todo"},
	{"GET", "/v1/sessions/s1/tasks", "GET /v1/sessions/{id}/tasks"},
	{"POST", "/v1/sessions/s1/compact", "POST /v1/sessions/{id}/compact"},
	{"POST", "/v1/sessions/s1/clear", "POST /v1/sessions/{id}/clear"},
	{"POST", "/v1/sessions/s1/system-message", "POST /v1/sessions/{id}/system-message"},
	{"GET", "/v1/bindings/current?client=terminal&external_key=local", "GET /v1/bindings/current"},
	{"POST", "/v1/bindings/use", "POST /v1/bindings/use"},
	{"GET", "/v1/snapshot?client=ios&external_key=phone", "GET /v1/snapshot"},
	{"GET", "/v1/events?session_id=s1", "GET /v1/events"},
	{"GET", "/v1/messages?session_id=s1&after_seq=3", "GET /v1/messages"},
	{"POST", "/v1/messages", "POST /v1/messages"},
	{"GET", "/v1/search?q=x", "GET /v1/search"},
	{"GET", "/v1/memory?scope=user", "GET /v1/memory"},

	// Runs, tasks, approvals, rules, tools.
	{"GET", "/v1/runs/r1", "GET /v1/runs/{id}"},
	{"GET", "/v1/runs/r1/progress", "GET /v1/runs/{id}/progress"},
	{"POST", "/v1/runs/r1/cancel", "POST /v1/runs/{id}/cancel"},
	{"GET", "/v1/tasks/t1", "GET /v1/tasks/{id}"},
	{"POST", "/v1/tasks/t1/cancel", "POST /v1/tasks/{id}/cancel"},
	{"GET", "/v1/approvals?session_id=s1&state=pending", "GET /v1/approvals"},
	{"POST", "/v1/approvals/a1/resolve", "POST /v1/approvals/{id}/resolve"},
	{"DELETE", "/v1/permission-rules/p1", "DELETE /v1/permission-rules/{id}"},
	{"GET", "/v1/tools", "GET /v1/tools"},
	{"POST", "/v1/tools/execute", "POST /v1/tools/execute"},

	// Modules.
	{"GET", "/v1/modules", "GET /v1/modules"},
	{"GET", "/v1/settings/tts", "GET /v1/settings/{module}"},
	{"POST", "/v1/settings/web_search", "POST /v1/settings/{module}"},
	{"GET", "/v1/modules/storage/files?prefix=notes", "GET /v1/modules/storage/files"},
	{"POST", "/v1/modules/storage/files", "POST /v1/modules/storage/files"},
	{"GET", "/v1/modules/storage/files/notes%2Fa.md?encoding=base64", "GET /v1/modules/storage/files/{path...}"},
	{"GET", "/v1/modules/storage/files/notes/a.md", "GET /v1/modules/storage/files/{path...}"},
	{"DELETE", "/v1/modules/storage/files/notes%2Fa.md", "DELETE /v1/modules/storage/files/{path...}"},
	{"GET", "/v1/modules/storage/temp?limit=5", "GET /v1/modules/storage/temp"},
	{"POST", "/v1/modules/storage/temp", "POST /v1/modules/storage/temp"},
	{"POST", "/v1/modules/storage/temp/cleanup", "POST /v1/modules/storage/temp/cleanup"},
	{"PATCH", "/v1/modules/storage/temp/settings", "PATCH /v1/modules/storage/temp/settings"},
	{"GET", "/v1/modules/storage/temp/calls%2Fa.wav", "GET /v1/modules/storage/temp/{path...}"},
	{"DELETE", "/v1/modules/storage/temp/calls%2Fa.wav", "DELETE /v1/modules/storage/temp/{path...}"},
	{"POST", "/v1/modules/storage/temp/calls%2Fa.wav/promote", "POST /v1/modules/storage/temp/{path}/promote"},
	{"GET", "/v1/modules/voice", "GET /v1/modules/voice"},
	{"PATCH", "/v1/modules/voice/tts", "PATCH /v1/modules/voice/{module}"},
	{"POST", "/v1/modules/voice/tts", "POST /v1/modules/voice/tts"},
	{"POST", "/v1/modules/voice/stt", "POST /v1/modules/voice/stt"},
	{"POST", "/v1/modules/voice/stt/providers/whispercpp/action", "POST /v1/modules/voice/{module}/providers/{provider}/action"},
	{"GET", "/v1/modules/voice/realtime_voice", "GET /v1/modules/voice/realtime_voice"},
	{"PATCH", "/v1/modules/voice/realtime_voice", "PATCH /v1/modules/voice/realtime_voice"},
	{"POST", "/v1/realtime-voice/sessions", "POST /v1/realtime-voice/sessions"},
	{"GET", "/v1/realtime-voice/sessions/v1", "GET /v1/realtime-voice/sessions/{id}"},
	{"DELETE", "/v1/realtime-voice/sessions/v1", "DELETE /v1/realtime-voice/sessions/{id}"},
	{"GET", "/v1/realtime-voice/sessions/v1/stream", "GET /v1/realtime-voice/sessions/{id}/stream"},
	{"GET", "/v1/modules/mcp", "GET /v1/modules/mcp"},
	{"PATCH", "/v1/modules/mcp", "PATCH /v1/modules/mcp"},
	{"POST", "/v1/modules/mcp/servers", "POST /v1/modules/mcp/servers"},
	{"PATCH", "/v1/modules/mcp/github/server", "PATCH /v1/modules/mcp/{id}/server"},
	{"DELETE", "/v1/modules/mcp/github/server", "DELETE /v1/modules/mcp/{id}/server"},
	{"GET", "/v1/modules/skills?include_archived=1", "GET /v1/modules/skills"},
	{"GET", "/v1/modules/skills?order=usage", "GET /v1/modules/skills"},
	{"POST", "/v1/modules/skills", "POST /v1/modules/skills"},
	{"POST", "/v1/modules/skills/drafts", "POST /v1/modules/skills/drafts"},
	{"GET", "/v1/modules/skills/team%2Freview", "GET /v1/modules/skills/{id}"},
	{"GET", "/v1/modules/skills/usage", "GET /v1/modules/skills/{id}"},
	{"PATCH", "/v1/modules/skills/review", "PATCH /v1/modules/skills/{id}"},
	{"DELETE", "/v1/modules/skills/review", "DELETE /v1/modules/skills/{id}"},
	{"PUT", "/v1/modules/skills/review/body", "PUT /v1/modules/skills/{id}/body"},
	{"POST", "/v1/modules/skills/review/trust", "POST /v1/modules/skills/{id}/{action}"},
	{"GET", "/v1/sessions/s1/skills", "GET /v1/sessions/{id}/skills"},
	{"POST", "/v1/sessions/s1/skills/review", "POST /v1/sessions/{id}/skills/{skill}"},
	{"DELETE", "/v1/sessions/s1/skills/review", "DELETE /v1/sessions/{id}/skills/{skill}"},
}

func TestClientRoutesReachTheirHandlers(t *testing.T) {
	server := New(Deps{})
	for _, route := range clientRoutes {
		request := httptest.NewRequest(route.method, route.path, nil)
		if _, pattern := server.mux.Handler(request); pattern != route.pattern {
			t.Errorf("%s %s routed to %q, want %q", route.method, route.path, pattern, route.pattern)
		}
	}
}
