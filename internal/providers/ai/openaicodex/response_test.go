package openaicodex

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestCompletedStreamUsesFinalTextAndOrderedTools(t *testing.T) {
	runtime := &Runtime{model: "test-model"}
	var preview strings.Builder
	ctx := providers.WithTextStream(context.Background(), func(delta string) error {
		preview.WriteString(delta)
		return nil
	})
	stream := `data: {"type":"response.output_text.delta","delta":"provisional"}

data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"item-b","call_id":"b","name":"second","arguments":"{"}}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Final text"}]},{"type":"function_call","call_id":"a","name":"first","arguments":"{\"id\":9007199254740993}"},{"type":"function_call","call_id":"b","name":"second","arguments":"{}"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}

`
	response, err := runtime.decodeStream(ctx, io.MultiReader(strings.NewReader(stream), unexpectedRead{t}))
	if err != nil {
		t.Fatal(err)
	}
	if preview.String() != "provisional" || response.Text != "Final text" {
		t.Fatalf("preview=%q final=%q", preview.String(), response.Text)
	}
	if len(response.ToolCalls) != 2 || response.ToolCalls[0].ID != "a" || response.ToolCalls[1].ID != "b" {
		t.Fatalf("ordered calls=%+v", response.ToolCalls)
	}
	if string(response.ToolCalls[0].Arguments) != `{"id":9007199254740993}` || response.Usage.TotalTokens != 15 {
		t.Fatalf("response=%+v", response)
	}
}

type unexpectedRead struct{ t *testing.T }

func (r unexpectedRead) Read([]byte) (int, error) {
	r.t.Error("decoder read past response.completed")
	return 0, io.ErrUnexpectedEOF
}

func TestStreamRejectsMissingCompletionAndTerminalFailures(t *testing.T) {
	for _, tc := range []struct {
		name, stream, want string
		incomplete         bool
	}{
		{"empty", "", "incomplete", true},
		{"partial text", `{"type":"response.output_text.delta","delta":"Partial"}`, "incomplete", true},
		{"partial tool", `{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call","name":"write","arguments":"{}"}}`, "incomplete", true},
		{"unrelated completed", `{"type":"response.custom.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"wrong"}]}]}}`, "incomplete", true},
		{"done marker", "[DONE]", "incomplete", true},
		{"flat error", `{"type":"error","message":"stream failed","code":"server_error"}`, "stream failed", false},
		{"nested error", `{"type":"error","error":{"message":"nested failure"}}`, "nested failure", false},
		{"failed", `{"type":"response.failed","response":{"error":{"message":"upstream failed"}}}`, "upstream failed", false},
		{"truncated", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, "max_output_tokens", false},
		{"cancelled", `{"type":"response.cancelled","response":{}}`, "cancelled", false},
		{"completed with error", `{"type":"response.completed","response":{"status":"failed","error":{"message":"hidden failure"}}}`, "hidden failure", false},
		{"invalid tool arguments", `{"type":"response.completed","response":{"output":[{"type":"function_call","call_id":"c","name":"write","arguments":"{"}]}}`, "invalid arguments", false},
		{"missing call id", `{"type":"response.completed","response":{"output":[{"type":"function_call","name":"write","arguments":"{}"}]}}`, "call_id", false},
		{"missing tool name", `{"type":"response.completed","response":{"output":[{"type":"function_call","call_id":"c","arguments":"{}"}]}}`, "name", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := ""
			if tc.stream != "" {
				stream = "data: " + tc.stream + "\n\n"
			}
			response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
			if err == nil || (!tc.incomplete && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
			if tc.incomplete && !errors.Is(err, providers.ErrIncompleteResponse) {
				t.Fatalf("missing typed incomplete error: %v", err)
			}
			if response.Text != "" || len(response.ToolCalls) != 0 {
				t.Fatalf("failed response returned executable output: %+v", response)
			}
		})
	}
}

func TestResponseDecodingHandlesRefusalAndEmptyOutput(t *testing.T) {
	runtime := &Runtime{}
	raw := `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"Cannot help with that."}]}]}`
	jsonResponse, err := runtime.decodeResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	streamResponse, err := runtime.decodeStream(context.Background(), strings.NewReader("event: response.completed\ndata: {\"response\":"+raw+"}\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if jsonResponse.Text != "Cannot help with that." || streamResponse.Text != jsonResponse.Text {
		t.Fatalf("json=%q stream=%q", jsonResponse.Text, streamResponse.Text)
	}
	if _, err := runtime.decodeResponse([]byte(`{"status":"completed","output":[]}`)); !errors.Is(err, providers.ErrEmptyResponse) {
		t.Fatalf("empty response error=%v", err)
	}
}

func TestStreamPropagatesCancellationAndSinkFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Runtime{}).decodeStream(ctx, strings.NewReader("")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
	sinkErr := errors.New("storage unavailable")
	ctx = providers.WithTextStream(context.Background(), func(string) error { return sinkErr })
	if _, err := (&Runtime{}).decodeStream(ctx, strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")); !errors.Is(err, sinkErr) {
		t.Fatalf("sink error=%v", err)
	}
}
