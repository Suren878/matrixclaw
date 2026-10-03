package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
)

func realtimeVoiceModelCandidates(provider realtime.ProviderDescriptor) []string {
	out := make([]string, 0, len(provider.Models))
	seen := map[string]struct{}{}
	for _, value := range provider.Models {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func realtimeVoiceVoiceCandidates(provider realtime.ProviderDescriptor) []string {
	out := make([]string, 0, len(provider.Voices))
	seen := map[string]struct{}{}
	for _, value := range provider.Voices {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func realtimeVoiceLanguageCandidates(provider realtime.ProviderDescriptor) []string {
	out := make([]string, 0, len(provider.Languages))
	for _, language := range provider.Languages {
		out = append(out, language.Code)
	}
	return out
}

func realtimeVoiceAPIKeyPlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return firstNonEmptyTrimmed(provider.Config.APIKeyPreview, "xai-...")
	case realtime.ProviderOpenAI:
		return firstNonEmptyTrimmed(provider.Config.APIKeyPreview, "sk-...")
	default:
		return firstNonEmptyTrimmed(provider.Config.APIKeyPreview, "AIza...")
	}
}

func realtimeVoiceLanguagePlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "auto or ru"
	case realtime.ProviderOpenAI:
		return "auto or ru-RU"
	default:
		return "auto or ru-RU"
	}
}

func realtimeVoiceModelUnavailableMessage(provider realtime.ProviderDescriptor, models []string) string {
	if !provider.Config.APIKeyConfigured {
		return "API key required"
	}
	if !provider.Config.APIKeyValid {
		return firstNonEmptyTrimmed(provider.Status, "Invalid API key")
	}
	if len(models) == 0 {
		return firstNonEmptyTrimmed(provider.Status, "No realtime models available")
	}
	return ""
}

func stringInSliceFold(value string, values []string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, candidate := range values {
		if strings.EqualFold(value, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}
