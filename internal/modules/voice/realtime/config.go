package realtime

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	keyCheckTTL      = 6 * time.Hour
	keyCheckErrorTTL = 30 * time.Second
	keyCheckTimeout  = 2 * time.Second
	defaultSessions  = 8
)

// Config is the realtime module's settings with every provider resolved.
type Config struct {
	Enabled     bool
	ProviderID  string
	MaxSessions int // 0 means the default of 8
	// Instructions come before every session's own: the assistant identity.
	Instructions string
	Providers    map[string]ProviderConfig
}

// provider is spec's effective settings: stored values over its defaults.
func (c Config) provider(spec ProviderSpec) ProviderConfig {
	cfg := c.Providers[spec.ID]
	cfg.Endpoint = cmp.Or(cfg.Endpoint, spec.Endpoint)
	cfg.ModelID = cmp.Or(cfg.ModelID, spec.DefaultModel)
	cfg.VoiceID = cmp.Or(cfg.VoiceID, spec.DefaultVoice)
	cfg.Language = spec.NormalizeLanguage(cfg.Language)
	return cfg
}

// Apply resolves the realtime settings of cfg: per provider the stored
// values, and the API key from setup, $api_key_env, a configured LLM
// provider of the same vendor or the vendor's usual environment variables.
func (m *Manager) Apply(_ context.Context, cfg setup.Config) error {
	module := cfg.Modules.RealtimeVoice
	next := Config{
		Enabled:      module.Enabled,
		ProviderID:   normalizeID(module.ProviderID),
		Instructions: assistantIdentity(cfg.Assistant.NameOrDefault()),
		Providers:    map[string]ProviderConfig{},
	}
	for _, spec := range m.specs {
		stored := module.Providers[spec.ID]
		next.Providers[spec.ID] = ProviderConfig{
			APIKey:    apiKey(cfg, spec, stored),
			APIKeyEnv: strings.TrimSpace(stored.APIKeyEnv),
			Endpoint:  strings.TrimSpace(stored.Endpoint),
			ModelID:   strings.TrimSpace(stored.ModelID),
			VoiceID:   strings.TrimSpace(stored.VoiceID),
			Language:  strings.TrimSpace(stored.Language),
		}
	}
	m.setConfig(next)
	return nil
}

func apiKey(cfg setup.Config, spec ProviderSpec, stored setup.VoiceProviderConfig) string {
	if key := strings.TrimSpace(stored.APIKey); key != "" {
		return key
	}
	if name := strings.TrimSpace(stored.APIKeyEnv); name != "" {
		if key := strings.TrimSpace(os.Getenv(name)); key != "" {
			return key
		}
	}
	for _, provider := range cfg.Providers {
		if runtime, ok := provider.Runtime(); ok && slices.Contains(spec.LLMProviders, runtime.ID) {
			return runtime.APIKey
		}
	}
	for _, name := range spec.KeyEnvs {
		if key := strings.TrimSpace(os.Getenv(name)); key != "" {
			return key
		}
	}
	return ""
}

func assistantIdentity(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return ""
	}
	return "Assistant identity:\n- Your configured assistant name is " + strconv.Quote(name) + ". Use this exact name when asked who you are."
}

// Edit applies update to the stored realtime settings; values equal to the
// provider's defaults are not stored, and an empty key keeps the stored one.
func (m *Manager) Edit(module *setup.VoiceModuleConfig, update setup.VoiceModuleUpdate) error {
	if update.Enabled != nil {
		module.Enabled = *update.Enabled
	}
	providerID := normalizeID(module.ProviderID)
	if update.ProviderID != "" {
		spec, ok := m.spec(update.ProviderID)
		if !ok {
			return errors.New("realtime voice provider " + strconv.Quote(update.ProviderID) + " is not available")
		}
		providerID = spec.ID
		module.ProviderID = spec.ID
	}
	if update.ProviderConfig == nil {
		return nil
	}
	spec, ok := m.spec(providerID)
	if !ok {
		spec = m.specs[0]
	}
	next := *update.ProviderConfig
	stored := module.Providers[spec.ID]
	next.APIKey = cmp.Or(strings.TrimSpace(next.APIKey), stored.APIKey)
	next.APIKeyEnv = cmp.Or(strings.TrimSpace(next.APIKeyEnv), stored.APIKeyEnv)
	next.Endpoint = omitDefault(next.Endpoint, spec.Endpoint)
	next.ModelID = omitDefault(next.ModelID, spec.DefaultModel)
	next.VoiceID = omitDefault(next.VoiceID, spec.DefaultVoice)
	next.Language = omitDefault(spec.NormalizeLanguage(next.Language), "auto")
	next.RuntimeMode, next.BinaryPath, next.Threads, next.Autostart = "", "", 0, false
	if module.Providers == nil {
		module.Providers = map[string]setup.VoiceProviderConfig{}
	}
	module.Providers[spec.ID] = next
	return nil
}

