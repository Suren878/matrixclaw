package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func sseStream(chunks ...string) string {
	var out strings.Builder
	for _, chunk := range chunks {
		out.WriteString("data: " + chunk + "\n\n")
	}
	return out.String() + "data: [DONE]\n\n"
}

func TestStreamFinishReasonBecomesStopReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream string
		want   providers.StopReason
		text   string
		calls  []string
	}{
		{"stop", sseStream(`{"choices":[{"delta":{"content":"Done"},"finish_reason":"stop"}]}`), providers.StopEndTurn, "Done", nil},
		{"stop with tool calls", sseStream(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"stop"}]}`), providers.StopToolUse, "", []string{"c1"}},
		{"length keeps text", sseStream(`{"choices":[{"delta":{"content":" Partial "},"finish_reason":"length"}]}`), providers.StopMaxTokens, " Partial ", nil},
		{"length drops truncated call", sseStream(`{"choices":[{"delta":{"content":"Reading","tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{}"}},{"index":1,"id":"c2","function":{"name":"read","arguments":"{\"pa"}}]},"finish_reason":"length"}]}`), providers.StopMaxTokens, "Reading", []string{"c1"}},
		{"length with nothing", sseStream(`{"choices":[{"delta":{},"finish_reason":"length"}]}`), providers.StopMaxTokens, "", nil},
		{"content filter", sseStream(`{"choices":[{"delta":{},"finish_reason":"content_filter"}]}`), providers.StopContentFilter, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := (&Runtime{model: "test"}).decodeStream(context.Background(), strings.NewReader(tc.stream))
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || response.Text != tc.text || len(response.ToolCalls) != len(tc.calls) {
				t.Fatalf("response=%+v, want stop=%q text=%q calls=%v", response, tc.want, tc.text, tc.calls)
			}
			for i, id := range tc.calls {
				if response.ToolCalls[i].ID != id {
					t.Fatalf("calls=%+v, want %v", response.ToolCalls, tc.calls)
				}
			}
		})
	}
}

func TestJSONFinishReasonLengthIsNotAnError(t *testing.T) {
	response, err := (&Runtime{model: "test"}).decodeChatResponse([]byte(`{"choices":[{"message":{"content":"Partial"},"finish_reason":"length"}]}`))
	if err != nil || response.StopReason != providers.StopMaxTokens || response.Text != "Partial" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestFinishReasonMapping(t *testing.T) {
	for _, tc := range []struct {
		name         string
		finishReason string
		toolCalls    string
		wantStop     providers.StopReason
		wantErr      error
	}{
		{"stop", "stop", "", providers.StopEndTurn, nil},
		{"tool_calls", "tool_calls", `[{"id":"c1","function":{"name":"read","arguments":"{}"}}]`, providers.StopToolUse, nil},
		{"tool_calls with zero calls", "tool_calls", "", providers.StopEndTurn, nil},
		{"function_call", "function_call", `[{"id":"c1","function":{"name":"read","arguments":"{}"}}]`, providers.StopToolUse, nil},
		{"max_tokens", "max_tokens", "", providers.StopMaxTokens, nil},
		{"length", "length", "", providers.StopMaxTokens, nil},
		{"content_filter", "content_filter", "", providers.StopContentFilter, nil},
		{"null/empty", "", "", providers.StopEndTurn, nil},
		{"openrouter error", "error", "", "", providers.ErrIncompleteResponse},
		{"deepseek insufficient resource", "insufficient_system_resource", "", "", providers.ErrIncompleteResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			toolCallsJSON := tc.toolCalls
			if toolCallsJSON == "" {
				toolCallsJSON = "null"
			}
			body := fmt.Sprintf(`{"choices":[{"message":{"content":"hi","tool_calls":%s},"finish_reason":%q}]}`, toolCallsJSON, tc.finishReason)
			response, err := (&Runtime{model: "test"}).decodeChatResponse([]byte(body))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err=%v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err=%v", err)
			}
			if response.StopReason != tc.wantStop {
				t.Fatalf("stop=%q, want %q", response.StopReason, tc.wantStop)
			}
		})
	}
}
