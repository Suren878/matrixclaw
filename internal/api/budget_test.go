package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionBudgetEndpointStoresOverrides(t *testing.T) {
	server, _ := newAPITestServer(t)
	put := httptest.NewRecorder()
	server.Handler().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/v1/sessions/s1/budget", strings.NewReader(`{"steps":7,"tokens":0}`)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/budget", nil))
	var response core.SessionBudgetResponse
	if err := json.Unmarshal(get.Body.Bytes(), &response); err != nil {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
	budget := response.Budget
	if budget.Steps != 7 || budget.Tokens != 0 || budget.ActiveSeconds != 4*3600 || budget.Override.Steps == nil || budget.Override.ActiveSeconds != nil {
		t.Fatalf("budget = %+v", budget)
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodPut, "/v1/sessions/s1/budget", strings.NewReader(`{"steps":0}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid PUT status=%d", invalid.Code)
	}
}
