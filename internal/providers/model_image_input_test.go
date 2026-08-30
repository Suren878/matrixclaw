package providers

import "testing"

func TestResolveStaticImageInput(t *testing.T) {
	tests := []struct {
		name         string
		providerID   string
		providerType string
		modelID      string
		fallback     bool
		want         bool
	}{
		{name: "DeepSeek text model", providerID: "deepseek", providerType: TypeOpenAICompat, modelID: "deepseek-v4-flash", want: false},
		{name: "OpenAI GPT-5", providerID: "openai", providerType: TypeOpenAICompat, modelID: "gpt-5.4-mini", fallback: true, want: true},
		{name: "OpenAI embedding model", providerID: "openai", providerType: TypeOpenAICompat, modelID: "text-embedding-4", fallback: true, want: false},
		{name: "Qwen text model", providerID: "qwen", providerType: TypeOpenAICompat, modelID: "qwen3.6-flash", want: false},
		{name: "Qwen vision model", providerID: "qwen", providerType: TypeOpenAICompat, modelID: "qwen3-vl-plus", want: true},
		{name: "Gemini model", providerID: "gemini", providerType: TypeGemini, modelID: "gemini-3-flash", fallback: true, want: true},
		{name: "Gemma text model", providerID: "gemini", providerType: TypeGemini, modelID: "gemma-4-31b-it", fallback: true, want: false},
		{name: "Claude model", providerID: "anthropic", providerType: TypeAnthropic, modelID: "claude-sonnet-4-5", fallback: true, want: true},
		{name: "Unknown compatible model", providerID: "custom", providerType: TypeOpenAICompat, modelID: "future-chat", fallback: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveStaticImageInput(tt.providerID, tt.providerType, tt.modelID, tt.fallback); got != tt.want {
				t.Fatalf("resolveStaticImageInput() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestResolvedDeepSeekCapabilitiesAreTextOnly(t *testing.T) {
	capabilities := ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   "deepseek",
		ProviderType: TypeOpenAICompat,
		ModelID:      "deepseek-v4-flash",
	}).RuntimeCapabilities
	if capabilities.ImageInput {
		t.Fatal("DeepSeek text model unexpectedly supports image input")
	}
}
