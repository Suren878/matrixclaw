package providers

import "testing"

func TestPromptCacheKeyOnlyForOpenAIEndpoint(t *testing.T) {
	openai := ResolveOpenAIChatOptions("openai", "https://api.openai.com/v1", "gpt-5.4")
	router := ResolveOpenAIChatOptions("openrouter", "https://openrouter.ai/api/v1", "openai/gpt-5.4")
	if !openai.PromptCacheKey || router.PromptCacheKey {
		t.Fatalf("prompt_cache_key openai=%v openrouter=%v, want true/false", openai.PromptCacheKey, router.PromptCacheKey)
	}
}

func TestContentCacheBreakpointsOnlyForClaudeOnOpenRouter(t *testing.T) {
	router := "https://openrouter.ai/api/v1"
	for _, tc := range []struct {
		baseURL string
		model   string
		want    bool
	}{
		{router, "anthropic/claude-sonnet-4.6", true},
		{router, "~anthropic/claude-sonnet-latest", true},
		{router, "openai/gpt-5.4", false},
		{"https://api.openai.com/v1", "claude-sonnet-4.6", false},
	} {
		got := ResolveOpenAIChatOptions("openrouter", tc.baseURL, tc.model).ContentCacheControl
		if got != tc.want {
			t.Errorf("%s %s: content cache control = %v, want %v", tc.baseURL, tc.model, got, tc.want)
		}
	}
}
