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
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithTextStream(context.Background(), func(string) error { t.Error("JSON reply should not invent deltas"); return nil })
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil || response.Text != "Complete JSON reply" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestGenerateRetriesRateLimitButNotAuthenticationFailure(t *testing.T) {
	for _, status := range []int{429, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":{"message":"rejected"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Recovered"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()
			runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "test"})
			if err != nil {
				t.Fatal(err)
			}
			response, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
			if status == 429 {
				if err != nil || response.Text != "Recovered" || calls != 2 {
					t.Fatalf("response=%#v err=%v calls=%d", response, err, calls)
				}
			} else if err == nil || calls != 1 {
				t.Fatalf("authentication error retried: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestGenerateCancellationInterruptsRateLimitWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
		cancel()
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
		t.Fatalf("cancellation was not honored: %v", err)
	}
}