func omitDefault(value string, def string) string {
	if value = strings.TrimSpace(value); strings.EqualFold(value, def) {
		return ""
	}
	return value
}

// keyChecks caches API key checks per provider and key.
type keyChecks struct {
	mu      sync.Mutex
	results map[string]keyCheck
}

type keyCheck struct {
	key    [32]byte
	at     time.Time
	models []string
	err    error
}

func (k *keyChecks) check(ctx context.Context, spec ProviderSpec, key string) keyCheck {
	sum := sha256.Sum256([]byte(key))
	k.mu.Lock()
	cached, ok := k.results[spec.ID]
	k.mu.Unlock()
	ttl := keyCheckTTL
	if cached.err != nil {
		ttl = keyCheckErrorTTL
	}
	if ok && cached.key == sum && time.Since(cached.at) < ttl {
		return cached
	}
	ctx, cancel := context.WithTimeout(ctx, keyCheckTimeout)
	defer cancel()
	models, err := spec.CheckKey(ctx, key)
	result := keyCheck{key: sum, at: time.Now(), models: models, err: err}
	k.mu.Lock()
	if k.results == nil {
		k.results = map[string]keyCheck{}
	}
	k.results[spec.ID] = result
	k.mu.Unlock()
	return result
}

func (m *Manager) providerDescriptor(ctx context.Context, spec ProviderSpec, cfg ProviderConfig) ProviderDescriptor {
	models := spec.Models
	status := "API key required"
	keyValid, keyError := false, ""
	if cfg.APIKey != "" {
		result := m.keys.check(ctx, spec, cfg.APIKey)
		if spec.Models == nil {
			models = result.models
		}
		keyValid = result.err == nil
		var authErr *KeyError
		switch {
		case errors.As(result.err, &authErr) && authErr.Auth():
			status, keyError = "Invalid API key", result.err.Error()
		case result.err != nil:
			status, keyError = "Could not verify API key", result.err.Error()
		case len(models) == 0:
			status = "No realtime models available"
		case cfg.ModelID == "":
			status = "Model required"
		case !spec.hasModel(cfg.ModelID, models):
			status = "Selected model is not available"
		default:
			status = "Ready"
		}
	}
	return ProviderDescriptor{
		ID:         spec.ID,
		Name:       spec.Name,
		Status:     status,
		Configured: status == "Ready",
		Config: ProviderConfigSummary{
			APIKeyConfigured: cfg.APIKey != "",
			APIKeyValid:      keyValid,
			APIKeyPreview:    setup.MaskSecret(cfg.APIKey),
			APIKeyError:      keyError,
			APIKeyEnv:        cfg.APIKeyEnv,
			ModelID:          cfg.ModelID,
			VoiceID:          cfg.VoiceID,
			Language:         cfg.Language,
			Endpoint:         cfg.Endpoint,
		},
		DefaultModel:  spec.DefaultModel,
		DefaultVoice:  spec.DefaultVoice,
		KeyEnvs:       spec.KeyEnvs,
		Models:        models,
		Voices:        spec.Voices,
		Languages:     spec.Languages,
		InputFormats:  []AudioFormat{DefaultInputAudioFormat()},
		OutputFormats: []AudioFormat{DefaultOutputAudioFormat()},
	}
}
