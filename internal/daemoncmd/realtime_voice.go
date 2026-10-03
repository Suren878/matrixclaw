package daemoncmd

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	geminilive "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/gemini"
	grokvoice "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/grok"
	openairealtime "github.com/Suren878/matrixclaw/internal/modules/voice/realtime/providers/openai"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func newRealtimeVoiceManager(setupService *setup.Service, app *core.Core) *realtime.Manager {
	manager := realtime.NewManager(app, realtime.Config{}).
		SetConfigSource(realtimeVoiceConfigSource(setupService))
	manager.RegisterProvider(geminilive.New(geminilive.Config{}).SetConfigSource(func(ctx context.Context) geminilive.Config {
		return geminilive.Config(geminiLiveSource.config(setupService))
	}))
	manager.RegisterProvider(grokvoice.New(grokvoice.Config{}).SetConfigSource(func(ctx context.Context) grokvoice.Config {
		return grokvoice.Config(grokVoiceSource.config(setupService))
	}))
	manager.RegisterProvider(openairealtime.New(openairealtime.Config{}).SetConfigSource(func(ctx context.Context) openairealtime.Config {
		return openairealtime.Config(openAIRealtimeSource.config(setupService))
	}))
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

// realtimeProviderConfig has the fields every realtime voice provider's Config has.
type realtimeProviderConfig struct {
	APIKey            string
	APIKeyEnv         string
	WSURL             string
	ModelID           string
	VoiceID           string
	Language          string
	SystemInstruction string
	DialTimeout       time.Duration
}

// realtimeProviderSource resolves one realtime voice provider's config from
// setup, overridden by the MATRIXCLAW_<envPrefix>_* environment variables.
type realtimeProviderSource struct {
	id        string
	envPrefix string
	keyEnvs   []string
	matches   func(setup.ProviderConfig) bool
}

var (
	geminiLiveSource     = realtimeProviderSource{id: realtime.ProviderGemini, envPrefix: "GEMINI_LIVE", keyEnvs: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, matches: isGeminiProvider}
	grokVoiceSource      = realtimeProviderSource{id: realtime.ProviderGrok, envPrefix: "GROK_VOICE", keyEnvs: []string{"XAI_API_KEY", "GROK_API_KEY"}, matches: isXAIProvider}
	openAIRealtimeSource = realtimeProviderSource{id: realtime.ProviderOpenAI, envPrefix: "OPENAI_REALTIME", keyEnvs: []string{"OPENAI_API_KEY"}, matches: isOpenAIProvider}
)

func (p realtimeProviderSource) config(setupService *setup.Service) realtimeProviderConfig {
	cfg := realtimeProviderConfig{}
	if setupService != nil {
		if setupCfg, err := setupService.Load(); err == nil {
			module := setup.RealtimeVoiceModuleDescriptor(setupCfg.Modules)
			providerCfg := realtimeVoiceProviderConfig(module, p.id)
			cfg.APIKeyEnv = providerCfg.APIKeyEnv
			cfg.ModelID = providerCfg.ModelID
			cfg.VoiceID = providerCfg.VoiceID
			cfg.Language = providerCfg.Language
			cfg.WSURL = providerCfg.Endpoint
			cfg.SystemInstruction = realtimeVoiceSystemInstruction(setupCfg)
			cfg.APIKey = firstNonEmpty(
				providerCfg.APIKey,
				realtimeAPIKeyFromEnvName(cfg.APIKeyEnv),
				configuredProviderAPIKey(setupCfg, p.matches),
			)
		}
	}
	env := func(name string) string { return os.Getenv("MATRIXCLAW_" + p.envPrefix + "_" + name) }
	keys := []string{env("API_KEY"), cfg.APIKey}
	for _, name := range p.keyEnvs {
		keys = append(keys, os.Getenv(name))
	}
	cfg.APIKey = firstNonEmpty(keys...)
	cfg.ModelID = firstNonEmpty(env("MODEL"), os.Getenv("MATRIXCLAW_REALTIME_VOICE_MODEL"), cfg.ModelID)
	cfg.VoiceID = firstNonEmpty(env("VOICE"), cfg.VoiceID)
	cfg.Language = firstNonEmpty(env("LANGUAGE"), os.Getenv("MATRIXCLAW_REALTIME_VOICE_LANGUAGE"), cfg.Language)
	cfg.WSURL = firstNonEmpty(env("WS_URL"), cfg.WSURL)
	return cfg
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
	name := strings.Join(strings.Fields(cfg.Assistant.NameOrDefault()), " ")
	if name == "" {
		return ""
	}
	return "Assistant identity:\n- Your configured assistant name is " + strconv.Quote(name) + ". Use this exact name when asked who you are."
}

func configuredProviderAPIKey(cfg setup.Config, matches func(setup.ProviderConfig) bool) string {
	for _, provider := range cfg.Providers {
		if resolved, ok := provider.Runtime(); ok && matches(resolved) {
			return resolved.APIKey
		}
	}
	return ""
}

func isGeminiProvider(provider setup.ProviderConfig) bool {
	switch strings.ToLower(firstNonEmpty(provider.Type, provider.ID)) {
	case "gemini", "google-gemini":
		return true
	default:
		return strings.EqualFold(provider.ID, "gemini")
	}
}

func isXAIProvider(provider setup.ProviderConfig) bool {
	id := strings.ToLower(firstNonEmpty(provider.Type, provider.ID))
	switch id {
	case "xai", "grok", "x-ai":
		return true
	default:
		return strings.EqualFold(provider.ID, "xai") || strings.Contains(strings.ToLower(provider.BaseURL), "api.x.ai")
	}
}

func isOpenAIProvider(provider setup.ProviderConfig) bool {
	return strings.EqualFold(provider.ID, "openai") || strings.Contains(strings.ToLower(provider.BaseURL), "api.openai.com")
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
