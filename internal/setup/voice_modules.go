package setup

import (
	"cmp"
	"slices"
	"strings"
)

const (
	VoiceModuleTTS = "tts"
	VoiceModuleSTT = "stt"
)

// normalizeVoiceModuleConfig trims a stored voice module and keeps, per
// provider, only the settings that differ from the provider's defaults.
func normalizeVoiceModuleConfig(moduleID string, cfg VoiceModuleConfig) VoiceModuleConfig {
	moduleID = normalizeVoiceModuleID(moduleID)
	cfg.ProviderID = normalizeVoiceProviderID(cfg.ProviderID)
	if !voiceProviderExists(moduleID, cfg.ProviderID) {
		cfg.ProviderID = ""
	}
	stored := map[string]VoiceProviderConfig{}
	for providerID, providerCfg := range cfg.Providers {
		providerID = normalizeVoiceProviderID(providerID)
		if !voiceProviderExists(moduleID, providerID) {
			continue
		}
		if providerCfg = storedVoiceProviderConfig(moduleID, providerID, providerCfg); providerCfg != (VoiceProviderConfig{}) {
			stored[providerID] = providerCfg
		}
	}
	cfg.Providers = nil
	if len(stored) > 0 {
		cfg.Providers = stored
	}
	return cfg
}

func storedVoiceProviderConfig(moduleID string, providerID string, cfg VoiceProviderConfig) VoiceProviderConfig {
	cfg = EffectiveVoiceConfig(moduleID, providerID, cfg)
	defaults := defaultVoiceProviderConfig(providerID)
	cfg.ModelID = omitDefault(cfg.ModelID, defaults.ModelID)
	cfg.VoiceID = omitDefault(cfg.VoiceID, defaults.VoiceID)
	cfg.Language = omitDefault(cfg.Language, defaults.Language)
	cfg.BinaryPath = omitDefault(cfg.BinaryPath, defaults.BinaryPath)
	cfg.Endpoint = omitDefault(cfg.Endpoint, defaults.Endpoint)
	cfg.RuntimeMode = omitDefault(cfg.RuntimeMode, "per_task")
	return cfg
}

// EffectiveVoiceConfig is a local voice provider's settings with its
// defaults filled and its language normalized.
func EffectiveVoiceConfig(moduleID string, providerID string, cfg VoiceProviderConfig) VoiceProviderConfig {
	moduleID = normalizeVoiceModuleID(moduleID)
	providerID = normalizeVoiceProviderID(providerID)
	cfg.APIKey = ""
	cfg.APIKeyEnv = ""
	cfg.ModelID = strings.TrimSpace(cfg.ModelID)
	cfg.VoiceID = strings.TrimSpace(cfg.VoiceID)
	if moduleID == VoiceModuleTTS {
		if providerID == "supertonic" {
			cfg.Language = NormalizeSupertonicLanguage(cfg.Language)
		} else {
			cfg.Language = NormalizeVoiceLanguage(cfg.Language)
		}
	} else {
		cfg.Language = strings.ToLower(strings.TrimSpace(cfg.Language))
	}
	cfg.BinaryPath = strings.TrimSpace(cfg.BinaryPath)
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.RuntimeMode = normalizeVoiceRuntimeMode(cfg.RuntimeMode)
	defaults := defaultVoiceProviderConfig(providerID)
	cfg.ModelID = cmp.Or(cfg.ModelID, defaults.ModelID)
	cfg.VoiceID = cmp.Or(cfg.VoiceID, defaults.VoiceID)
	cfg.Language = cmp.Or(cfg.Language, defaults.Language)
	cfg.BinaryPath = cmp.Or(cfg.BinaryPath, defaults.BinaryPath)
	cfg.Endpoint = cmp.Or(cfg.Endpoint, defaults.Endpoint)
	cfg.Threads = max(cfg.Threads, 0)
	return cfg
}

func normalizeVoiceRuntimeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "always_running", "persistent", "server":
		return "always_running"
	default:
		return "per_task"
	}
}

