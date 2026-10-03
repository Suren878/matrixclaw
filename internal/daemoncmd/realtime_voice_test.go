package daemoncmd

import (
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestRealtimeProviderConfigPrefersEnvironmentOverSetup(t *testing.T) {
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	err := store.Save(setup.Config{
		Assistant: setup.AssistantConfig{Name: "Ava"},
		Providers: []setup.ProviderConfig{{ID: "xai", Type: "openai-compatible", APIKey: "xai-key", Model: "grok-4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_GROK_VOICE_API_KEY", "")
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("GROK_API_KEY", "")
	t.Setenv("MATRIXCLAW_REALTIME_VOICE_MODEL", "")
	t.Setenv("MATRIXCLAW_GROK_VOICE_MODEL", "grok-voice-test")

	cfg := grokVoiceSource.config(setup.NewService(store))
	if cfg.APIKey != "xai-key" {
		t.Fatalf("APIKey = %q, want the configured xAI provider key", cfg.APIKey)
	}
	if cfg.ModelID != "grok-voice-test" {
		t.Fatalf("ModelID = %q, want the environment override", cfg.ModelID)
	}
	if cfg.SystemInstruction == "" {
		t.Fatal("SystemInstruction is empty, want the assistant name")
	}
}
