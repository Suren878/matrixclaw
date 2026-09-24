package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestOneToolStepIsSentBackAsOneModelAndOneUserContent(t *testing.T) {
	payloads := capturePayloads(t, providers.Request{Messages: []providers.Message{
		{Role: "user", Content: "Compare a and b."},
		{Role: "assistant", Content: "Reading both files.", Reasoning: []providers.ReasoningBlock{{Signature: "sig-a"}}, ToolCalls: []providers.ToolCall{
			{ID: "g1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
			{ID: "g2", Name: "read", Arguments: json.RawMessage(`{"path":"b"}`)},
		}},
		{Role: "tool", ToolCallID: "g1", Content: "A"},
		{Role: "tool", ToolCallID: "g2", Content: "no such file", IsError: true},
	}})
	contents := payloads[0].Contents
	if len(contents) != 3 {
		t.Fatalf("contents=%+v, want user, model, user", contents)
	}
	model, results := contents[1], contents[2]
	if model.Role != "model" || len(model.Parts) != 3 || model.Parts[0].Text != "Reading both files." {
		t.Fatalf("model content=%+v", model)
	}
	if model.Parts[1].FunctionCall == nil || model.Parts[1].ThoughtSignature != "sig-a" || model.Parts[2].ThoughtSignature != "" {
		t.Fatalf("thought signature must ride on the first call only: %+v", model.Parts)
	}
	if results.Role != "user" || len(results.Parts) != 2 {
		t.Fatalf("function responses=%+v, want both in one content", results)
	}
	first, second := results.Parts[0].FunctionResponse, results.Parts[1].FunctionResponse
	if first == nil || first.Name != "read" || first.Response["content"] != "A" {
		t.Fatalf("first response=%+v", first)
	}
	if second == nil || second.Response["error"] != "no such file" {
		t.Fatalf("failed call must be reported as an error: %+v", second)
	}
}

func TestThoughtSignatureIsReturnedAsReasoning(t *testing.T) {
	stream := `data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{"path":"a"}},"thoughtSignature":"sig-a"},{"functionCall":{"name":"read","args":{"path":"b"}}}]},"finishReason":"STOP"}]}` + "\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 2 || len(response.Reasoning) != 1 || response.Reasoning[0].Signature != "sig-a" {
		t.Fatalf("response=%+v", response)
	}
}

func TestUnsignedToolStepGetsTheDocumentedDummySignature(t *testing.T) {
	payloads := capturePayloads(t, providers.Request{Messages: []providers.Message{
		{Role: "user", Content: "Compare a and b."},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "g1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
			{ID: "g2", Name: "read", Arguments: json.RawMessage(`{"path":"b"}`)},
		}},
		{Role: "tool", ToolCallID: "g1", Content: "A"},
		{Role: "tool", ToolCallID: "g2", Content: "B"},
	}})
	parts := payloads[0].Contents[1].Parts
	if len(parts) != 2 || parts[0].ThoughtSignature != "skip_thought_signature_validator" || parts[1].ThoughtSignature != "" {
		t.Fatalf("model parts=%+v, want the dummy signature on the first call only", parts)
	}
}
