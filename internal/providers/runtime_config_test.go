package providers

import "testing"

func TestAnthropicModelsCallToolsNatively(t *testing.T) {
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
	if base.Capabilities.ToolCalling {
		t.Fatalf("capabilities = %+v, want no tool use", base.Capabilities)
	}
}

func TestIsLoopbackURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://localhost:11434/v1": true, "http://127.0.0.1:1234/v1": true, "http://[::1]:8080": true,
		"http://ollama.localhost/v1": true, "https://api.openai.com/v1": false, "http://192.168.1.5:1234/v1": false, "": false,
	} {
		if got := IsLoopbackURL(raw); got != want {
			t.Errorf("IsLoopbackURL(%q) = %t, want %t", raw, got, want)
		}
	}
}
