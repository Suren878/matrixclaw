package gemini

import (
	"context"
	"strings"
	"testing"
)

func TestRepeatedGeminiToolInLaterTurnGetsDistinctCallID(t *testing.T) {
	runtime := &Runtime{}
	stream := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"inspect","args":{"path":"a"}}}]},"finishReason":"STOP"}]}` + "\n\n"
	first, err := runtime.decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || len(second.ToolCalls) != 1 || first.ToolCalls[0].ID == "" || first.ToolCalls[0].ID == second.ToolCalls[0].ID {
		t.Fatalf("tool IDs collide across turns: %#v %#v", first.ToolCalls, second.ToolCalls)
	}
}
