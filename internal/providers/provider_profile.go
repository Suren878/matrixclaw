package providers

import "strings"

type ProviderProfile struct {
	ProviderType            string
	RuntimeProviderType     string
	RuntimeProfile          RuntimeProfile
	SupportsReasoningEffort bool
}

func ProfileForProvider(providerType string) ProviderProfile {
	providerType = normalizeProviderTypeForProfile(providerType)
	profile := ProviderProfile{
		ProviderType:        providerType,
		RuntimeProviderType: providerType,
		RuntimeProfile:      runtimeProfileDefaults(providerType),
	}
	if providerType == TypeOpenAICompat {
		profile.SupportsReasoningEffort = true
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
