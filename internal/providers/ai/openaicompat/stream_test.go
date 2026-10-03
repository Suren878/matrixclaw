package openaicompat

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestDecodeStreamRequiresCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, frames string
		wantErr      error
		wantText     string
	}{
		{"truncated", `data: {"choices":[{"delta":{"content":"Partial"}}]}` + "\n\n", providers.ErrIncompleteResponse, ""},
		{"done", `data: {"choices":[{"delta":{"content":"Complete"}}]}` + "\n\ndata: [DONE]\n\n", nil, "Complete"},
		{"finish-reason", `data: {"choices":[{"delta":{"content":"Complete"},"finish_reason":"stop"}]}` + "\n\n", nil, "Complete"},
		{"empty", "data: [DONE]\n\n", providers.ErrEmptyResponse, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := (&Runtime{RuntimeBase: providers.RuntimeBase{Model: "test"}}).decodeStream(context.Background(), strings.NewReader(tc.frames))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v, want %v", err, tc.wantErr)
			}
			if response.Text != tc.wantText {
				t.Fatalf("text=%q", response.Text)
			}
		})
	}
}

func TestDecodeStreamRejectsErrorAndIncompleteToolArguments(t *testing.T) {
	for _, frames := range []string{
		`data: {"error":{"message":"upstream failed"}}` + "\n\ndata: [DONE]\n\n",
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"mutate","arguments":"{\"path\":"}}]}}]}` + "\n\ndata: [DONE]\n\n",
		`data: {"choices":[{"delta":{"content":"Commentary","tool_calls":[{"index":0,"id":"call-1","function":{"arguments":"{}"}}]}}]}` + "\n\ndata: [DONE]\n\n",
	} {
		response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(frames))
		if err == nil || len(response.ToolCalls) != 0 {
			t.Fatalf("unsafe success: response=%#v error=%v", response, err)
		}
	}
}

func TestJSONResponseRejectsMalformedToolsAlongsideText(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"message":{"content":"Commentary","tool_calls":[{"id":"call","function":{"name":"mutate","arguments":"{"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"content":"Commentary","tool_calls":[{"id":"call","function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	} {
		response, err := (&Runtime{}).decodeChatResponse([]byte(body))
		if err == nil || response.Text != "" || len(response.ToolCalls) != 0 {
			t.Fatalf("malformed tool treated as successful reply: response=%+v err=%v", response, err)
		}
	}
}

func TestDecodeStreamKeepsSparseToolIndicesInOrder(t *testing.T) {
	frames := `data: {"choices":[{"delta":{"tool_calls":[{"index":5,"id":"five","function":{"name":"inspect5","arguments":"{}"}},{"index":2,"id":"two","function":{"name":"inspect2","arguments":"{}"}}]}}]}` + "\n\ndata: [DONE]\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(frames))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 2 || response.ToolCalls[0].Name != "inspect2" || response.ToolCalls[1].Name != "inspect5" {
		t.Fatalf("tools=%#v", response.ToolCalls)
	}
}

type failIfRead struct{ t *testing.T }

func (r failIfRead) Read([]byte) (int, error) {
	r.t.Error("read beyond terminal event")
	return 0, io.ErrUnexpectedEOF
}

func TestDecodeStreamStopsAtDoneWithoutWaitingForConnectionClose(t *testing.T) {
	frames := `data: {"choices":[{"delta":{"content":"Done"}}]}` + "\n\ndata: [DONE]\n\n"
	_, err := (&Runtime{}).decodeStream(context.Background(), io.MultiReader(strings.NewReader(frames), failIfRead{t}))
	if err != nil {
		t.Fatal(err)
	}
}

// Kimi numbers calls per conversation, so two responses can both carry
// functions.read:0; each response's calls get IDs of their own.
func TestEachResponseGivesItsToolCallsIDsOfTheirOwn(t *testing.T) {
	frames := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"functions.read:0","function":{"name":"read","arguments":"{}"}},{"index":1,"id":"functions.read:0","function":{"name":"read","arguments":"{}"}},{"index":2,"id":"functions.read:1","function":{"name":"read","arguments":"{}"}}]}}]}` + "\n\ndata: [DONE]\n\n"
	first, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(frames))
	if err != nil {
		t.Fatal(err)
	}
	second, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(frames))
	if err != nil {
		t.Fatal(err)
	}
	calls := first.ToolCalls
	if len(calls) != 3 || calls[0].ID != calls[1].ID || calls[0].ID == calls[2].ID || len(calls[0].ID) > 40 {
		t.Fatalf("calls = %+v, want a repeated call to keep one ID and another call its own", calls)
	}
	if second.ToolCalls[0].ID == calls[0].ID {
		t.Fatalf("both responses gave their call the ID %q", calls[0].ID)
	}
}
