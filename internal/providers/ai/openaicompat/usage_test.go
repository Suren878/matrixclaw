package openaicompat

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageIsNormalised(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage string
		want  providers.Usage
	}{
		{"openai", `{"prompt_tokens":1000,"completion_tokens":100,"total_tokens":1100,"prompt_tokens_details":{"cached_tokens":800},"completion_tokens_details":{"reasoning_tokens":40}}`,
			providers.Usage{PromptTokens: 1000, CacheReadTokens: 800, OutputTokens: 100, ReasoningTokens: 40}},
		{"openrouter cache write", `{"prompt_tokens":1000,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":900}}`,
			providers.Usage{PromptTokens: 1000, CacheWriteTokens: 900, OutputTokens: 10}},
		{"deepseek cache hit", `{"prompt_tokens":1000,"completion_tokens":10,"prompt_cache_hit_tokens":600,"prompt_cache_miss_tokens":400}`,
			providers.Usage{PromptTokens: 1000, CacheReadTokens: 600, OutputTokens: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jsonBody := `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":` + tc.usage + `}`
			fromJSON, err := (&Runtime{}).decodeChatResponse([]byte(jsonBody))
			if err != nil {
				t.Fatal(err)
			}
			frames := `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\n" +
				`data: {"choices":[],"usage":` + tc.usage + `}` + "\n\ndata: [DONE]\n\n"
			fromStream, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(frames))
			if err != nil {
				t.Fatal(err)
			}
			for source, got := range map[string]providers.Usage{"json": fromJSON.Usage, "stream": fromStream.Usage} {
				if len(got.ProviderRaw) == 0 {
					t.Fatalf("%s: provider raw usage missing", source)
				}
				got.ProviderRaw = nil
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s usage=%+v, want %+v", source, got, tc.want)
				}
			}
		})
	}
}
