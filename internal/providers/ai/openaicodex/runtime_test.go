package openaicodex

import (
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestResponsesItemsFromToolMessageIncludesRequiredOutput(t *testing.T) {
	t.Parallel()

	items := responsesItemsFromMessage(providers.Message{
		Role:       "tool",
		ToolCallID: "call_1",
	})
	if len(items) != 1 {
		t.Fatalf("len(responsesItemsFromMessage()) = %d, want 1", len(items))
	}
	if items[0].Type != "function_call_output" || items[0].CallID != "call_1" {
		t.Fatalf("item = %#v, want function_call_output for call_1", items[0])
	}
	if items[0].Output == "" {
		t.Fatal("function_call_output output is empty")
	}

	raw, err := json.Marshal(items[0])
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatalf("decode encoded item: %v", err)
	}
	if _, ok := encoded["output"]; !ok {
		t.Fatalf("encoded item %s is missing required output", string(raw))
	}
}

func TestResponsesItemsFromAssistantToolCallIncludesArguments(t *testing.T) {
	t.Parallel()

	items := responsesItemsFromMessage(providers.Message{
		Role: "assistant",
		ToolCalls: []providers.ToolCall{{
			ID:   "call_1",
			Name: "bash",
		}},
	})
	if len(items) != 1 {
		t.Fatalf("len(responsesItemsFromMessage()) = %d, want 1", len(items))
	}
	if items[0].Arguments != "{}" {
		t.Fatalf("items[0].Arguments = %q, want {}", items[0].Arguments)
	}
}
