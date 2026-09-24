package anthropic

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func frame(eventType string, data string) string {
	return "event: " + eventType + "\ndata: " + data + "\n\n"
}

func toolStream(partialJSON string, stopReason string) string {
	return frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5,"output_tokens":1}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me read"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_a","name":"read","input":{}}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":`+partialJSON+`}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":1}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+stopReason+`","stop_sequence":null},"usage":{"output_tokens":9}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
}

func TestStreamAssemblesThinkingTextAndParallelToolCalls(t *testing.T) {
	stream := frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":100,"output_tokens":1}}}`) +
		frame("ping", `{"type":"ping"}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Read both "}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"files."}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-stream"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Reading."}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":1}`) +
		frame("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_a","name":"read","input":{}}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":2}`) +
		frame("content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_b","name":"ls","input":{}}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":3}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":42}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	var deltas []string
	ctx := providers.WithTextStream(context.Background(), func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	response, err := (&Runtime{model: "claude-test"}).decodeStream(ctx, strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "Reading." || strings.Join(deltas, "") != "Reading." || response.StopReason != providers.StopToolUse {
		t.Fatalf("response = %#v deltas = %q", response, deltas)
	}
	if len(response.ToolCalls) != 2 {
		t.Fatalf("tool calls = %#v", response.ToolCalls)
	}
	if call := response.ToolCalls[0]; call.ID != "toolu_a" || call.Name != "read" || string(call.Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("first call = %#v", call)
	}
	if call := response.ToolCalls[1]; call.ID != "toolu_b" || call.Name != "ls" || string(call.Arguments) != `{}` {
		t.Fatalf("second call = %#v", call)
	}
	if len(response.Reasoning) != 1 || response.Reasoning[0] != (providers.ReasoningBlock{Text: "Read both files.", Signature: "sig-stream"}) {
		t.Fatalf("reasoning = %#v", response.Reasoning)
	}
	if response.Usage.PromptTokens != 105 || response.Usage.CacheReadTokens != 100 || response.Usage.OutputTokens != 42 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestStreamRejectsMalformedToolInput(t *testing.T) {
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(toolStream(`"{\"path\":"`, "tool_use")))
	if !errors.Is(err, providers.ErrMalformedToolCall) || len(response.ToolCalls) != 0 {
		t.Fatalf("response = %#v err = %v", response, err)
	}
}

func TestStreamDropsToolCallTruncatedByMaxTokens(t *testing.T) {
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(toolStream(`"{\"path\":"`, "max_tokens")))
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != providers.StopMaxTokens || response.Text != "Let me read" || len(response.ToolCalls) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestStreamDoesNotCompleteBeforeMessageStop(t *testing.T) {
	partial := frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`)
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(partial)); !errors.Is(err, providers.ErrIncompleteResponse) {
		t.Fatalf("truncated stream error = %v", err)
	}
	complete := partial + frame("message_stop", `{"type":"message_stop"}`)
	response, err := (&Runtime{}).decodeStream(context.Background(), io.MultiReader(strings.NewReader(complete), failIfRead{t}))
	if err != nil || response.Text != "Hello" || response.StopReason != providers.StopEndTurn {
		t.Fatalf("response = %#v err = %v", response, err)
	}
}

func TestStreamErrorEventFails(t *testing.T) {
	stream := frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5}}}`) +
		frame("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream)); err == nil || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("error = %v", err)
	}
}

func TestGenerateStreamsToolCallsOverSSE(t *testing.T) {
	runtime, sent := newTestRuntime(t, toolStream(`"{\"path\":\"a.go\"}"`, "tool_use"))
	var streamed strings.Builder
	ctx := providers.WithTextStream(context.Background(), func(delta string) error {
		streamed.WriteString(delta)
		return nil
	})
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "Read a.go"}}, Tools: testTools})
	if err != nil {
		t.Fatal(err)
	}
	if !decodeSent(t, sent(0)).Stream {
		t.Fatalf("request did not ask for a stream: %s", sent(0))
	}
	if streamed.String() != "Let me read" || len(response.ToolCalls) != 1 || string(response.ToolCalls[0].Arguments) != `{"path":"a.go"}` {
		t.Fatalf("response = %#v streamed = %q", response, streamed.String())
	}
}

type failIfRead struct{ t *testing.T }

func (r failIfRead) Read([]byte) (int, error) {
	r.t.Error("read beyond message_stop")
	return 0, io.ErrUnexpectedEOF
}
