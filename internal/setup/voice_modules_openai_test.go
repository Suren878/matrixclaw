package setup

import "testing"

func TestOpenAIRealtimeVoiceProviderDefaults(t *testing.T) {
	if !voiceProviderExists(VoiceModuleRealtime, "openai_realtime") {
		t.Fatal("openai_realtime provider is not registered")
	}
	cfg := defaultVoiceProviderConfig("openai_realtime")
	if cfg.ModelID != "gpt-realtime-2.1" {
		t.Fatalf("model = %q", cfg.ModelID)
	}
	if cfg.VoiceID != "marin" {
		t.Fatalf("voice = %q", cfg.VoiceID)
	}
	if cfg.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("API key env = %q", cfg.APIKeyEnv)
	}
	if cfg.Endpoint != "wss://api.openai.com/v1/realtime" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
}

func TestOpenAIRealtimeVoiceProviderKeepsCloudCredentials(t *testing.T) {
	cfg := normalizeVoiceProviderConfig(VoiceModuleRealtime, "openai_realtime", VoiceProviderConfig{
		APIKey:    "  sk-test  ",
		APIKeyEnv: "  CUSTOM_OPENAI_KEY  ",
		Language:  "ru_ru",
	})
	if cfg.APIKey != "sk-test" {
		t.Fatalf("API key = %q", cfg.APIKey)
	}
	if cfg.APIKeyEnv != "CUSTOM_OPENAI_KEY" {
		t.Fatalf("API key env = %q", cfg.APIKeyEnv)
	}
	if cfg.Language != "ru-RU" {
		t.Fatalf("language = %q", cfg.Language)
	}
	if cfg.ModelID != "gpt-realtime-2.1" || cfg.VoiceID != "marin" {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
}