// NormalizeVoiceLanguage is a Piper language code such as en_US; "" is any.
func NormalizeVoiceLanguage(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return ""
	}
	switch strings.ToLower(language) {
	case "auto":
		return ""
	case "en", "english":
		return "en_US"
	case "ru", "russian":
		return "ru_RU"
	}
	if before, after, ok := strings.Cut(language, "_"); ok {
		before = strings.ToLower(strings.TrimSpace(before))
		after = strings.ToUpper(strings.TrimSpace(after))
		if before != "" && after != "" {
			return before + "_" + after
		}
	}
	return language
}

// NormalizeSupertonicLanguage is a Supertonic language code; "auto" detects.
func NormalizeSupertonicLanguage(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return "auto"
	}
	language = strings.ToLower(strings.ReplaceAll(language, "_", "-"))
	switch language {
	case "auto":
		return "auto"
	case "unknown", "fallback":
		return "na"
	}
	if before, _, ok := strings.Cut(language, "-"); ok {
		language = before
	}
	for _, known := range SupertonicLanguages {
		if known.Code == language {
			return language
		}
	}
	return "auto"
}

// Language is a language a voice engine speaks or understands.
type Language struct{ Code, Name string }

// SupertonicLanguages are the languages Supertonic speaks.
var SupertonicLanguages = []Language{
	{"auto", "Auto"}, {"na", "Fallback"}, {"ar", "Arabic"}, {"bg", "Bulgarian"}, {"cs", "Czech"},
	{"da", "Danish"}, {"de", "German"}, {"el", "Greek"}, {"en", "English"}, {"es", "Spanish"},
	{"et", "Estonian"}, {"fi", "Finnish"}, {"fr", "French"}, {"hi", "Hindi"}, {"hr", "Croatian"},
	{"hu", "Hungarian"}, {"id", "Indonesian"}, {"it", "Italian"}, {"ja", "Japanese"}, {"ko", "Korean"},
	{"lt", "Lithuanian"}, {"lv", "Latvian"}, {"nl", "Dutch"}, {"pl", "Polish"}, {"pt", "Portuguese"},
	{"ro", "Romanian"}, {"ru", "Russian"}, {"sk", "Slovak"}, {"sl", "Slovenian"}, {"sv", "Swedish"},
	{"tr", "Turkish"}, {"uk", "Ukrainian"}, {"vi", "Vietnamese"},
}

// WhisperLanguages are the languages whisper.cpp transcribes.
var WhisperLanguages = []Language{
	{"auto", "Auto"}, {"af", "Afrikaans"}, {"am", "Amharic"}, {"ar", "Arabic"}, {"as", "Assamese"},
	{"az", "Azerbaijani"}, {"ba", "Bashkir"}, {"be", "Belarusian"}, {"bg", "Bulgarian"}, {"bn", "Bengali"},
	{"bo", "Tibetan"}, {"br", "Breton"}, {"bs", "Bosnian"}, {"ca", "Catalan"}, {"cs", "Czech"},
	{"cy", "Welsh"}, {"da", "Danish"}, {"de", "German"}, {"el", "Greek"}, {"en", "English"},
	{"es", "Spanish"}, {"et", "Estonian"}, {"eu", "Basque"}, {"fa", "Persian"}, {"fi", "Finnish"},
	{"fo", "Faroese"}, {"fr", "French"}, {"gl", "Galician"}, {"gu", "Gujarati"}, {"ha", "Hausa"},
	{"haw", "Hawaiian"}, {"he", "Hebrew"}, {"hi", "Hindi"}, {"hr", "Croatian"}, {"ht", "Haitian Creole"},
	{"hu", "Hungarian"}, {"hy", "Armenian"}, {"id", "Indonesian"}, {"is", "Icelandic"}, {"it", "Italian"},
	{"ja", "Japanese"}, {"jw", "Javanese"}, {"ka", "Georgian"}, {"kk", "Kazakh"}, {"km", "Khmer"},
	{"kn", "Kannada"}, {"ko", "Korean"}, {"la", "Latin"}, {"lb", "Luxembourgish"}, {"ln", "Lingala"},
	{"lo", "Lao"}, {"lt", "Lithuanian"}, {"lv", "Latvian"}, {"mg", "Malagasy"}, {"mi", "Maori"},
	{"mk", "Macedonian"}, {"ml", "Malayalam"}, {"mn", "Mongolian"}, {"mr", "Marathi"}, {"ms", "Malay"},
	{"mt", "Maltese"}, {"my", "Myanmar"}, {"ne", "Nepali"}, {"nl", "Dutch"}, {"nn", "Norwegian Nynorsk"},
	{"no", "Norwegian"}, {"oc", "Occitan"}, {"pa", "Punjabi"}, {"pl", "Polish"}, {"ps", "Pashto"},
	{"pt", "Portuguese"}, {"ro", "Romanian"}, {"ru", "Russian"}, {"sa", "Sanskrit"}, {"sd", "Sindhi"},
	{"si", "Sinhala"}, {"sk", "Slovak"}, {"sl", "Slovenian"}, {"sn", "Shona"}, {"so", "Somali"},
	{"sq", "Albanian"}, {"sr", "Serbian"}, {"su", "Sundanese"}, {"sv", "Swedish"}, {"sw", "Swahili"},
	{"ta", "Tamil"}, {"te", "Telugu"}, {"tg", "Tajik"}, {"th", "Thai"}, {"tk", "Turkmen"},
	{"tl", "Tagalog"}, {"tr", "Turkish"}, {"tt", "Tatar"}, {"uk", "Ukrainian"}, {"ur", "Urdu"},
	{"uz", "Uzbek"}, {"vi", "Vietnamese"}, {"yi", "Yiddish"}, {"yo", "Yoruba"}, {"yue", "Cantonese"},
	{"zh", "Chinese"},
}

