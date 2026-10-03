package setup

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// policy is the catalog entry for a built-in provider, or the type defaults
// for a custom one.
func (p ProviderConfig) policy() providers.ProviderPolicy {
	return providers.PolicyForProvider(p.ID, p.Type)
}

// Effective fills the catalog defaults the stored config leaves out: name,
// type, key env name, base URL, model, and the model's reasoning effort.
func (p ProviderConfig) Effective() ProviderConfig {
	policy := p.policy()
	if policy.Known {
		p.ID = policy.CatalogID
		p.Name = cmp.Or(p.Name, policy.Name)
		p.Type = cmp.Or(p.Type, policy.Type)
		p.APIKeyEnv = cmp.Or(p.APIKeyEnv, policy.APIKeyEnv)
		p.BaseURL = cmp.Or(p.BaseURL, policy.DefaultBaseURL)
		p.Model = cmp.Or(p.Model, policy.DefaultModel)
	} else {
		p.Name = cmp.Or(p.Name, p.ID)
		p.APIKeyEnv = cmp.Or(p.APIKeyEnv, customProviderAPIKeyEnv(p.Type))
	}
	p.Model = providers.NormalizeModelID(p.ID, p.Type, p.Model)
	capabilities := providers.ResolveModelCapabilities(providers.ModelCapabilityInput{ProviderID: p.ID, ProviderType: p.Type, ModelID: p.Model})
	p.ReasoningEffort = providers.NormalizeReasoningEffortForModel(p.ID, p.Type, p.Model, p.ReasoningEffort)
	if !capabilities.ProviderCapabilities.ToolCalling {
		p.ToolUseMode = ""
	}
	return p
}

// Runtime is the effective provider with its API key taken from the file or
// the environment; ok is false when a required key is missing.
func (p ProviderConfig) Runtime() (ProviderConfig, bool) {
	p = p.Effective()
	if !p.policy().RequiresAPIKey {
		p.APIKey = ""
		return p, true
	}
	p.APIKey = cmp.Or(normalizeProviderAPIKey(p.APIKey), providerAPIKeyFromEnvName(p.APIKeyEnv))
	return p, p.APIKey != ""
}

// APIKeyPreview shows where the provider's key comes from without the key.
func (p ProviderConfig) APIKeyPreview() string {
	p = p.Effective()
	if policy := p.policy(); !policy.RequiresAPIKey {
		return policy.AuthStatusLabel
	}
	if apiKey := normalizeProviderAPIKey(p.APIKey); apiKey != "" {
		return MaskSecret(apiKey)
	}
	if p.APIKeyEnv != "" && providerAPIKeyFromEnvName(p.APIKeyEnv) != "" {
		return "env:" + p.APIKeyEnv
	}
	return ""
}

// CheckProvider reports whether p can be saved and used: a valid entry with
// its API key available.
func CheckProvider(p ProviderConfig) error {
	if err := p.validate(); err != nil {
		return err
	}
	if _, ok := p.Runtime(); !ok {
		return fmt.Errorf("%s API key is required", p.Effective().Name)
	}
	return nil
}

func (p ProviderConfig) validate() error {
	if p.ID == "" {
		return errors.New("provider id is required")
	}
	policy := p.policy()
	if policy.Known && !policy.Implemented {
		return fmt.Errorf("provider %q is listed but not implemented yet", policy.Name)
	}
	if !policy.Known {
		if !isCustomProviderType(p.Type) {
			return fmt.Errorf("unsupported custom provider type %q", p.Type)
		}
		if p.Name == "" {
			return errors.New("provider name is required")
		}
	}
	effective := p.Effective()
	if policy.RequiresBaseURL && effective.BaseURL == "" {
		return fmt.Errorf("%s base URL is required", effective.Name)
	}
	if effective.Model == "" {
		return fmt.Errorf("%s model is required", effective.Name)
	}
	if p.ContextWindow < 0 {
		return errors.New("context window must be a positive integer")
	}
	if p.MaxOutputTokens < 0 {
		return errors.New("max output tokens must be a positive integer")
	}
	return nil
}

// NewProviderConfig starts a provider that is not saved yet: a built-in one by
// catalog id, otherwise a custom one of providerType.
func NewProviderConfig(providerID string, providerType string, name string) (ProviderConfig, error) {
	providerID = providers.NormalizeProviderID(providerID)
	if providerID == "" {
		return ProviderConfig{}, errors.New("provider id is required")
	}
	if policy := providers.PolicyForProvider(providerID, ""); policy.Known {
		if !policy.Implemented {
			return ProviderConfig{}, fmt.Errorf("provider %q is listed but not implemented yet", policy.Name)
		}
		return ProviderConfig{ID: policy.CatalogID}, nil
	}
	providerType = providers.NormalizeOptionalProviderType(providerType)
	if !isCustomProviderType(providerType) {
		return ProviderConfig{}, fmt.Errorf("unknown provider %q", providerID)
	}
	return ProviderConfig{ID: providerID, Name: cmp.Or(strings.TrimSpace(name), providerID), Type: providerType}, nil
}

