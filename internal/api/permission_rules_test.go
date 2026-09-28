package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestPermissionRuleEndpoints(t *testing.T) {
	server, _ := newAPITestServer(t)
	post := httptest.NewRecorder()
	server.Handler().ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/v1/sessions/s1/permission-rules", strings.NewReader(`{"tool":"mcp","pattern":"github__*","effect":"allow","scope":"session"}`)))
	var added core.PermissionRuleResponse
	if err := json.Unmarshal(post.Body.Bytes(), &added); err != nil || post.Code != http.StatusOK || added.Rule.ID == "" || added.Rule.SessionID != "s1" {
		t.Fatalf("POST status=%d body=%s", post.Code, post.Body.String())
	}

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/permission-rules", nil))
	var listed core.PermissionRulesResponse
	if err := json.Unmarshal(get.Body.Bytes(), &listed); err != nil || len(listed.Rules) != 1 || listed.Rules[0].String() != "mcp: github__*" {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}

	for _, want := range []int{http.StatusNoContent, http.StatusNotFound} {
		deleted := httptest.NewRecorder()
		server.Handler().ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/v1/permission-rules/"+added.Rule.ID, nil))
		if deleted.Code != want {
			t.Fatalf("DELETE status=%d, want %d", deleted.Code, want)
		}
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/v1/sessions/s1/permission-rules", strings.NewReader(`{"tool":"mcp","effect":"sometimes","scope":"session"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid POST status=%d", invalid.Code)
	}
}
