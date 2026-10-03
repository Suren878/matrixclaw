package openaicodex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestTerminalStatusBecomesStopReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event string
		want  providers.StopReason
		text  string
		calls int
	}{
		{"completed text", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Done"}]}]}}`, providers.StopEndTurn, "Done", 0},
		{"completed tools", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","call_id":"a","name":"read","arguments":"{}"}]}}`, providers.StopToolUse, "", 1},
		{"output limit keeps text", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":" Partial "}]}]}}`, providers.StopMaxTokens, " Partial ", 0},
		{"output limit drops cut call", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","call_id":"a","name":"read","arguments":"{}"},{"type":"function_call","call_id":"b","name":"read","arguments":"{\"pa"}]}}`, providers.StopMaxTokens, "", 1},
		{"output limit spent on reasoning", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, providers.StopMaxTokens, "", 0},
		{"content filter", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"content_filter"}}}`, providers.StopContentFilter, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader("data: "+tc.event+"\n\n"))
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || response.Text != tc.text || len(response.ToolCalls) != tc.calls {
				t.Fatalf("response=%+v, want stop=%q text=%q calls=%d", response, tc.want, tc.text, tc.calls)
			}
		})
	}
}

func TestPayloadCarriesCacheKeyAndToolChoiceButNoOutputLimit(t *testing.T) {
	payload := (&Runtime{RuntimeBase: providers.RuntimeBase{Model: "gpt-5.4"}}).responsesPayload(providers.Request{
		CacheKey:        "session-1",
		ToolChoice:      providers.ToolChoiceNone,
		MaxOutputTokens: 4000,
		Tools:           []providers.ToolDefinition{{Name: "read"}},
		Messages:        []providers.Message{{Role: "user", Content: "hello"}},
	})
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["prompt_cache_key"] != "session-1" || wire["tool_choice"] != "none" {
		t.Fatalf("wire=%s", raw)
	}
	if _, sent := wire["max_output_tokens"]; sent {
		t.Fatalf("max_output_tokens sent to the Codex backend: %s", raw)
	}
}
