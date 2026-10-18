package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestQuotaErrorIsARateLimitWithItsRetryDelay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"You exceeded your current quota, please check your plan and billing details.","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"41s"}]}}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), providers.RuntimeConfig{APIKey: "test", BaseURL: server.URL, Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if kind, wait := providers.ClassifyError(err); kind != providers.ErrorRateLimit || wait != 41*time.Second {
		t.Fatalf("kind=%s wait=%s err=%v", kind, wait, err)
	}
}
