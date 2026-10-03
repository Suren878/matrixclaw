package realtime

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// Settings are the provider choice and a page per provider: its key, model,
// voice and language, and in Advanced its key variable and endpoint.
func (m *Manager) Settings(ctx context.Context) []modules.Item {
	module := m.Descriptor(ctx)
	choice := modules.Item{Key: "provider", Kind: modules.ItemChoice, Label: "Provider", Value: "off", Display: "Disabled",
		Options: []modules.Option{{Value: "off", Label: "Disabled"}}}
	items := []modules.Item{}
	for _, provider := range module.Providers {
		choice.Options = append(choice.Options, modules.Option{Value: provider.ID, Label: provider.Name, Info: provider.Status})
		if module.Enabled && provider.ID == module.ProviderID {
			choice.Value, choice.Display = provider.ID, provider.Name+" · "+provider.Status
		}
		items = append(items, m.providerPage(provider))
	}
	return append([]modules.Item{choice}, items...)
}

func (m *Manager) providerPage(provider ProviderDescriptor) modules.Item {
	cfg := provider.Config
	key := modules.Item{Key: "api_key", Kind: modules.ItemSecret, Label: "API Key", Display: "API key required", Hint: keyHint(provider)}
	switch {
	case cfg.APIKeyConfigured && cfg.APIKeyValid:
		key.Display = cfg.APIKeyPreview
	case cfg.APIKeyConfigured:
		key.Display = provider.Status + " (" + cfg.APIKeyPreview + ")"
	}
	model := choiceOf("model", "Model", cfg.ModelID, provider.Models)
	if len(provider.Models) == 0 {
		model.Disabled, model.Hint = true, provider.Status
	}
	language := modules.Item{Key: "language", Kind: modules.ItemChoice, Label: "Language", Value: cfg.Language}
	for _, option := range provider.Languages {
		language.Options = append(language.Options, modules.Option{Value: option.Code, Label: option.Name})
	}
	return modules.Item{Key: provider.ID, Kind: modules.ItemPage, Label: provider.Name, Display: provider.Status, Items: []modules.Item{
		key, model, choiceOf("voice", "Voice", cfg.VoiceID, provider.Voices), language,
		{Key: "advanced", Kind: modules.ItemPage, Label: "Advanced", Items: []modules.Item{
			{Key: "api_key_env", Kind: modules.ItemText, Label: "API Key Env", Value: cfg.APIKeyEnv,
				Display: firstNonEmpty(cfg.APIKeyEnv, "Default env fallbacks"), Hint: firstNonEmpty(strings.Join(provider.KeyEnvs, " or "), "API_KEY")},
			{Key: "endpoint", Kind: modules.ItemText, Label: "Endpoint", Value: cfg.Endpoint, Display: cfg.Endpoint, Hint: "wss://..."},
		}},
	}}
}

func choiceOf(key string, label string, value string, values []string) modules.Item {
	choice := modules.Item{Key: key, Kind: modules.ItemChoice, Label: label, Value: value}
	for _, option := range values {
		choice.Options = append(choice.Options, modules.Option{Value: option, Label: option})
	}
	return choice
}

func keyHint(provider ProviderDescriptor) string {
	switch provider.ID {
	case ProviderGrok:
		return "xai-..."
	case ProviderOpenAI:
		return "sk-..."
	default:
		return "AIza..."
	}
}

// Change picks the provider (one that is not ready opens its page) or sets
// one of a provider's settings; values equal to the provider's defaults are
// not stored.
func (m *Manager) Change(ctx context.Context, path []string, value string) (modules.Change, error) {
	if len(path) == 1 && path[0] == "provider" {
		if value == "off" {
			return modules.Change{Config: func(cfg *setup.Config) error { cfg.Modules.RealtimeVoice.Enabled = false; return nil }}, nil
		}
		spec, ok := m.spec(value)
		if !ok {
			return modules.Change{}, fmt.Errorf("%w: unknown realtime voice provider %q", modules.ErrInvalidSetting, value)
		}
		provider := providerDescriptorByID(m.Descriptor(ctx).Providers, spec.ID)
		ready := provider.Configured
		change := modules.Change{Config: func(cfg *setup.Config) error {
			cfg.Modules.RealtimeVoice.Enabled, cfg.Modules.RealtimeVoice.ProviderID = ready, spec.ID
			return nil
		}}
		if !ready {
			change.Open, change.Message = []string{spec.ID}, spec.Name+": "+provider.Status+"."
		}
		return change, nil
	}
	if len(path) < 2 {
		return modules.Change{}, modules.Unknown(path)
	}
	spec, ok := m.spec(path[0])
	if !ok {
		return modules.Change{}, modules.Unknown(path)
	}
	field := path[len(path)-1]
	var set func(*setup.VoiceProviderConfig)
	switch {
	case len(path) == 2 && field == "api_key":
		set = func(c *setup.VoiceProviderConfig) { c.APIKey = value }
	case len(path) == 2 && field == "model":
		models := providerDescriptorByID(m.Descriptor(ctx).Providers, spec.ID).Models
		if value != "" && !spec.hasModel(value, models) {
			return modules.Change{}, fmt.Errorf("%w: %s has no model %q", modules.ErrInvalidSetting, spec.Name, value)
		}
		set = func(c *setup.VoiceProviderConfig) { c.ModelID = value }
	case len(path) == 2 && field == "voice":
		if value != "" && !slices.ContainsFunc(spec.Voices, func(voice string) bool { return strings.EqualFold(voice, value) }) {
			return modules.Change{}, fmt.Errorf("%w: %s has no voice %q", modules.ErrInvalidSetting, spec.Name, value)
		}
		set = func(c *setup.VoiceProviderConfig) { c.VoiceID = value }
	case len(path) == 2 && field == "language":
		set = func(c *setup.VoiceProviderConfig) { c.Language = value }
	case len(path) == 3 && path[1] == "advanced" && field == "api_key_env":
		set = func(c *setup.VoiceProviderConfig) { c.APIKeyEnv = value }
	case len(path) == 3 && path[1] == "advanced" && field == "endpoint":
		set = func(c *setup.VoiceProviderConfig) { c.Endpoint = value }
	default:
		return modules.Change{}, modules.Unknown(path)
	}
	return modules.Change{Config: func(cfg *setup.Config) error {
		edit(&cfg.Modules.RealtimeVoice, spec, set)
		return nil
	}}, nil
}

// edit changes spec's stored settings and drops the values equal to its
// defaults.
func edit(module *setup.VoiceModuleConfig, spec ProviderSpec, set func(*setup.VoiceProviderConfig)) {
	next := module.Providers[spec.ID]
	set(&next)
	next.APIKey, next.APIKeyEnv = strings.TrimSpace(next.APIKey), strings.TrimSpace(next.APIKeyEnv)
	next.Endpoint = omitDefault(next.Endpoint, spec.Endpoint)
	next.ModelID = omitDefault(next.ModelID, spec.DefaultModel)
	next.VoiceID = omitDefault(next.VoiceID, spec.DefaultVoice)
	next.Language = omitDefault(spec.NormalizeLanguage(next.Language), "auto")
	next.RuntimeMode, next.BinaryPath, next.Threads, next.Autostart = "", "", 0, false
	if module.Providers == nil {
		module.Providers = map[string]setup.VoiceProviderConfig{}
	}
	module.Providers[spec.ID] = next
}
