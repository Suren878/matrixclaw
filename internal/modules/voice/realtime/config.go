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

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
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
	// Identity, the user's custom instructions and, for phone calls, the
	// phone prompt come before every session's own instructions.
	Identity           string
	CustomInstructions string
	PhonePrompt        string
	Providers          map[string]ProviderConfig
}

// telephonyClient is the client name of the telephony gateway's sessions.
const telephonyClient = "telephony"

// instructions are the daemon's instructions for a session of client.
func (c Config) instructions(client string) string {
	parts := []string{}
	if c.Identity != "" {
		parts = append(parts, c.Identity)
	}
	if c.PhonePrompt != "" && strings.EqualFold(strings.TrimSpace(client), telephonyClient) {
		parts = append(parts, "Phone assistant instructions:\n"+c.PhonePrompt)
	}
	if c.CustomInstructions != "" {
		parts = append(parts, "User custom instructions:\n"+c.CustomInstructions)
	}
	return strings.Join(parts, "\n\n")
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
		Enabled:            module.Enabled,
		ProviderID:         normalizeID(module.ProviderID),
		Identity:           assistantIdentity(cfg.Assistant.NameOrDefault()),
		CustomInstructions: strings.TrimSpace(cfg.Assistant.CustomInstructions),
		PhonePrompt:        strings.TrimSpace(cfg.Modules.Telephony.PhonePrompt),
		Providers:          map[string]ProviderConfig{},
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

// peek is the cached check of key, if any, without checking it.
func (k *keyChecks) peek(spec ProviderSpec, key string) (keyCheck, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	cached, ok := k.results[spec.ID]
	return cached, ok && cached.key == sha256.Sum256([]byte(key))
}

func (m *Manager) ID() string { return ModuleID }

func (m *Manager) Tools() []tools.Executor { return nil }

func (m *Manager) Context() string { return "" }

// Close leaves running sessions to end with their streams.
func (m *Manager) Close() error { return nil }

// Status reports the module from its settings and the last key check.
func (m *Manager) Status(context.Context) modules.Status {
	cfg := m.currentConfig()
	spec := m.activeSpec(cfg)
	provider := cfg.provider(spec)
	check, checked := m.keys.peek(spec, provider.APIKey)
	state := "Disabled"
	switch {
	case !cfg.Enabled:
	case provider.APIKey == "":
		state = "API key required"
	case checked && check.err != nil:
		state = "Could not verify API key"
	default:
		state = "Ready"
	}
	return modules.Status{
		ID:      ModuleID,
		Title:   "Realtime Voice",
		Enabled: cfg.Enabled,
		Ready:   state == "Ready",
		State:   state,
		Facts: []modules.Fact{
			{Key: "provider", Label: "Provider", Value: spec.Name},
			{Key: "model", Label: "Model", Value: provider.ModelID},
		},
	}
}
