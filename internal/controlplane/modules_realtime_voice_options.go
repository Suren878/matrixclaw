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
	options := realtimeVoiceLanguageOptions(provider)
	out := make([]string, 0, len(options))
	for _, option := range options {
		out = append(out, option.id)
	}
	return out
}

func realtimeVoiceLanguageOptions(provider realtime.ProviderDescriptor) []struct{ id, title string } {
	if provider.ID == realtime.ProviderGrok {
		return []struct{ id, title string }{
			{id: "auto", title: "Auto"},
			{id: "en", title: "English"},
			{id: "ar-EG", title: "Arabic (Egypt)"},
			{id: "ar-SA", title: "Arabic (Saudi Arabia)"},
			{id: "ar-AE", title: "Arabic (United Arab Emirates)"},
			{id: "bn", title: "Bengali"},
			{id: "zh", title: "Chinese"},
			{id: "fr", title: "French"},
			{id: "de", title: "German"},
			{id: "hi", title: "Hindi"},
			{id: "id", title: "Indonesian"},
			{id: "it", title: "Italian"},
			{id: "ja", title: "Japanese"},
			{id: "ko", title: "Korean"},
			{id: "pt-BR", title: "Portuguese (Brazil)"},
			{id: "pt-PT", title: "Portuguese (Portugal)"},
			{id: "ru", title: "Russian"},
			{id: "es-MX", title: "Spanish (Mexico)"},
			{id: "es-ES", title: "Spanish (Spain)"},
			{id: "tr", title: "Turkish"},
			{id: "vi", title: "Vietnamese"},
		}
	}
	return []struct{ id, title string }{
		{id: "auto", title: "Auto"},
		{id: "ar-EG", title: "Arabic (Egyptian)"},
		{id: "bn-BD", title: "Bengali (Bangladesh)"},
		{id: "nl-NL", title: "Dutch (Netherlands)"},
		{id: "en-IN", title: "English (India)"},
		{id: "en-US", title: "English (US)"},
		{id: "fr-FR", title: "French (France)"},
		{id: "de-DE", title: "German (Germany)"},
		{id: "hi-IN", title: "Hindi (India)"},
		{id: "id-ID", title: "Indonesian (Indonesia)"},
		{id: "it-IT", title: "Italian (Italy)"},
		{id: "ja-JP", title: "Japanese (Japan)"},
		{id: "ko-KR", title: "Korean (Korea)"},
		{id: "mr-IN", title: "Marathi (India)"},
		{id: "pl-PL", title: "Polish (Poland)"},
		{id: "pt-BR", title: "Portuguese (Brazil)"},
		{id: "ro-RO", title: "Romanian (Romania)"},
		{id: "ru-RU", title: "Russian (Russia)"},
		{id: "es-US", title: "Spanish (US)"},
		{id: "ta-IN", title: "Tamil (India)"},
		{id: "te-IN", title: "Telugu (India)"},
		{id: "th-TH", title: "Thai (Thailand)"},
		{id: "tr-TR", title: "Turkish (Turkey)"},
		{id: "uk-UA", title: "Ukrainian (Ukraine)"},
		{id: "vi-VN", title: "Vietnamese (Vietnam)"},
	}
}

func normalizeRealtimeVoiceLanguage(provider realtime.ProviderDescriptor, language string) string {
	if provider.ID == realtime.ProviderGrok {
		return normalizeGrokVoiceLanguage(language)
	}
	value := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch value {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	case "ar", "ar-eg":
		return "ar-EG"
	case "bn", "bn-bd":
		return "bn-BD"
	case "nl", "nl-nl":
		return "nl-NL"
	case "en", "en-us":
		return "en-US"
	case "en-in":
		return "en-IN"
	case "fr", "fr-fr":
		return "fr-FR"
	case "de", "de-de":
		return "de-DE"
	case "hi", "hi-in":
		return "hi-IN"
	case "id", "id-id":
		return "id-ID"
	case "it", "it-it":
		return "it-IT"
	case "ja", "ja-jp":
		return "ja-JP"
	case "ko", "ko-kr":
		return "ko-KR"
	case "mr", "mr-in":
		return "mr-IN"
	case "pl", "pl-pl":
		return "pl-PL"
	case "pt", "pt-br":
		return "pt-BR"
	case "ro", "ro-ro":
		return "ro-RO"
	case "ru", "ru-ru":
		return "ru-RU"
	case "es", "es-us":
		return "es-US"
	case "ta", "ta-in":
		return "ta-IN"
	case "te", "te-in":
		return "te-IN"
	case "th", "th-th":
		return "th-TH"
	case "tr", "tr-tr":
		return "tr-TR"
	case "uk", "uk-ua":
		return "uk-UA"
	case "vi", "vi-vn":
		return "vi-VN"
	default:
		return strings.TrimSpace(language)
	}
}

func normalizeGrokVoiceLanguage(language string) string {
	value := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch value {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	case "en", "en-us", "en-gb", "bn", "bn-bd", "zh", "zh-cn", "fr", "fr-fr", "de", "de-de", "hi", "hi-in", "id", "id-id", "it", "it-it", "ja", "ja-jp", "ko", "ko-kr", "ru", "ru-ru", "tr", "tr-tr", "vi", "vi-vn":
		if before, _, ok := strings.Cut(value, "-"); ok {
			return before
		}
		return value
	case "ar", "ar-eg":
		return "ar-EG"
	case "ar-sa":
		return "ar-SA"
	case "ar-ae":
		return "ar-AE"
	case "pt", "pt-br":
		return "pt-BR"
	case "pt-pt":
		return "pt-PT"
	case "es", "es-mx":
		return "es-MX"
	case "es-es":
		return "es-ES"
	default:
		return strings.TrimSpace(language)
	}
}

func realtimeVoiceAPIKeyPlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return firstNonEmptyTrimmed(provider.Config.APIKeyPreview, "xai-...")
	default:
		return firstNonEmptyTrimmed(provider.Config.APIKeyPreview, "AIza...")
	}
}

func realtimeVoiceAPIKeyEnvPlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "XAI_API_KEY"
	default:
		return "MATRIXCLAW_GEMINI_LIVE_API_KEY"
	}
}

func realtimeVoiceVoicePlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "eve"
	default:
		return "Puck"
	}
}

func realtimeVoiceLanguagePlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "auto or ru"
	default:
		return "auto or ru-RU"
	}
}

func realtimeVoiceEndpointPlaceholder(provider realtime.ProviderDescriptor) string {
	switch provider.ID {
	case realtime.ProviderGrok:
		return "wss://api.x.ai/v1/realtime"
	default:
		return "wss://generativelanguage.googleapis.com/..."
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
