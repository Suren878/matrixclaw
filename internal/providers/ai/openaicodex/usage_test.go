package openaicodex

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageKeepsCachedInputInPrompt(t *testing.T) {
	stream := `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":700},"output_tokens":200,"output_tokens_details":{"reasoning_tokens":150},"total_tokens":1200}}}

`
	response, err := (&Runtime{RuntimeBase: providers.RuntimeBase{Model: "test-model"}}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	got := response.Usage
	if len(got.ProviderRaw) == 0 {
		t.Fatal("provider raw usage missing")
	}
	got.ProviderRaw = nil
	want := providers.Usage{PromptTokens: 1000, CacheReadTokens: 700, OutputTokens: 200, ReasoningTokens: 150}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage=%+v, want %+v", got, want)
	}
}
