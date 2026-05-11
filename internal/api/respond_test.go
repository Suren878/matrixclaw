package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testDecodePayload struct {
	Name string `json:"name"`
}

func TestDecodeJSONBodyRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name":"one"} {"name":"two"}`))
	rec := httptest.NewRecorder()
	var payload testDecodePayload

	if decodeJSONBody(rec, req, &payload) {
		t.Fatal("decodeJSONBody() = true, want false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "invalid json body") {
		t.Fatalf("body = %q, want invalid json body", rec.Body.String())
	}
}

func TestDecodeJSONBodyRejectsOversizeBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name":"abcdef"}`))
	rec := httptest.NewRecorder()
	var payload testDecodePayload

	if decodeJSONBodyLimit(rec, req, &payload, 8) {
		t.Fatal("decodeJSONBodyLimit() = true, want false")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body = %q, want request body too large", rec.Body.String())
	}
}

func TestDecodeOptionalJSONBodyAllowsEmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(""))
	rec := httptest.NewRecorder()
	var payload testDecodePayload

	if !decodeOptionalJSONBody(rec, req, &payload) {
		t.Fatal("decodeOptionalJSONBody() = false, want true")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
