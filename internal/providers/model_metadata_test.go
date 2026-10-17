package providers

import "testing"

func TestLearnedMetadataAnswersOnlyForTheProviderThatListedIt(t *testing.T) {
	noTools := false
	RegisterModelMetadata("openrouter", "openai/gpt-5-leak", ModelMetadataRegistration{ContextWindow: 50_000, MaxOutputTokens: 1_000, ToolCalling: &noTools})

	got := ResolveModelMetadata("openrouter", TypeOpenAICompat, "openai/gpt-5-leak")
	if got.ContextWindow != 50_000 || got.MaxOutputTokens != 1_000 || got.ToolCalling || got.Source != ModelMetadataSourceLiveCatalog {
		t.Fatalf("listing provider = %+v, want its own listing", got)
	}
	for _, tc := range []struct{ provider, providerType, model string }{
		{"openai", "", "gpt-5-leak"},
		{"openai", "", "openai/gpt-5-leak"},
		{"deepseek", "", "openai/gpt-5-leak"},
		{"my-gateway", TypeOpenAICompat, "openai/gpt-5-leak"},
		{"openai-codex", "", "gpt-5-leak"},
	} {
		other := ResolveModelMetadata(tc.provider, tc.providerType, tc.model)
		if other.ContextWindow == 50_000 || other.MaxOutputTokens != 0 || !other.ToolCalling || other.Source == ModelMetadataSourceLiveCatalog {
			t.Errorf("%s %s = %+v, leaked from openrouter", tc.provider, tc.model, other)
		}
	}
}

func TestLearnedMetadataKeepsTheVendorPrefix(t *testing.T) {
	RegisterModelMetadata("prefix-gw", "vendor-a/shared-name", ModelMetadataRegistration{ContextWindow: 32_000})
	if got := ResolveContextWindowTokens("prefix-gw", TypeOpenAICompat, "Vendor-A/Shared-Name"); got != 32_000 {
		t.Fatalf("same model, other case = %d, want 32000", got)
	}
	if got := ResolveContextWindowTokens("prefix-gw", TypeOpenAICompat, "vendor-b/shared-name"); got == 32_000 {
		t.Fatal("another vendor's model of the same name got the learned window")
	}
}

func TestLearnedWindowBeatsStaticRuleThenFallback(t *testing.T) {
	for _, tc := range []struct {
		provider, providerType, model string
		learned, want                 int
		source                        ModelMetadataSource
	}{
		{"anthropic", "", "claude-sonnet-4-5-precedence", 0, 200_000, ModelMetadataSourceStaticRule},
		{"anthropic-precedence", TypeAnthropic, "claude-sonnet-4-5-precedence", 150_000, 150_000, ModelMetadataSourceLiveCatalog},
		{"gemini", "", "models/gemini-precedence", 0, 1_048_576, ModelMetadataSourceStaticRule},
		{"openai-codex", "", "gpt-5.4-precedence", 0, 272_000, ModelMetadataSourceStaticRule},
		{"openai", "", "gpt-5.4-precedence", 0, 1_050_000, ModelMetadataSourceStaticRule},
		{"codex-precedence", TypeOpenAICodex, "gpt-5.4-precedence", 400_000, 400_000, ModelMetadataSourceLiveCatalog},
		{"custom-precedence", TypeOpenAICompat, "mystery-precedence", 0, DefaultFallbackContextWindowTokens, ModelMetadataSourceFallback},
	} {
		if tc.learned > 0 {
			RegisterContextWindowTokens(tc.provider, tc.model, tc.learned)
		}
		got := ResolveModelMetadata(tc.provider, tc.providerType, tc.model)
		if got.ContextWindow != tc.want || got.Source != tc.source {
			t.Errorf("%s %s = %d (%s), want %d (%s)", tc.provider, tc.model, got.ContextWindow, got.Source, tc.want, tc.source)
		}
	}
}

func TestGeminiListingNamesMatchNormalizedModelIDs(t *testing.T) {
	RegisterModelMetadata("gemini", NormalizeModelID("gemini", TypeGemini, "models/gemini-listing-test"), ModelMetadataRegistration{MaxOutputTokens: 65_536})
	if got := ResolveModelMetadata("gemini", TypeGemini, "gemini-listing-test").MaxOutputTokens; got != 65_536 {
		t.Fatalf("max output = %d, want 65536", got)
	}
}
