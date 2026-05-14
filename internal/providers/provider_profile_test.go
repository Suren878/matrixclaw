package providers

import "testing"

func TestProfileForProviderSelectsRuntimeAndDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		provider               string
		baseURL                string
		wantType               string
		wantRuntime            string
		wantReasoningSupported bool
		wantToolUse            ToolUseMode
	}{
		{name: "default", provider: "", baseURL: "https://api.example.com/v1", wantType: TypeOpenAICompat, wantRuntime: TypeOpenAICompat, wantToolUse: ToolUseNative},
		{name: "case insensitive", provider: " Gemini ", baseURL: "https://generativelanguage.googleapis.com/v1beta", wantType: TypeGemini, wantRuntime: TypeGemini, wantToolUse: ToolUseNative},
		{name: "gemini", provider: TypeGemini, baseURL: "https://generativelanguage.googleapis.com/v1beta", wantType: TypeGemini, wantRuntime: TypeGemini, wantToolUse: ToolUseNative},
		{name: "anthropic", provider: TypeAnthropic, baseURL: "https://api.anthropic.com/v1", wantType: TypeAnthropic, wantRuntime: TypeAnthropic, wantToolUse: ToolUseDisabled},
	}

	for _, tt := range tests {
		got := ProfileForProvider(tt.provider)
		if got.ProviderType != tt.wantType || got.RuntimeProviderType != tt.wantRuntime {
			t.Fatalf("%s profile = %#v, want provider/runtime %q/%q", tt.name, got, tt.wantType, tt.wantRuntime)
		}
		if got.SupportsReasoningEffort != tt.wantReasoningSupported {
			t.Fatalf("%s SupportsReasoningEffort = %v, want %v", tt.name, got.SupportsReasoningEffort, tt.wantReasoningSupported)
		}
		if got.Capabilities.ToolCalling != (tt.wantToolUse != ToolUseDisabled) {
			t.Fatalf("%s ToolCalling = %v, want profile tool mode %q", tt.name, got.Capabilities.ToolCalling, tt.wantToolUse)
		}
		if got.RuntimeProfile.ToolUseMode != tt.wantToolUse {
			t.Fatalf("%s ToolUseMode = %q, want %q", tt.name, got.RuntimeProfile.ToolUseMode, tt.wantToolUse)
		}
		if tt.wantType == TypeGemini && got.RuntimeProfile.ToolSchemaDialect != ToolSchemaGemini {
			t.Fatalf("Gemini profile = %#v, want Gemini schema", got)
		}
	}
}

func TestProfileForModelUsesCatalogCapabilities(t *testing.T) {
	t.Parallel()

	openAI := ProfileForModel("openai", TypeOpenAICompat, "gpt-5.4-mini")
	if !openAI.SupportsReasoningEffort || !openAI.Capabilities.ReasoningWithTools {
		t.Fatalf("openai profile = %#v, want reasoning effort with tools", openAI)
	}

	got := ProfileForModel("openrouter", TypeOpenAICompat, "qwen/qwen3-coder-next")
	if got.ProviderID != "openrouter" || got.Model != "qwen/qwen3-coder-next" {
		t.Fatalf("profile identity = %#v", got)
	}
	if got.SupportsReasoningEffort {
		t.Fatalf("openrouter SupportsReasoningEffort = true, want false")
	}
	if !got.Capabilities.ToolCalling || got.RuntimeProfile.ToolUseMode != ToolUseNative {
		t.Fatalf("openrouter profile = %#v, want native tools", got)
	}
}