func defaultVoiceProviderConfig(providerID string) VoiceProviderConfig {
	switch normalizeVoiceProviderID(providerID) {
	case "piper":
		return VoiceProviderConfig{
			VoiceID:     "en_US-lessac-medium",
			RuntimeMode: "per_task",
			BinaryPath:  "piper",
		}
	case "supertonic":
		return VoiceProviderConfig{
			VoiceID:     "M1",
			Language:    "auto",
			RuntimeMode: "per_task",
			BinaryPath:  "supertonic",
			Endpoint:    "http://127.0.0.1:7788",
		}
	case "whispercpp":
		return VoiceProviderConfig{
			ModelID:     "base",
			Language:    "auto",
			RuntimeMode: "per_task",
			BinaryPath:  "whisper-cli",
		}
	default:
		return VoiceProviderConfig{}
	}
}

func normalizeVoiceModuleID(id string) string {
	switch id = strings.TrimSpace(id); id {
	case VoiceModuleTTS, VoiceModuleSTT:
		return id
	default:
		return ""
	}
}

func normalizeVoiceProviderID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func voiceProviderExists(moduleID string, providerID string) bool {
	providerID = normalizeVoiceProviderID(providerID)
	if providerID == "" {
		return false
	}
	return slices.Contains(VoiceProviderIDs(moduleID), providerID)
}

// VoiceProviderIDs are the local providers of the TTS or STT module, the
// default first.
func VoiceProviderIDs(moduleID string) []string {
	switch normalizeVoiceModuleID(moduleID) {
	case VoiceModuleTTS:
		return []string{"piper", "supertonic"}
	case VoiceModuleSTT:
		return []string{"whispercpp"}
	default:
		return nil
	}
}

// normalizeRealtimeVoiceConfig trims the realtime voice settings; the realtime
// module owns their providers and defaults.
func normalizeRealtimeVoiceConfig(cfg VoiceModuleConfig) VoiceModuleConfig {
	cfg.ProviderID = normalizeVoiceProviderID(cfg.ProviderID)
	stored := map[string]VoiceProviderConfig{}
	for providerID, provider := range cfg.Providers {
		provider = VoiceProviderConfig{
			APIKey:    normalizeProviderAPIKey(provider.APIKey),
			APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
			ModelID:   strings.TrimSpace(provider.ModelID),
			VoiceID:   strings.TrimSpace(provider.VoiceID),
			Language:  strings.TrimSpace(provider.Language),
			Endpoint:  strings.TrimSpace(provider.Endpoint),
		}
		if providerID = normalizeVoiceProviderID(providerID); providerID != "" && provider != (VoiceProviderConfig{}) {
			stored[providerID] = provider
		}
	}
	cfg.Providers = nil
	if len(stored) > 0 {
		cfg.Providers = stored
	}
	return cfg
}
