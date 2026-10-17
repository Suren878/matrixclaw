package setup

import (
	"slices"
	"testing"
)

func TestChildEnvironmentHidesKeysAndDaemonVariables(t *testing.T) {
	cfg := Config{
		Providers: []ProviderConfig{{ID: "custom", Type: "openai-compatible", APIKeyEnv: "CUSTOM_LLM_TOKEN"}},
		Modules: ModulesConfig{RealtimeVoice: VoiceModuleConfig{Providers: map[string]VoiceProviderConfig{
			"grok": {APIKeyEnv: "VOICE_SECRET"},
		}}},
	}
	environ := []string{
		"PATH=/usr/bin", "HOME=/home/u", "GOFLAGS=-mod=mod",
		"OPENAI_API_KEY=sk-1", "HF_TOKEN=hf", "TAVILY_API_KEY=t", "CUSTOM_LLM_TOKEN=c", "VOICE_SECRET=v",
		"MATRIXCLAW_API_TOKEN=x", "MATRIXCLAW_SETUP_PATH=/s",
	}

	got := ChildEnvironment(cfg, environ)

	if want := []string{"PATH=/usr/bin", "HOME=/home/u", "GOFLAGS=-mod=mod"}; !slices.Equal(got, want) {
		t.Fatalf("ChildEnvironment = %q, want %q", got, want)
	}
}
