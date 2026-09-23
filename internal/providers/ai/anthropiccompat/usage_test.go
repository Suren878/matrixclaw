package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const anthropicStreamWithUsage = "event: message_start\n" +
	`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":300,"output_tokens":1}}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"

func TestUsageCountsCachedInputAsPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(anthropicStreamWithUsage))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Hi"}],"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":300,"output_tokens":50}}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	want := providers.Usage{PromptTokens: 420, CacheReadTokens: 300, CacheWriteTokens: 20, OutputTokens: 50}
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"json", context.Background()},
		{"stream", providers.WithTextStream(context.Background(), func(string) error { return nil })},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := runtime.Generate(tc.ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			got := response.Usage
			if len(got.ProviderRaw) == 0 {
				t.Fatal("provider raw usage missing")
			}
			got.ProviderRaw = nil
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("usage=%+v, want %+v", got, want)
			}
		})
	}
}
