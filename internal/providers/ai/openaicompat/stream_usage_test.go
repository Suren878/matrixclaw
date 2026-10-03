package openaicompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestStreamedRequestAsksForUsage(t *testing.T) {
	var options []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		options = append(options, body["stream_options"])
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseStream(
			`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":2}}`,
		)))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), providers.RuntimeConfig{APIKey: "test", BaseURL: server.URL, Model: "stream-model"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithTextStream(context.Background(), func(string) error { return nil })
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"include_usage": true}; len(options) != 1 || !mapsEqual(options[0], want) {
		t.Fatalf("stream_options=%v, want %v", options, want)
	}
	if response.Usage.PromptTokens != 12 || response.Usage.OutputTokens != 2 {
		t.Fatalf("streamed reply usage=%+v", response.Usage)
	}
}

func TestGenerateRetriesWithoutRejectedStreamOptions(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, asked := body["stream_options"]; asked {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseStream(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), providers.RuntimeConfig{APIKey: "test", BaseURL: server.URL, Model: "strict-gateway"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithTextStream(context.Background(), func(string) error { return nil })
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil || response.Text != "ok" || calls != 2 {
		t.Fatalf("response=%+v err=%v calls=%d", response, err, calls)
	}
}

func mapsEqual(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for key, value := range want {
		if m[key] != value {
			return false
		}
	}
	return true
}
