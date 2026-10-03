package providers

import "strings"

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
