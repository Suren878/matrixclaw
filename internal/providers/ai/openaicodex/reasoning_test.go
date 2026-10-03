package openaicodex

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestEncryptedReasoningIsRequestedAndReturned(t *testing.T) {
	runtime := &Runtime{RuntimeBase: providers.RuntimeBase{Model: "gpt-5.4"}, reasoningEffort: "medium"}
	payload := runtime.responsesPayload(providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if !slices.Contains(payload.Include, "reasoning.encrypted_content") {
		t.Fatalf("include=%v", payload.Include)
	}
	response, err := runtime.decodeResponse([]byte(`{"status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plan"}],"encrypted_content":"enc-1"},{"type":"function_call","call_id":"a","name":"read","arguments":"{}"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []providers.ReasoningBlock{{Text: "Plan", RedactedData: "enc-1"}}
	if !slices.Equal(response.Reasoning, want) {
		t.Fatalf("reasoning=%+v, want %+v", response.Reasoning, want)
	}
}

func TestReasoningIsReplayedBeforeCommentaryAndCalls(t *testing.T) {
	payload := (&Runtime{RuntimeBase: providers.RuntimeBase{Model: "gpt-5.4"}}).responsesPayload(providers.Request{Messages: []providers.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "Checking.", Reasoning: []providers.ReasoningBlock{{RedactedData: "enc-1"}}, ToolCalls: []providers.ToolCall{{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolCallID: "a", Content: "file"},
	}})
	raw, err := json.Marshal(payload.Input)
	if err != nil {
		t.Fatal(err)
	}
	var input []map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, item := range input {
		types = append(types, item["type"].(string))
	}
	want := []string{"message", "reasoning", "message", "function_call", "function_call_output"}
	if !slices.Equal(types, want) {
		t.Fatalf("input=%s, want item types %v", raw, want)
	}
	reasoning := input[1]
	if reasoning["encrypted_content"] != "enc-1" || reasoning["id"] != nil {
		t.Fatalf("reasoning item=%v", reasoning)
	}
	if summary, ok := reasoning["summary"].([]any); !ok || len(summary) != 0 {
		t.Fatalf("reasoning item needs an empty summary: %v", reasoning)
	}
}
