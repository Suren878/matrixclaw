package daemoncmd

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	geminilive "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/gemini"
	grokvoice "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/grok"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func newRealtimeVoiceManager(setupService *setup.Service, app *core.Core) *realtime.Manager {
	manager := realtime.NewManager(app, realtime.Config{}).
		SetConfigSource(realtimeVoiceConfigSource(setupService))
	manager.RegisterProvider(geminilive.New(geminilive.Config{}).
		SetConfigSource(geminiLiveConfigSource(setupService)))
	manager.RegisterProvider(grokvoice.New(grokvoice.Config{}).
		SetConfigSource(grokVoiceConfigSource(setupService)))
	return manager
}

func realtimeVoiceConfigSource(setupService *setup.Service) realtime.ConfigSource {
	return func(ctx context.Context) realtime.Config {
		cfg := realtime.Config{
			ProviderID:  realtime.ProviderGemini,
			PersistMode: realtime.PersistModeTurnsAndSummary,
		}
		if setupService != nil {
			if setupCfg, err := setupService.Load(); err == nil {
				module := setup.RealtimeVoiceModuleDescriptor(setupCfg.Modules)
				cfg.Enabled = module.Enabled
				cfg.ProviderID = module.ProviderID
			}
		}
		if value, ok := boolEnv("MATRIXCLAW_REALTIME_VOICE_ENABLED"); ok {
			cfg.Enabled = value
		}
		if providerID := strings.TrimSpace(os.Getenv("MATRIXCLAW_REALTIME_VOICE_PROVIDER")); providerID != "" {
			cfg.ProviderID = providerID
		}
		if maxSessions, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MATRIXCLAW_REALTIME_VOICE_MAX_SESSIONS"))); err == nil {
			cfg.MaxSessions = maxSessions
		}
		return cfg
	}
}

func geminiLiveConfigSource(setupService *setup.Service) geminilive.ConfigSource {
	return func(ctx context.Context) geminilive.Config {
		cfg := geminilive.Config{}
		if setupService != nil {
			if setupCfg, err := setupService.Load(); err == nil {
				module := setup.RealtimeVoiceModuleDescriptor(setupCfg.Modules)
				providerCfg := realtimeVoiceProviderConfig(module, realtime.ProviderGemini)
				cfg.APIKey = providerCfg.APIKey
				cfg.APIKeyEnv = providerCfg.APIKeyEnv
				cfg.ModelID = providerCfg.ModelID
				cfg.VoiceID = providerCfg.VoiceID
				cfg.Language = providerCfg.Language
				cfg.WSURL = providerCfg.Endpoint
				cfg.SystemInstruction = realtimeVoiceSystemInstruction(setupCfg)
				cfg.APIKey = firstNonEmpty(
					cfg.APIKey,
					realtimeAPIKeyFromEnvName(cfg.APIKeyEnv),
					configuredGeminiAPIKey(setupCfg),
				)
			}
		}
		cfg.APIKey = firstNonEmpty(
			os.Getenv("MATRIXCLAW_GEMINI_LIVE_API_KEY"),
			cfg.APIKey,
			os.Getenv("GEMINI_API_KEY"),
			os.Getenv("GOOGLE_API_KEY"),
		)
		cfg.ModelID = firstNonEmpty(
			os.Getenv("MATRIXCLAW_GEMINI_LIVE_MODEL"),
			os.Getenv("MATRIXCLAW_REALTIME_VOICE_MODEL"),
			cfg.ModelID,
		)
		cfg.VoiceID = firstNonEmpty(os.Getenv("MATRIXCLAW_GEMINI_LIVE_VOICE"), cfg.VoiceID)
		cfg.Language = firstNonEmpty(
			os.Getenv("MATRIXCLAW_GEMINI_LIVE_LANGUAGE"),
			os.Getenv("MATRIXCLAW_REALTIME_VOICE_LANGUAGE"),
			cfg.Language,
		)
		cfg.WSURL = firstNonEmpty(os.Getenv("MATRIXCLAW_GEMINI_LIVE_WS_URL"), cfg.WSURL)
		return cfg
	}
}

