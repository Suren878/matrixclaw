package providers

import "strings"

type ProviderProfile struct {
	ProviderID              string
	Model                   string
	ProviderType            string
	RuntimeProviderType     string
	RuntimeProfile          RuntimeProfile
	Capabilities            ModelCapabilities
	SupportsReasoningEffort bool
}

func ProfileForProvider(providerType string) ProviderProfile {
	return ProfileForModel("", providerType, "")
}

func ProfileForModel(providerID string, providerType string, modelID string) ProviderProfile {
	providerID = NormalizeProviderID(providerID)
	providerType = normalizeProviderTypeForProfile(providerType)
	modelID = strings.TrimSpace(modelID)
	capabilitySet := ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   providerID,
		ProviderType: providerType,
		ModelID:      modelID,
	})
	capabilities := capabilitySet.RuntimeCapabilities
	runtimeProfile := runtimeProfileDefaults(providerType)
	if capabilities.ToolSchemaDialect != "" {
		runtimeProfile.ToolSchemaDialect = capabilities.ToolSchemaDialect
	}
	if !capabilities.ToolCalling {
		runtimeProfile.ToolUseMode = ToolUseDisabled
	}
	runtimeProfile = NormalizeRuntimeProfile(runtimeProfile)

	profile := ProviderProfile{
		ProviderID:              providerID,
		Model:                   modelID,
		ProviderType:            providerType,
		RuntimeProviderType:     providerType,
		RuntimeProfile:          runtimeProfile,
		Capabilities:            capabilities,
		SupportsReasoningEffort: capabilities.ReasoningEffort,
	}
	return profile
}

func (p ProviderProfile) RuntimeProfileWithOverrides(profile RuntimeProfile) RuntimeProfile {
	if profile.ToolUseMode == "" && profile.ToolSchemaDialect == "" {
		return p.RuntimeProfile
	}
	if p.ProviderType == TypeGemini {
		if profile.ToolUseMode == "" {
			profile.ToolUseMode = p.RuntimeProfile.ToolUseMode
		}
		if profile.ToolSchemaDialect == "" {
			profile.ToolSchemaDialect = p.RuntimeProfile.ToolSchemaDialect
		}
	}
	return NormalizeRuntimeProfile(profile)
}

func normalizeProviderTypeForProfile(providerType string) string {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "", TypeOpenAICompat:
		return TypeOpenAICompat
	case TypeAnthropic:
		return TypeAnthropic
	case TypeGemini:
		return TypeGemini
	default:
		return strings.ToLower(strings.TrimSpace(providerType))
	}
}
