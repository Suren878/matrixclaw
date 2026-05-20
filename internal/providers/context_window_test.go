package providers

import "testing"

func TestResolveContextWindowTokensCodexSubscription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
		want  int
	}{
		{name: "spark", model: "gpt-5.3-codex-spark", want: 128_000},
		{name: "codex", model: "gpt-5.3-codex", want: 272_000},
		{name: "gpt54", model: "gpt-5.4", want: 272_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveContextWindowTokens("openai-codex", TypeOpenAICodex, tt.model); got != tt.want {
				t.Fatalf("ResolveContextWindowTokens(openai-codex, %q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestResolveContextWindowTokensProviderPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		providerID string
		model      string
		want       int
	}{
		{name: "openai full", providerID: "openai", model: "gpt-5.4", want: 1_050_000},
		{name: "openai mini", providerID: "openai", model: "gpt-5.4-mini", want: 400_000},
		{name: "openrouter strips owner", providerID: "openrouter", model: "x-ai/grok-4.3", want: 1_000_000},
		{name: "unknown fallback", providerID: "custom", model: "unknown-model", want: DefaultFallbackContextWindowTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveContextWindowTokens(tt.providerID, TypeOpenAICompat, tt.model); got != tt.want {
				t.Fatalf("ResolveContextWindowTokens(%q, %q) = %d, want %d", tt.providerID, tt.model, got, tt.want)
			}
		})
	}
}

func TestResolveContextWindowTokensUsesRegisteredMetadata(t *testing.T) {
	t.Parallel()

	RegisterContextWindowTokens("custom-provider", TypeOpenAICompat, "owner/live-context-model", 777_000)

	if got := ResolveContextWindowTokens("custom-provider", TypeOpenAICompat, "owner/live-context-model"); got != 777_000 {
		t.Fatalf("ResolveContextWindowTokens() = %d, want registered metadata", got)
	}
}