// applyProviderUpdate sets the fields present in update; an empty value
// resets a field to its default.
func applyProviderUpdate(p *ProviderConfig, update ProviderSetupUpdate) error {
	custom := !p.policy().Known
	if custom && update.Name != nil {
		p.Name = strings.TrimSpace(*update.Name)
	}
	if custom && update.Type != nil {
		providerType := providers.NormalizeOptionalProviderType(*update.Type)
		if !isCustomProviderType(providerType) {
			return fmt.Errorf("unsupported custom provider type %q", providerType)
		}
		p.Type = providerType
	}
	if update.BaseURL != nil {
		next := *p
		next.BaseURL = strings.TrimSpace(*update.BaseURL)
		if update.APIKey == nil && normalizeProviderAPIKey(p.APIKey) != "" && next.Effective().BaseURL != p.Effective().BaseURL {
			return errors.New("changing provider base URL requires re-entering the API key")
		}
		p.BaseURL = next.BaseURL
	}
	if update.APIKey != nil {
		p.APIKey = normalizeProviderAPIKey(*update.APIKey)
	}
	if update.Model != nil {
		p.Model = providers.NormalizeModelID(p.ID, p.Type, *update.Model)
	}
	if update.ContextWindow != nil {
		p.ContextWindow = max(*update.ContextWindow, 0)
	}
	if update.ReasoningEffort != nil {
		p.ReasoningEffort = providers.NormalizeReasoningEffort(*update.ReasoningEffort)
	}
	if update.ToolUseMode != nil {
		p.ToolUseMode = providers.NormalizeOptionalToolUseMode(*update.ToolUseMode)
	}
	return nil
}

func isCustomProviderType(providerType string) bool {
	switch providerType {
	case providers.TypeOpenAICompat, providers.TypeOpenAICodex, providers.TypeAnthropic:
		return true
	default:
		return false
	}
}

func customProviderAPIKeyEnv(providerType string) string {
	switch providerType {
	case providers.TypeAnthropic:
		return "ANTHROPIC_API_KEY"
	case providers.TypeOpenAICompat:
		return "OPENAI_COMPAT_API_KEY"
	default:
		return ""
	}
}

// Provider returns the stored provider with id.
func (cfg Config) Provider(id string) (ProviderConfig, bool) {
	for _, provider := range cfg.Providers {
		if sameProvider(provider.ID, id) {
			return provider, true
		}
	}
	return ProviderConfig{}, false
}

// ActiveProvider returns the stored active provider.
func (cfg Config) ActiveProvider() (ProviderConfig, bool) {
	if cfg.ActiveProviderID == "" {
		return ProviderConfig{}, false
	}
	return cfg.Provider(cfg.ActiveProviderID)
}

// SetProvider adds p or replaces the stored provider with its id.
func (cfg *Config) SetProvider(p ProviderConfig) {
	for i, provider := range cfg.Providers {
		if sameProvider(provider.ID, p.ID) {
			cfg.Providers[i] = p
			return
		}
	}
	cfg.Providers = append(cfg.Providers, p)
}

func (cfg *Config) removeProvider(id string) {
	kept := cfg.Providers[:0]
	for _, provider := range cfg.Providers {
		if !sameProvider(provider.ID, id) {
			kept = append(kept, provider)
		}
	}
	cfg.Providers = kept
	if _, ok := cfg.ActiveProvider(); !ok {
		cfg.ActiveProviderID = ""
		if len(cfg.Providers) > 0 {
			cfg.ActiveProviderID = cfg.Providers[0].ID
		}
	}
}

// ProviderItems lists the configured providers, the active one first, then
// the built-in providers that are not configured yet.
func ProviderItems(cfg Config) []ProviderSetupItem {
	items := make([]ProviderSetupItem, 0, len(cfg.Providers)+8)
	seen := map[string]bool{}
	add := func(p ProviderConfig, configured bool, active bool) {
		id := providers.CanonicalProviderID(p.ID)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		items = append(items, providerItem(p, configured, active))
	}
	if active, ok := cfg.ActiveProvider(); ok {
		add(active, true, true)
	}
	for _, provider := range cfg.Providers {
		add(provider, true, false)
	}
	for _, spec := range providers.ProviderSpecs() {
		if spec.Entry.Implemented {
			add(ProviderConfig{ID: spec.Entry.ID}, false, false)
		}
	}
	return items
}

func providerItem(p ProviderConfig, configured bool, active bool) ProviderSetupItem {
	effective := p.Effective()
	policy := p.policy()
	capabilities := providers.ResolveModelCapabilities(providers.ModelCapabilityInput{ProviderID: effective.ID, ProviderType: effective.Type, ModelID: effective.Model})
	item := ProviderSetupItem{
		ID:              effective.ID,
		Name:            effective.Name,
		Type:            effective.Type,
		Configured:      configured,
		Active:          active,
		Implemented:     true,
		RequiresBaseURL: policy.RequiresBaseURL,
		Capabilities:    capabilities.ProviderCapabilities,
		BaseURL:         effective.BaseURL,
		BaseURLOptions:  policy.BaseURLOptions,
		ReasoningEffort: effective.ReasoningEffort,
		APIKeyPreview:   p.APIKeyPreview(),
	}
	if policy.Known {
		item.CatalogID = policy.CatalogID
	}
	if !configured {
		item.DefaultModel = effective.Model
		item.Notes = policy.Notes
		return item
	}
	item.Model = effective.Model
	item.ContextWindow = effective.ContextWindow
	item.ToolUseMode = effective.ToolUseMode
	item.Status = strings.Join(append([]string{"Configured", effective.Model}, activeLabel(active)...), " · ")
	return item
}

func activeLabel(active bool) []string {
	if active {
		return []string{"Active"}
	}
	return nil
}
