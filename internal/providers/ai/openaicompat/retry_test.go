package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestGenerateAcceptsJSONWhenGatewayIgnoresStreamRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if !request.Stream {
			t.Error("test did not request a stream")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Complete JSON reply"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), providers.RuntimeConfig{APIKey: "test", BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithTextStream(context.Background(), func(string) error { t.Error("JSON reply should not invent deltas"); return nil })
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil || response.Text != "Complete JSON reply" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestGenerateReportsTypedHTTPErrorsWithoutRetrying(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   providers.ErrorKind
	}{{429, providers.ErrorRateLimit}, {503, providers.ErrorOverloaded}, {401, providers.ErrorAuth}} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"message":"rejected"}}`))
		}))
		runtime, err := New(context.Background(), providers.RuntimeConfig{APIKey: "test", BaseURL: server.URL, Model: "test"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
		server.Close()
		var apiErr *providers.APIError
		if !errors.As(err, &apiErr) || apiErr.Kind != tc.kind || apiErr.RetryAfter != 3*time.Second || calls != 1 {
			t.Fatalf("%d: err=%v calls=%d", tc.status, err, calls)
		}
		if err.Error() != fmt.Sprintf("openaicompat: status %d: rejected", tc.status) {
			t.Fatalf("%d: message %q", tc.status, err.Error())
		}
	}
}
