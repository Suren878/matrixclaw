package daemoncmd

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	geminilive "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/gemini"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func newRealtimeVoiceManager(setupService *setup.Service, app *core.Core) *realtime.Manager {
	manager := realtime.NewManager(app, realtime.Config{}).
		SetConfigSource(realtimeVoiceConfigSource(setupService))
	manager.RegisterProvider(geminilive.New(geminilive.Config{}).
		SetConfigSource(geminiLiveConfigSource(setupService)))
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
				if module.ProviderID == realtime.ProviderGemini {
					cfg.APIKey = module.Config.APIKey
					cfg.APIKeyEnv = module.Config.APIKeyEnv
					cfg.ModelID = module.Config.ModelID
					cfg.VoiceID = module.Config.VoiceID
					cfg.WSURL = module.Config.Endpoint
				}
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
		cfg.WSURL = firstNonEmpty(os.Getenv("MATRIXCLAW_GEMINI_LIVE_WS_URL"), cfg.WSURL)
		return cfg
	}
}

func realtimeAPIKeyFromEnvName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(name))
}

func realtimeVoiceSystemInstruction(cfg setup.Config) string {
	system := strings.TrimSpace(setup.InitializeAssistantSystemPromptForConfig(cfg.Assistant.SystemPrompt, cfg))
	custom := strings.TrimSpace(cfg.Assistant.CustomInstructions)
	if system == "" {
		return custom
	}
	if custom == "" {
		return system
	}
	return system + "\n\n" + custom
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

func isGeminiProvider(provider setup.ProviderConfig) bool {
	switch strings.ToLower(strings.TrimSpace(firstNonEmpty(provider.Type, provider.CatalogID, provider.ID))) {
	case "gemini", "google-gemini":
		return true
	default:
		return strings.EqualFold(strings.TrimSpace(provider.CatalogID), "gemini") ||
			strings.EqualFold(strings.TrimSpace(provider.ID), "gemini")
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
