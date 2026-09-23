package gemini

import (
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageCountsThoughtsAsOutput(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":600,"candidatesTokenCount":100,"thoughtsTokenCount":40,"totalTokenCount":1140}}`)
	response, err := (&Runtime{}).decodeGenerateResponse(providers.Request{}, body)
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
