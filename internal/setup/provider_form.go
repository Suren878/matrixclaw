package setup

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type ProviderFormFieldID string

const (
	ProviderFormFieldName            ProviderFormFieldID = "name"
	ProviderFormFieldBaseURL         ProviderFormFieldID = "base_url"
	ProviderFormFieldAPIKey          ProviderFormFieldID = "api_key"
	ProviderFormFieldModel           ProviderFormFieldID = "model"
	ProviderFormFieldReasoningEffort ProviderFormFieldID = "reasoning_effort"
	ProviderFormFieldToolUse         ProviderFormFieldID = "tool_use"
)

type ProviderFormSpecInput struct {
	ID                  string
	CatalogID           string
	Name                string
	Type                string
	APIKey              string
	BaseURL             string
	BaseURLOptions      []providers.BaseURLOption
	Model               string
	ReasoningEffort     string
	ToolUseMode         providers.ToolUseMode
	HasStoredAPIKey     bool
	StoredAPIKeyPreview string

	Custom            bool
	CustomKnown       bool
	Capabilities      providers.Capabilities
	CapabilitiesKnown bool
}

type ProviderFormSpec struct {
	ID           string
	CatalogID    string
	Type         string
	Custom       bool
	Capabilities providers.Capabilities
	Fields       []ProviderFormField
}

type ProviderFormField struct {
	ID        ProviderFormFieldID
	Label     string
	Value     string
	Status    string
	Options   []string
	Required  bool
	Sensitive bool
	Editable  bool
	Picker    bool
}

func ProviderFormSpecForDraft(provider ProviderDraft) ProviderFormSpec {
	return ProviderFormSpecFromInput(ProviderFormSpecInput{
		ID:                  provider.ID,
		CatalogID:           provider.CatalogID,
		Name:                provider.Name,
		Type:                provider.Type,
		APIKey:              provider.APIKey,
		BaseURL:             provider.BaseURL,
		Model:               provider.Model,
		ReasoningEffort:     provider.ReasoningEffort,
		ToolUseMode:         provider.ToolUseMode,
		HasStoredAPIKey:     provider.HasStoredAPIKey,
		StoredAPIKeyPreview: provider.StoredAPIKeyPreview,
	})
}

func ProviderFormSpecForSetupItem(item ProviderSetupItem) ProviderFormSpec {
	return ProviderFormSpecFromInput(ProviderFormSpecInput{
		ID:                  item.ID,
		CatalogID:           item.CatalogID,
		Name:                item.Name,
		Type:                item.Type,
		BaseURL:             item.BaseURL,
		BaseURLOptions:      item.BaseURLOptions,
		Model:               firstNonEmptyTrimmed(item.Model, item.DefaultModel),
		ReasoningEffort:     item.ReasoningEffort,
		ToolUseMode:         item.ToolUseMode,
		HasStoredAPIKey:     strings.TrimSpace(item.APIKeyPreview) != "",
		StoredAPIKeyPreview: item.APIKeyPreview,
		Capabilities:        item.Capabilities,
		CapabilitiesKnown:   item.Capabilities != (providers.Capabilities{}),
	})
}

