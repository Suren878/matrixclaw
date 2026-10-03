package providers

import "testing"

func TestAnthropicProfilesUseNativeToolCalling(t *testing.T) {
	for _, tc := range []struct{ providerID, modelID string }{
		{"anthropic", "claude-sonnet-4-5"},
		{"custom-anthropic-compatible", "glm-4.6"},
		{"", ""},
	} {
		base := NewRuntimeBase(RuntimeConfig{ProviderID: tc.providerID, Model: tc.modelID}, TypeAnthropic, DefaultAnthropicModel)
		if !base.Capabilities.ToolCalling {
			t.Errorf("%q/%q: tool calling is off", tc.providerID, tc.modelID)
		}
	}
}

func TestDisabledToolUseTurnsToolCallingOff(t *testing.T) {
	base := NewRuntimeBase(RuntimeConfig{ProviderID: "anthropic", ToolUseMode: ToolUseDisabled}, TypeAnthropic, DefaultAnthropicModel)
	if base.Capabilities.ToolCalling || base.Capabilities.ParallelToolCalls || base.Capabilities.ReasoningWithTools {
		t.Fatalf("capabilities = %+v, want no tool use", base.Capabilities)
	}
}
