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
		{name: "default", provider: "", baseURL: "https://api.example.com/v1", wantType: TypeOpenAICompat, wantRuntime: TypeOpenAICompat, wantReasoningSupported: true, wantToolUse: ToolUseNative},
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
		if got.RuntimeProfile.ToolUseMode != tt.wantToolUse {
			t.Fatalf("%s ToolUseMode = %q, want %q", tt.name, got.RuntimeProfile.ToolUseMode, tt.wantToolUse)
		}
		if tt.wantType == TypeGemini && got.RuntimeProfile.ToolSchemaDialect != ToolSchemaGemini {
			t.Fatalf("Gemini profile = %#v, want Gemini schema", got)
		}
	}
}