func grokVoiceConfigSource(setupService *setup.Service) grokvoice.ConfigSource {
	return func(ctx context.Context) grokvoice.Config {
		cfg := grokvoice.Config{}
		if setupService != nil {
			if setupCfg, err := setupService.Load(); err == nil {
				module := setup.RealtimeVoiceModuleDescriptor(setupCfg.Modules)
				providerCfg := realtimeVoiceProviderConfig(module, realtime.ProviderGrok)
				cfg.APIKey = providerCfg.APIKey
				cfg.APIKeyEnv = providerCfg.APIKeyEnv
				cfg.ModelID = providerCfg.ModelID
				cfg.VoiceID = providerCfg.VoiceID
				cfg.Language = providerCfg.Language
				cfg.WSURL = providerCfg.Endpoint
				cfg.SystemInstruction = realtimeVoiceSystemInstruction(setupCfg)
				cfg.APIKey = firstNonEmpty(
					cfg.APIKey,
					realtimeAPIKeyFromEnvName(cfg.APIKeyEnv),
					configuredXAIAPIKey(setupCfg),
				)
			}
		}
		cfg.APIKey = firstNonEmpty(
			os.Getenv("MATRIXCLAW_GROK_VOICE_API_KEY"),
			cfg.APIKey,
			os.Getenv("XAI_API_KEY"),
			os.Getenv("GROK_API_KEY"),
		)
		cfg.ModelID = firstNonEmpty(
			os.Getenv("MATRIXCLAW_GROK_VOICE_MODEL"),
			os.Getenv("MATRIXCLAW_REALTIME_VOICE_MODEL"),
			cfg.ModelID,
		)
		cfg.VoiceID = firstNonEmpty(os.Getenv("MATRIXCLAW_GROK_VOICE_VOICE"), cfg.VoiceID)
		cfg.Language = firstNonEmpty(
			os.Getenv("MATRIXCLAW_GROK_VOICE_LANGUAGE"),
			os.Getenv("MATRIXCLAW_REALTIME_VOICE_LANGUAGE"),
			cfg.Language,
		)
		cfg.WSURL = firstNonEmpty(os.Getenv("MATRIXCLAW_GROK_VOICE_WS_URL"), cfg.WSURL)
		return cfg
	}
}

func realtimeVoiceProviderConfig(module setup.VoiceModuleDescriptor, providerID string) setup.VoiceProviderConfig {
	providerID = strings.TrimSpace(providerID)
	for _, provider := range module.Providers {
		if strings.EqualFold(strings.TrimSpace(provider.ID), providerID) {
			return provider.Config
		}
	}
	if strings.EqualFold(strings.TrimSpace(module.ProviderID), providerID) {
		return module.Config
	}
	return setup.VoiceProviderConfig{}
}

func realtimeAPIKeyFromEnvName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(name))
}

func realtimeVoiceSystemInstruction(cfg setup.Config) string {
	name := strings.Join(strings.Fields(cfg.Assistant.Name), " ")
	if name == "" {
		return ""
	}
	return "Assistant identity:\n- Your configured assistant name is " + strconv.Quote(name) + ". Use this exact name when asked who you are."
}

func configuredGeminiAPIKey(cfg setup.Config) string {
	for _, provider := range cfg.Providers {
		if !isGeminiProvider(provider) {
			continue
		}
		if resolved, ok := setup.ProviderConfigWithResolvedAPIKey(provider); ok {
			return strings.TrimSpace(resolved.APIKey)
		}
	}
	return ""
}

func configuredXAIAPIKey(cfg setup.Config) string {
	for _, provider := range cfg.Providers {
		if !isXAIProvider(provider) {
			continue
		}
		if resolved, ok := setup.ProviderConfigWithResolvedAPIKey(provider); ok {
			return strings.TrimSpace(resolved.APIKey)
		}
	}
	return ""
}

func isGeminiProvider(provider setup.ProviderConfig) bool {
	switch strings.ToLower(strings.TrimSpace(firstNonEmpty(provider.Type, provider.CatalogID, provider.ID))) {
	case "gemini", "google-gemini":
		return true
	default:
		return strings.EqualFold(strings.TrimSpace(provider.CatalogID), "gemini") ||
			strings.EqualFold(strings.TrimSpace(provider.ID), "gemini")
	}
}

func isXAIProvider(provider setup.ProviderConfig) bool {
	id := strings.ToLower(strings.TrimSpace(firstNonEmpty(provider.Type, provider.CatalogID, provider.ID)))
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))
	switch id {
	case "xai", "grok", "x-ai":
		return true
	default:
		return strings.EqualFold(strings.TrimSpace(provider.CatalogID), "xai") ||
			strings.EqualFold(strings.TrimSpace(provider.ID), "xai") ||
			strings.Contains(baseURL, "api.x.ai")
	}
}

func boolEnv(name string) (bool, bool) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on", "enabled":
		return true, true
	case "0", "false", "no", "off", "disabled":
		return false, true
	default:
		return false, false
	}
}
