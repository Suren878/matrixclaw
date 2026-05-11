package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestGenerateSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/chat/completions")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q, want %q", got, "Bearer secret")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "gpt-test" {
			t.Fatalf("model = %#v, want %q", body["model"], "gpt-test")
		}
		if body["max_tokens"] != float64(2048) {
			t.Fatalf("max_tokens = %#v, want %d", body["max_tokens"], 2048)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "hello from openai-compatible",
					},
				},
			},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:          "secret",
		BaseURL:         server.URL,
		Model:           "gpt-test",
		MaxOutputTokens: 2048,
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
	if response.Text != "hello from openai-compatible" {
		t.Fatalf("Text = %q, want %q", response.Text, "hello from openai-compatible")
	}
}

func TestNewRuntimeProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cfg           Config
		wantProfile   providers.RuntimeProfile
		wantReasoning string
	}{
		{
			name: "OpenAI-compatible provider",
			cfg: Config{
				APIKey:          "secret",
				BaseURL:         "https://api.example.com/v1",
				Model:           "gpt-test",
				ReasoningEffort: "high",
			},
			wantProfile: providers.RuntimeProfile{
				ToolUseMode:       providers.ToolUseNative,
				ToolSchemaDialect: providers.ToolSchemaJSONSchema,
			},
			wantReasoning: "high",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runtime, err := New(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			got := runtime.(*Runtime)
			if got.profile != tt.wantProfile {
				t.Fatalf("profile = %#v, want %#v", got.profile, tt.wantProfile)
			}
			if profile := got.RuntimeProfile(); profile != tt.wantProfile {
				t.Fatalf("RuntimeProfile() = %#v, want %#v", profile, tt.wantProfile)
			}
			if got.reasoningEffort != tt.wantReasoning {
				t.Fatalf("reasoningEffort = %q, want %q", got.reasoningEffort, tt.wantReasoning)
			}
		})
	}
}

func TestGenerateUsesMaxCompletionTokensForGPT5Models(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["max_completion_tokens"] != float64(2048) {
			t.Fatalf("max_completion_tokens = %#v, want %d", body["max_completion_tokens"], 2048)
		}
		if _, ok := body["max_tokens"]; ok {
			t.Fatalf("max_tokens unexpectedly present: %#v", body["max_tokens"])
		}
		if got := body["reasoning_effort"]; got != "high" {
			t.Fatalf("reasoning_effort = %#v, want high", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:          "secret",
		BaseURL:         server.URL,
		Model:           "gpt-5.4-nano",
		MaxOutputTokens: 2048,
		ReasoningEffort: "high",
		HTTPClient:      server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestGenerateSendsSystemAndCustomInstructions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("len(messages) = %d, want 2", len(body.Messages))
		}
		if body.Messages[0].Role != "system" || body.Messages[0].Content != "system prompt\n\nUser custom instructions:\ncustom prompt" {
			t.Fatalf("system message = %#v, want combined system prompt", body.Messages[0])
		}
		if body.Messages[1].Role != "user" || body.Messages[1].Content != "hello" {
			t.Fatalf("user message = %#v, want hello", body.Messages[1])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gpt-test",
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

func TestGenerateReturnsResponseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "empty choices",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
			},
			wantErr: "empty choices",
		},
		{
			name: "api error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"message": "bad request"},
				})
			},
			wantErr: "bad request",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tt.handler)
			defer server.Close()

			runtime, err := New(context.Background(), Config{
				APIKey:     "secret",
				BaseURL:    server.URL,
				Model:      "gpt-test",
				HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			_, err = runtime.Generate(context.Background(), providers.Request{
				Messages: []providers.Message{{Role: "user", Content: "hello"}},
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Generate() error = %v, want %s", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateRetriesTransientServerErrorForToolResult(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("len(messages) = %d, want 2", len(body.Messages))
		}
		if got := body.Messages[0]["role"]; got != "assistant" {
			t.Fatalf("messages[0].role = %#v, want assistant tool history", got)
		}
		if _, ok := body.Messages[0]["tool_calls"]; !ok {
			t.Fatalf("messages[0] should include native tool calls: %#v", body.Messages[0])
		}
		if got := body.Messages[1]["role"]; got != "tool" {
			t.Fatalf("messages[1].role = %#v, want tool result", got)
		}
		if got := body.Messages[1]["tool_call_id"]; got != "call_1" {
			t.Fatalf("messages[1].tool_call_id = %#v, want call_1", got)
		}

		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    500,
				"message": "Internal error encountered",
				"status":  "INTERNAL",
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"content": "done"},
			}},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gpt-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "edit", Arguments: json.RawMessage(`{"file_path":"notes.txt","old_string":"a","new_string":"b"}`)}}},
			{Role: "tool", ToolCallID: "call_1", Content: "File edited: notes.txt"},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.Text != "done" {
		t.Fatalf("Text = %q, want done", response.Text)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestGenerateReturnsToolCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		toolsBody, ok := body["tools"].([]any)
		if !ok || len(toolsBody) != 1 {
			t.Fatalf("tools = %#v, want one tool definition", body["tools"])
		}
		if got := body["tool_choice"]; got != "auto" {
			t.Fatalf("tool_choice = %#v, want auto", got)
		}
		if got := body["reasoning_effort"]; got != nil {
			t.Fatalf("reasoning_effort = %#v, want omitted for tools", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q, want application/json", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "read",
							"arguments": `{"file_path":"notes.txt"}`,
						},
					}},
				},
			}},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:          "secret",
		BaseURL:         server.URL,
		Model:           "gpt-test",
		ReasoningEffort: "high",
		HTTPClient:      server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response, err := runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "inspect"}},
		Tools: []providers.ToolDefinition{{
			Name:        "read",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want one tool call", response.ToolCalls)
	}
	if response.ToolCalls[0].Name != "read" {
		t.Fatalf("ToolCalls[0].Name = %q, want read", response.ToolCalls[0].Name)
	}
	if string(response.ToolCalls[0].Arguments) != `{"file_path":"notes.txt"}` {
		t.Fatalf("ToolCalls[0].Arguments = %s", string(response.ToolCalls[0].Arguments))
	}
}

