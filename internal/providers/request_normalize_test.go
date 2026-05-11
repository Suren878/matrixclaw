package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeRequestKeepsInvalidToolCallsAndOrphanResults(t *testing.T) {
	t.Parallel()

	request := Request{
		Messages: []Message{
			{Role: "user", Content: "read files"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:        "call_bad",
					Name:      "read",
					Arguments: json.RawMessage(`{"file_path":"a"}{"file_path":"b"}`),
				}},
			},
			{Role: "tool", ToolCallID: "call_bad", Content: "invalid args"},
			{Role: "tool", ToolCallID: "call_missing", Content: "orphan result"},
			{Role: "user", Content: "continue"},
		},
	}

	normalized := NormalizeRequest(request, RuntimeProfile{ToolUseMode: ToolUseNative, ToolSchemaDialect: ToolSchemaJSONSchema})
	if len(normalized.Messages) != 5 {
		t.Fatalf("len(Messages) = %d, want 5", len(normalized.Messages))
	}
	if normalized.Messages[1].Role != "assistant" || len(normalized.Messages[1].ToolCalls) != 1 {
		t.Fatalf("Messages[1] = %#v, want assistant tool call", normalized.Messages[1])
	}
	if got := string(normalized.Messages[1].ToolCalls[0].Arguments); got != `{"file_path":"a"}{"file_path":"b"}` {
		t.Fatalf("Arguments = %q, want raw invalid arguments", got)
	}
	if normalized.Messages[2].Role != "tool" || normalized.Messages[2].ToolCallID != "call_bad" {
		t.Fatalf("Messages[2] = %#v, want paired tool result", normalized.Messages[2])
	}
	if normalized.Messages[3].Role != "tool" || normalized.Messages[3].ToolCallID != "call_missing" {
		t.Fatalf("Messages[3] = %#v, want orphan tool result preserved", normalized.Messages[3])
	}
	if normalized.Messages[4].Role != "user" || normalized.Messages[4].Content != "continue" {
		t.Fatalf("Messages[4] = %#v, want final user message", normalized.Messages[4])
	}
}

func TestNormalizeRequestKeepsValidToolCallPairs(t *testing.T) {
	t.Parallel()

	request := Request{
		Messages: []Message{
			{Role: "user", Content: "read"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:        "call_1",
					Name:      "read",
					Arguments: json.RawMessage(`{"file_path":"a"}`),
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		},
	}

	normalized := NormalizeRequest(request, RuntimeProfile{ToolUseMode: ToolUseNative, ToolSchemaDialect: ToolSchemaJSONSchema})
	if len(normalized.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(normalized.Messages))
	}
	if got := string(normalized.Messages[1].ToolCalls[0].Arguments); got != `{"file_path":"a"}` {
		t.Fatalf("Arguments = %s", got)
	}
	if normalized.Messages[2].Role != "tool" || normalized.Messages[2].ToolCallID != "call_1" {
		t.Fatalf("Messages[2] = %#v, want paired tool result", normalized.Messages[2])
	}
}

func TestNormalizeRequestSanitizesGeminiToolSchemas(t *testing.T) {
	t.Parallel()

	request := Request{
		Tools: []ToolDefinition{{
			Name: "read",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{"limit":{"type":"integer","minimum":1}},
				"additionalProperties":false
			}`),
		}},
	}

	normalized := NormalizeRequest(request, RuntimeProfile{ToolUseMode: ToolUseNative, ToolSchemaDialect: ToolSchemaGemini})
	if len(normalized.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(normalized.Tools))
	}
	var schema map[string]any
	if err := json.Unmarshal(normalized.Tools[0].InputSchema, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if _, ok := schema["additionalProperties"]; ok {
		t.Fatalf("schema still contains additionalProperties: %#v", schema)
	}
	props := schema["properties"].(map[string]any)
	limit := props["limit"].(map[string]any)
	if _, ok := limit["minimum"]; ok {
		t.Fatalf("nested schema still contains minimum: %#v", limit)
	}
}

func TestNormalizeRequestDisablesNativeToolsForTextOnlyProfiles(t *testing.T) {
	t.Parallel()

	request := Request{
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:        "call_1",
					Name:      "read",
					Arguments: json.RawMessage(`{"file_path":"a"}`),
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		},
		Tools: []ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}

	normalized := NormalizeRequest(request, RuntimeProfile{ToolUseMode: ToolUseDisabled})
	if len(normalized.Tools) != 0 {
		t.Fatalf("Tools = %#v, want none", normalized.Tools)
	}
	for _, message := range normalized.Messages {
		if len(message.ToolCalls) != 0 || strings.TrimSpace(message.ToolCallID) != "" || message.Role == "tool" {
			t.Fatalf("message still contains native tool data: %#v", message)
		}
	}
}

func TestProviderProfileRuntimeProfileDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerType string
		baseURL      string
		profile      RuntimeProfile
		want         RuntimeProfile
	}{
		{
			name:         "local openai compatible",
			providerType: TypeOpenAICompat,
			baseURL:      "http://127.0.0.1:11434/v1",
			want: RuntimeProfile{
				ToolUseMode:       ToolUseNative,
				ToolSchemaDialect: ToolSchemaJSONSchema,
			},
		},
		{
			name:         "gemini type",
			providerType: TypeGemini,
			baseURL:      "https://generativelanguage.googleapis.com/v1beta",
			want: RuntimeProfile{
				ToolUseMode:       ToolUseNative,
				ToolSchemaDialect: ToolSchemaGemini,
			},
		},
		{
			name:         "partial gemini override keeps gemini dialect default",
			providerType: TypeGemini,
			baseURL:      "https://generativelanguage.googleapis.com/v1beta",
			profile: RuntimeProfile{
				ToolUseMode: ToolUseDisabled,
			},
			want: RuntimeProfile{
				ToolUseMode:       ToolUseDisabled,
				ToolSchemaDialect: ToolSchemaGemini,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			profile := ProfileForProvider(tt.providerType)
			if got := profile.RuntimeProfileWithOverrides(tt.profile); got != tt.want {
				t.Fatalf("RuntimeProfileWithOverrides() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeOptionalToolUseModeRejectsPrompted(t *testing.T) {
	t.Parallel()

	if got := NormalizeOptionalToolUseMode(ToolUseMode("prompted")); got != "" {
		t.Fatalf("NormalizeOptionalToolUseMode(prompted) = %q, want empty unsupported mode", got)
	}
	if got := NormalizeOptionalToolUseMode(ToolUseDisabled); got != ToolUseDisabled {
		t.Fatalf("NormalizeOptionalToolUseMode(disabled) = %q, want disabled", got)
	}
}