func ProviderFormSpecFromInput(input ProviderFormSpecInput) ProviderFormSpec {
	providerID := providers.NormalizeProviderID(input.ID)
	catalogID := providers.NormalizeProviderID(input.CatalogID)
	providerType := providers.NormalizeOptionalProviderType(input.Type)
	custom := input.Custom
	if !input.CustomKnown {
		custom = catalogID == ""
	}
	capabilities := input.Capabilities
	reasoningOptions := []string(nil)
	defaultReasoningEffort := ""
	if !input.CapabilitiesKnown {
		capabilitySet := providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
			ProviderID:   firstNonEmptyTrimmed(catalogID, providerID),
			ProviderType: providerType,
			ModelID:      input.Model,
		})
		capabilities = capabilitySet.ProviderCapabilities
		reasoningOptions = capabilitySet.ReasoningEfforts
		defaultReasoningEffort = capabilitySet.DefaultReasoningEffort
	} else if capabilities.ReasoningEffort {
		reasoningOptions = providers.ReasoningEffortsForModel(firstNonEmptyTrimmed(catalogID, providerID), providerType, input.Model)
		if len(reasoningOptions) == 0 {
			reasoningOptions = providers.ReasoningEfforts()
		}
		defaultReasoningEffort = providers.DefaultReasoningEffort
	}
	baseURLOptions := append([]providers.BaseURLOption(nil), input.BaseURLOptions...)
	if len(baseURLOptions) == 0 {
		baseURLOptions = providerBaseURLOptions(firstNonEmptyTrimmed(catalogID, providerID))
	}

	fields := make([]ProviderFormField, 0, 6)
	if custom {
		fields = append(fields,
			ProviderFormField{
				ID:       ProviderFormFieldName,
				Label:    "Provider name",
				Value:    strings.TrimSpace(input.Name),
				Status:   strings.TrimSpace(input.Name),
				Required: true,
				Editable: true,
			},
		)
	}
	if custom || len(baseURLOptions) > 0 {
		fields = append(fields, ProviderFormField{
			ID:       ProviderFormFieldBaseURL,
			Label:    providerBaseURLFieldLabel(baseURLOptions),
			Value:    strings.TrimSpace(input.BaseURL),
			Status:   providerBaseURLStatus(input.BaseURL, baseURLOptions),
			Options:  providerBaseURLValues(baseURLOptions),
			Required: true,
			Editable: true,
			Picker:   len(baseURLOptions) > 0,
		})
	}

	fields = append(fields,
		ProviderFormField{
			ID:        ProviderFormFieldAPIKey,
			Label:     "API key",
			Value:     strings.TrimSpace(input.APIKey),
			Status:    providerFormAPIKeyStatus(input.APIKey, input.HasStoredAPIKey, input.StoredAPIKeyPreview),
			Required:  true,
			Sensitive: true,
			Editable:  true,
		},
		ProviderFormField{
			ID:       ProviderFormFieldModel,
			Label:    "Model",
			Value:    strings.TrimSpace(input.Model),
			Status:   strings.TrimSpace(input.Model),
			Required: true,
			Editable: true,
			Picker:   !custom && capabilities.ModelDiscovery,
		},
	)

	if capabilities.ReasoningEffort {
		fields = append(fields, ProviderFormField{
			ID:       ProviderFormFieldReasoningEffort,
			Label:    "Reasoning effort",
			Value:    strings.TrimSpace(input.ReasoningEffort),
			Status:   firstNonEmptyTrimmed(input.ReasoningEffort, defaultReasoningEffort),
			Options:  reasoningOptions,
			Picker:   true,
			Editable: true,
		})
	}
	if capabilities.ToolCalling {
		fields = append(fields, ProviderFormField{
			ID:       ProviderFormFieldToolUse,
			Label:    "Tool use",
			Value:    string(providers.NormalizeOptionalToolUseMode(input.ToolUseMode)),
			Status:   ProviderFormToolUseModeStatus(input.ToolUseMode),
			Picker:   true,
			Editable: true,
		})
	}

	return ProviderFormSpec{
		ID:           providerID,
		CatalogID:    catalogID,
		Type:         providerType,
		Custom:       custom,
		Capabilities: capabilities,
		Fields:       fields,
	}
}

func providerBaseURLOptions(providerID string) []providers.BaseURLOption {
	entry, ok := providers.CatalogEntryByID(providerID)
	if !ok || len(entry.BaseURLOptions) == 0 {
		return nil
	}
	return append([]providers.BaseURLOption(nil), entry.BaseURLOptions...)
}

func providerBaseURLValues(options []providers.BaseURLOption) []string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		if value := strings.TrimSpace(option.URL); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func providerBaseURLStatus(baseURL string, options []providers.BaseURLOption) string {
	baseURL = strings.TrimSpace(baseURL)
	for _, option := range options {
		if strings.TrimSpace(option.URL) == baseURL {
			return strings.TrimSpace(option.Name)
		}
	}
	return baseURL
}

func providerBaseURLFieldLabel(options []providers.BaseURLOption) string {
	if len(options) > 0 {
		return "Endpoint"
	}
	return "Base URL"
}

func (spec ProviderFormSpec) Field(id ProviderFormFieldID) (ProviderFormField, bool) {
	for _, field := range spec.Fields {
		if field.ID == id {
			return field, true
		}
	}
	return ProviderFormField{}, false
}

func ProviderFormToolUseModes() []providers.ToolUseMode {
	return []providers.ToolUseMode{providers.ToolUseNative, providers.ToolUseDisabled}
}

func ProviderFormToolUseModeStatus(mode providers.ToolUseMode) string {
	switch providers.NormalizeToolUseMode(mode) {
	case providers.ToolUseDisabled:
		return "Disabled"
	default:
		return "Enabled"
	}
}

func providerFormAPIKeyStatus(apiKey string, hasStoredAPIKey bool, storedPreview string) string {
	if strings.TrimSpace(apiKey) != "" {
		return MaskSecret(apiKey)
	}
	if hasStoredAPIKey {
		return strings.TrimSpace(storedPreview)
	}
	return ""
}