func TestGenerateSendsNativeToolMessages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("len(messages) = %d, want 2", len(body.Messages))
		}
		if got, exists := body.Messages[0]["content"]; !exists || got != "" {
			t.Fatalf("assistant tool-call content = %#v exists=%v, want empty string", got, exists)
		}
		if got := body.Messages[1]["role"]; got != "tool" {
			t.Fatalf("messages[1].role = %#v, want tool", got)
		}
		if got := body.Messages[1]["tool_call_id"]; got != "call_1" {
			t.Fatalf("messages[1].tool_call_id = %#v, want call_1", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": "done",
				},
			}},
		})
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gpt-test",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = runtime.Generate(context.Background(), providers.Request{
		Messages: []providers.Message{
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"file_path":"notes.txt"}`)}}},
			{Role: "tool", ToolCallID: "call_1", Content: "<file>ok</file>"},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestGenerateKeepsOpenAICompatibleBrandsOnNativeToolMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "openai", baseURL: "https://api.openai.com/v1"},
		{name: "deepseek", baseURL: "https://api.deepseek.com/v1"},
		{name: "xai", baseURL: "https://api.x.ai/v1"},
		{name: "zai", baseURL: "https://api.z.ai/api/paas/v4"},
		{name: "kimi", baseURL: "https://api.moonshot.ai/v1"},
		{name: "aihubmix", baseURL: "https://api.aihubmix.com/v1"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []map[string]any `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if len(body.Messages) != 2 {
					t.Fatalf("len(messages) = %d, want 2", len(body.Messages))
				}
				if got := body.Messages[0]["role"]; got != "assistant" {
					t.Fatalf("messages[0].role = %#v, want assistant", got)
				}
				if _, ok := body.Messages[0]["tool_calls"]; !ok {
					t.Fatalf("messages[0].tool_calls missing for native OpenAI-compatible tool history: %#v", body.Messages[0])
				}
				if got := body.Messages[1]["role"]; got != "tool" {
					t.Fatalf("messages[1].role = %#v, want tool", got)
				}
				if got := body.Messages[1]["tool_call_id"]; got != "call_1" {
					t.Fatalf("messages[1].tool_call_id = %#v, want call_1", got)
				}

				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []map[string]any{{
						"message": map[string]any{
							"content": "done",
						},
					}},
				})
			}))
			defer server.Close()

			runtime, err := New(context.Background(), Config{
				APIKey:     "secret",
				BaseURL:    server.URL,
				Model:      "gpt-test",
				HTTPClient: server.Client(),
				Profile:    providers.ProfileForProvider(providers.TypeOpenAICompat),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			_, err = runtime.Generate(context.Background(), providers.Request{
				Messages: []providers.Message{
					{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"file_path":"notes.txt"}`)}}},
					{Role: "tool", ToolCallID: "call_1", Content: "<file>ok</file>"},
				},
			})
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
		})
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
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gpt-test",
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

func TestGenerateStreamsToolCalls(t *testing.T) {
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
		if _, ok := body["tools"].([]any); !ok {
			t.Fatalf("tools = %#v, want tools in streamed request", body["tools"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"file_path\\\":\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"notes.txt\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	runtime, err := New(context.Background(), Config{
		APIKey:     "secret",
		BaseURL:    server.URL,
		Model:      "gpt-test",
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
		Messages: []providers.Message{{Role: "user", Content: "inspect"}},
		Tools: []providers.ToolDefinition{{
			Name:        "read",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(deltas) != 0 {
		t.Fatalf("streamed text deltas = %#v, want none for tool call", deltas)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v, want one tool call", response.ToolCalls)
	}
	if response.ToolCalls[0].ID != "call_1" || response.ToolCalls[0].Name != "read" {
		t.Fatalf("ToolCalls[0] = %#v, want call_1/read", response.ToolCalls[0])
	}
	if string(response.ToolCalls[0].Arguments) != `{"file_path":"notes.txt"}` {
		t.Fatalf("ToolCalls[0].Arguments = %s", string(response.ToolCalls[0].Arguments))
	}
}
