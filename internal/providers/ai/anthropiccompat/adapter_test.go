package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestGenerateSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/messages")
		}
		if got := r.Header.Get("x-api-key"); got != "secret" {
			t.Fatalf("x-api-key = %q, want %q", got, "secret")
		}
		if got := r.Header.Get("anthropic-version"); got != defaultAnthropicVersion {
			t.Fatalf("anthropic-version = %q, want %q", got, defaultAnthropicVersion)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "claude-test" {
			t.Fatalf("model = %#v, want %q", body["model"], "claude-test")
		}
		if body["max_tokens"] != float64(1024) {
			t.Fatalf("max_tokens = %#v, want %d", body["max_tokens"], 1024)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "hello "},
				{"type": "tool_use", "id": "tool_1"},
				{"type": "text", "text": "from anthropic"},
			},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:          "secret",
		BaseURL:         server.URL,
		Model:           "claude-test",
		MaxOutputTokens: 1024,
		HTTPClient:      server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.Text != "hello from anthropic" {
		t.Fatalf("Text = %q, want %q", response.Text, "hello from anthropic")
	}
}

func TestGenerateRejectsUnsupportedToolUse(t *testing.T) {
	t.Parallel()

	runtime, err := New(context.Background(), Config{
		APIKey:  "secret",
		BaseURL: "https://api.anthropic.com/v1",
		Model:   "claude-test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
		Tools: []providers.ToolDefinition{{
			Name:        "read",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "tool use disabled") || !strings.Contains(err.Error(), "tool definitions") {
		t.Fatalf("Generate() error = %v, want explicit unsupported tool definitions error", err)
	}

	_, err = runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"file_path":"notes.txt"}`)}}},
			{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "tool use disabled") || !strings.Contains(err.Error(), "assistant tool-call messages") {
		t.Fatalf("Generate() error = %v, want explicit unsupported tool-call history error", err)
	}
}

func TestGenerateSendsSystemAndCustomInstructions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			System   string `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.System != "system prompt\n\nUser custom instructions:\ncustom prompt" {
			t.Fatalf("system = %q, want combined system prompt", body.System)
		}
		if len(body.Messages) != 1 {
			t.Fatalf("len(messages) = %d, want 1", len(body.Messages))
		}
		if body.Messages[0].Role != "user" || body.Messages[0].Content != "hello" {
			t.Fatalf("user message = %#v, want hello", body.Messages[0])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "claude-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runtime.Generate(context.Background(), providers.Request{
		SystemPrompt:       "system prompt",
		CustomInstructions: "custom prompt",
		Messages:           []providers.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestGenerateRejectsMissingText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "tool_use", "id": "tool_1"},
			},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "claude-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "empty assistant reply") {
		t.Fatalf("Generate() error = %v, want empty assistant reply", err)
	}
}

func TestGenerateReturnsAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "invalid api key"},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "claude-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("Generate() error = %v, want invalid api key", err)
	}
}

func TestGenerateStreamsTextDeltas(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("Accept = %q, want %q", got, "text/event-stream")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got := body["stream"]; got != true {
			t.Fatalf("stream = %#v, want true", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello \"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"world\"}}\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "claude-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var deltas []string
	response, err := runtime.Generate(providers.WithTextStream(context.Background(), func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	}), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.Text != "hello world" {
		t.Fatalf("Text = %q, want %q", response.Text, "hello world")
	}
	if !reflect.DeepEqual(deltas, []string{"hello ", "world"}) {
		t.Fatalf("streamed deltas = %#v, want %#v", deltas, []string{"hello ", "world"})
	}
}
