package gemini

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestRepeatedGeminiToolInLaterTurnGetsDistinctCallID(t *testing.T) {
	runtime := &Runtime{}
	request := providers.Request{RunID: "same-run"}
	body := []byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"inspect","args":{"path":"a"}}}]}}]}`)
	first, err := runtime.decodeGenerateResponse(request, body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.decodeGenerateResponse(request, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || len(second.ToolCalls) != 1 || first.ToolCalls[0].ID == "" || first.ToolCalls[0].ID == second.ToolCalls[0].ID {
		t.Fatalf("tool IDs collide across turns: %#v %#v", first.ToolCalls, second.ToolCalls)
	}
}
