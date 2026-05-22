package providers

import "strings"

type ProviderProfile struct {
	ProviderID              string
	Model                   string
	ProviderType            string
	RuntimeProviderType     string
	AuthMode                ProviderAuthMode
	Transport               ProviderTransport
	RuntimeProfile          RuntimeProfile
	Capabilities            ModelCapabilities
	SupportsReasoningEffort bool
	OpenAIChat              OpenAIChatOptions
}

func ProfileForProvider(providerType string) ProviderProfile {
	return ProfileForModel("", providerType, "")
}

func (p ProviderProfile) IsZero() bool {
	return p.ProviderID == "" && p.ProviderType == "" && p.RuntimeProviderType == ""
}

func ProfileForModel(providerID string, providerType string, modelID string) ProviderProfile {
	providerID = NormalizeProviderID(providerID)
	providerType = NormalizeProviderType(providerType)
	modelID = strings.TrimSpace(modelID)
	policy := PolicyForProvider(providerID, providerType)
	capabilitySet := ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   providerID,
		ProviderType: providerType,
		ModelID:      modelID,
	})
	capabilities := capabilitySet.RuntimeCapabilities
	runtimeProfile := runtimeProfileDefaults(policy.RuntimeProviderType)
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
		RuntimeProviderType:     policy.RuntimeProviderType,
		AuthMode:                policy.AuthMode,
		Transport:               policy.Transport,
		RuntimeProfile:          runtimeProfile,
		Capabilities:            capabilities,
		SupportsReasoningEffort: capabilities.ReasoningEffort,
		OpenAIChat:              cloneOpenAIChatOptions(policy.OpenAIChat),
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

func NormalizeProviderType(providerType string) string {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "", TypeOpenAICompat:
		return TypeOpenAICompat
	case TypeOpenAICodex:
		return TypeOpenAICodex
	case TypeAnthropic:
		return TypeAnthropic
	case TypeGemini:
		return TypeGemini
	default:
		return strings.ToLower(strings.TrimSpace(providerType))
	}
}

func NormalizeOptionalProviderType(providerType string) string {
	if strings.TrimSpace(providerType) == "" {
		return ""
	}
	return NormalizeProviderType(providerType)
}
