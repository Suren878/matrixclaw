package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestGetSessionByID(t *testing.T) {
	server, _ := newAPITestServer(t)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1", nil))
	var response core.SessionResponse
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &response) != nil || response.Session.ID != "s1" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/sessions/missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing session status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
