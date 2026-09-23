package gemini

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageCountsThoughtsAsOutput(t *testing.T) {
	stream := `data: {"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":600,"candidatesTokenCount":100,"thoughtsTokenCount":40,"totalTokenCount":1140}}` + "\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	got := response.Usage
	if len(got.ProviderRaw) == 0 {
		t.Fatal("provider raw usage missing")
	}
	got.ProviderRaw = nil
	want := providers.Usage{PromptTokens: 1000, CacheReadTokens: 600, OutputTokens: 140, ReasoningTokens: 40}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage=%+v, want %+v", got, want)
	}
}
