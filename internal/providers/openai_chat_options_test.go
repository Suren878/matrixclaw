package providers

import "testing"

func TestPromptCacheKeyOnlyForOpenAIEndpoint(t *testing.T) {
	openai := ResolveOpenAIChatOptions(ProfileForModel("openai", TypeOpenAICompat, "gpt-5.4"), "https://api.openai.com/v1", "gpt-5.4")
	router := ResolveOpenAIChatOptions(ProfileForModel("openrouter", TypeOpenAICompat, "openai/gpt-5.4"), "https://openrouter.ai/api/v1", "openai/gpt-5.4")
	if !openai.PromptCacheKey || router.PromptCacheKey {
		t.Fatalf("prompt_cache_key openai=%v openrouter=%v, want true/false", openai.PromptCacheKey, router.PromptCacheKey)
	}
}
