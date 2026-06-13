package setup

import (
	"fmt"
	"strings"
)

const (
	VoiceModuleTTS      = "tts"
	VoiceModuleSTT      = "stt"
	VoiceModuleRealtime = "realtime_voice"
)

const FutureVoiceModuleRealtime = VoiceModuleRealtime

func (s *Service) VoiceModules() ([]VoiceModuleDescriptor, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	return VoiceModuleDescriptors(cfg.Modules), nil
}

func (s *Service) UpdateVoiceModule(id string, update VoiceModuleUpdate) ([]VoiceModuleDescriptor, error) {
	id = normalizeVoiceModuleID(id)
	if id == "" {
		return nil, fmt.Errorf("voice module id is required")
	}
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	current := voiceModuleConfigByID(cfg.Modules, id)
	if update.Enabled != nil {
		current.Enabled = *update.Enabled
	}
	if providerID := normalizeVoiceProviderID(update.ProviderID); providerID != "" {
		if !voiceProviderExists(id, providerID) {
			return nil, fmt.Errorf("voice provider %q is not available for %s", providerID, id)
		}
		current.ProviderID = providerID
	}
	if update.ProviderConfig != nil {
		providerID := current.ProviderID
		if update.ProviderID != "" {
			providerID = normalizeVoiceProviderID(update.ProviderID)
		}
		if providerID == "" {
			providerID = defaultVoiceProviderID(id)
		}
		if !voiceProviderExists(id, providerID) {
			return nil, fmt.Errorf("voice provider %q is not available for %s", providerID, id)
		}
		if current.Providers == nil {
			current.Providers = map[string]VoiceProviderConfig{}
		}
		providerConfig := *update.ProviderConfig
		if id == VoiceModuleRealtime && (providerID == "gemini_live" || providerID == "grok_voice") {
			existing := voiceProviderConfigByID(id, current, providerID)
			if strings.TrimSpace(providerConfig.APIKey) == "" {
				providerConfig.APIKey = existing.APIKey
			}
			if strings.TrimSpace(providerConfig.APIKeyEnv) == "" {
				providerConfig.APIKeyEnv = existing.APIKeyEnv
			}
		}
		current.Providers[providerID] = normalizeVoiceProviderConfig(id, providerID, providerConfig)
	}
	current = normalizeVoiceModuleConfig(id, current)
	setVoiceModuleConfigByID(&cfg.Modules, id, current)
	if err := s.store.Save(cfg); err != nil {
		return nil, err
	}
	cfg, err = s.Load()
	if err != nil {
		return nil, err
	}
	return VoiceModuleDescriptors(cfg.Modules), nil
}

func VoiceModuleDescriptors(modules ModulesConfig) []VoiceModuleDescriptor {
	modules = normalizeModulesConfig(modules)
	return []VoiceModuleDescriptor{
		voiceModuleDescriptor(VoiceModuleTTS, "Text to Speech", modules.TextToSpeech),
		voiceModuleDescriptor(VoiceModuleSTT, "Speech to Text", modules.SpeechToText),
	}
}

func RealtimeVoiceModuleDescriptor(modules ModulesConfig) VoiceModuleDescriptor {
	modules = normalizeModulesConfig(modules)
	return voiceModuleDescriptor(VoiceModuleRealtime, "Realtime Voice", modules.RealtimeVoice)
}

func voiceModuleDescriptor(id string, title string, cfg VoiceModuleConfig) VoiceModuleDescriptor {
	cfg = normalizeVoiceModuleConfig(id, cfg)
	provider := voiceProviderByID(id, cfg.ProviderID)
	providerConfig := voiceProviderConfigByID(id, cfg, provider.ID)
	status := "Disabled"
	if cfg.Enabled {
		status = voiceProviderRuntimeStatus(provider)
	}
	providers := voiceProviders(id)
	for i := range providers {
		providers[i].Config = voiceProviderConfigByID(id, cfg, providers[i].ID)
	}
	return VoiceModuleDescriptor{
		ID:           id,
		Title:        title,
		Enabled:      cfg.Enabled,
		ProviderID:   provider.ID,
		ProviderName: provider.Name,
		Local:        provider.Local,
		Status:       status,
		Config:       providerConfig,
		Providers:    providers,
	}
}

func normalizeVoiceModuleConfig(moduleID string, cfg VoiceModuleConfig) VoiceModuleConfig {
	moduleID = normalizeVoiceModuleID(moduleID)
	cfg.ProviderID = normalizeVoiceProviderID(cfg.ProviderID)
	if !voiceProviderExists(moduleID, cfg.ProviderID) {
		cfg.ProviderID = defaultVoiceProviderID(moduleID)
	}
	if len(cfg.Providers) == 0 {
		cfg.Providers = nil
	}
	normalized := map[string]VoiceProviderConfig{}
	for providerID, providerCfg := range cfg.Providers {
		providerID = normalizeVoiceProviderID(providerID)
		if providerID == "" || !voiceProviderExists(moduleID, providerID) {
			continue
		}
		normalized[providerID] = normalizeVoiceProviderConfig(moduleID, providerID, providerCfg)
	}
	for _, provider := range voiceProviders(moduleID) {
		if provider.Local {
			if _, ok := normalized[provider.ID]; !ok {
				normalized[provider.ID] = defaultVoiceProviderConfig(provider.ID)
			}
		}
	}
	if len(normalized) == 0 {
		cfg.Providers = nil
	} else {
		cfg.Providers = normalized
	}
	return cfg
}

func voiceProviderConfigByID(moduleID string, module VoiceModuleConfig, providerID string) VoiceProviderConfig {
	providerID = normalizeVoiceProviderID(providerID)
	if module.Providers != nil {
		if cfg, ok := module.Providers[providerID]; ok {
			return normalizeVoiceProviderConfig(moduleID, providerID, cfg)
		}
	}
	return defaultVoiceProviderConfig(providerID)
}

func normalizeVoiceProviderConfig(moduleID string, providerID string, cfg VoiceProviderConfig) VoiceProviderConfig {
	moduleID = normalizeVoiceModuleID(moduleID)
	providerID = normalizeVoiceProviderID(providerID)
	if moduleID == VoiceModuleRealtime && (providerID == "gemini_live" || providerID == "grok_voice") {
		cfg.APIKey = normalizeProviderAPIKey(cfg.APIKey)
		cfg.APIKeyEnv = strings.TrimSpace(cfg.APIKeyEnv)
	} else {
		cfg.APIKey = ""
		cfg.APIKeyEnv = ""
	}
	cfg.ModelID = strings.TrimSpace(cfg.ModelID)
	cfg.VoiceID = strings.TrimSpace(cfg.VoiceID)
	if moduleID == VoiceModuleTTS {
		if providerID == "supertonic" {
			cfg.Language = normalizeSupertonicLanguageCode(cfg.Language)
		} else {
			cfg.Language = normalizeVoiceLanguageCode(cfg.Language)
		}
	} else if moduleID == VoiceModuleRealtime && providerID == "gemini_live" {
		cfg.Language = normalizeRealtimeVoiceLanguageCode(cfg.Language)
	} else if moduleID == VoiceModuleRealtime && providerID == "grok_voice" {
		cfg.Language = normalizeGrokVoiceLanguageCode(cfg.Language)
	} else {
		cfg.Language = strings.ToLower(strings.TrimSpace(cfg.Language))
	}
	cfg.BinaryPath = strings.TrimSpace(cfg.BinaryPath)
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.RuntimeMode = normalizeVoiceRuntimeMode(cfg.RuntimeMode)
	defaults := defaultVoiceProviderConfig(providerID)
	if cfg.ModelID == "" {
		cfg.ModelID = defaults.ModelID
	}
	if cfg.VoiceID == "" {
		cfg.VoiceID = defaults.VoiceID
	}
	if cfg.Language == "" {
		cfg.Language = defaults.Language
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = defaults.BinaryPath
	}
	if cfg.Threads < 0 {
		cfg.Threads = 0
	}
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

func normalizeVoiceLanguageCode(language string) string {
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

func normalizeSupertonicLanguageCode(language string) string {
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
	switch language {
	case "en", "ko", "ja", "ar", "bg", "cs", "da", "de", "el", "es", "et", "fi", "fr", "hi", "hr", "hu", "id", "it", "lt", "lv", "nl", "pl", "pt", "ro", "ru", "sk", "sl", "sv", "tr", "uk", "vi", "na":
		return language
	default:
		return "auto"
	}
}

func normalizeRealtimeVoiceLanguageCode(language string) string {
	language = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch language {
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

func normalizeGrokVoiceLanguageCode(language string) string {
	language = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	switch language {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	case "en", "en-us", "en-gb", "bn", "bn-bd", "zh", "zh-cn", "fr", "fr-fr", "de", "de-de", "hi", "hi-in", "id", "id-id", "it", "it-it", "ja", "ja-jp", "ko", "ko-kr", "ru", "ru-ru", "tr", "tr-tr", "vi", "vi-vn":
		if before, _, ok := strings.Cut(language, "-"); ok {
			return before
		}
		return language
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
	case "gemini_live":
		return VoiceProviderConfig{
			VoiceID:  "Puck",
			Language: "auto",
			Endpoint: "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent",
		}
	case "grok_voice":
		return VoiceProviderConfig{
			ModelID:   "grok-voice-latest",
			VoiceID:   "eve",
			Language:  "auto",
			APIKeyEnv: "XAI_API_KEY",
			Endpoint:  "wss://api.x.ai/v1/realtime",
		}
	default:
		return VoiceProviderConfig{}
	}
}

func voiceModuleConfigByID(modules ModulesConfig, id string) VoiceModuleConfig {
	switch normalizeVoiceModuleID(id) {
	case VoiceModuleTTS:
		return modules.TextToSpeech
	case VoiceModuleSTT:
		return modules.SpeechToText
	case VoiceModuleRealtime:
		return modules.RealtimeVoice
	default:
		return VoiceModuleConfig{}
	}
}

func setVoiceModuleConfigByID(modules *ModulesConfig, id string, cfg VoiceModuleConfig) {
	switch normalizeVoiceModuleID(id) {
	case VoiceModuleTTS:
		modules.TextToSpeech = cfg
	case VoiceModuleSTT:
		modules.SpeechToText = cfg
	case VoiceModuleRealtime:
		modules.RealtimeVoice = cfg
	}
}

func normalizeVoiceModuleID(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "tts", "text-to-speech", "text_to_speech":
		return VoiceModuleTTS
	case "stt", "speech-to-text", "speech_to_text":
		return VoiceModuleSTT
	case "realtime", "realtime-voice", "realtime_voice", "live", "live-voice", "live_voice":
		return VoiceModuleRealtime
	default:
		return ""
	}
}

func normalizeVoiceProviderID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func defaultVoiceProviderID(moduleID string) string {
	switch normalizeVoiceModuleID(moduleID) {
	case VoiceModuleTTS:
		return "piper"
	case VoiceModuleSTT:
		return "whispercpp"
	case VoiceModuleRealtime:
		return "gemini_live"
	default:
		return ""
	}
}

func voiceProviderExists(moduleID string, providerID string) bool {
	providerID = normalizeVoiceProviderID(providerID)
	if providerID == "" {
		return false
	}
	for _, provider := range voiceProviders(moduleID) {
		if provider.ID == providerID {
			return true
		}
	}
	return false
}

func voiceProviderByID(moduleID string, providerID string) VoiceProviderOption {
	providerID = normalizeVoiceProviderID(providerID)
	for _, provider := range voiceProviders(moduleID) {
		if provider.ID == providerID {
			return provider
		}
	}
	for _, provider := range voiceProviders(moduleID) {
		if provider.ID == defaultVoiceProviderID(moduleID) {
			return provider
		}
	}
	return VoiceProviderOption{}
}

func voiceProviders(moduleID string) []VoiceProviderOption {
	switch normalizeVoiceModuleID(moduleID) {
	case VoiceModuleTTS:
		return []VoiceProviderOption{
			{ID: "piper", Name: "Piper", Local: true, Status: "Local · not installed", Models: []VoiceModelOption{
				{ID: "en_US-lessac-medium", Name: "Lessac Medium", Size: "~60 MB", Description: "Fallback English voice", Default: true, LanguageCode: "en_US", LanguageName: "English", Quality: "medium"},
				{ID: "ru_RU-ruslan-medium", Name: "Ruslan Medium", Size: "~60 MB", Description: "Fallback Russian voice", LanguageCode: "ru_RU", LanguageName: "Russian", Quality: "medium"},
			}},
			{ID: "supertonic", Name: "Supertonic 3", Local: true, Status: "Local · not installed"},
		}
	case VoiceModuleSTT:
		return []VoiceProviderOption{
			{ID: "whispercpp", Name: "Whisper.cpp", Local: true, Status: "Local · not downloaded", Models: []VoiceModelOption{
				{ID: "tiny", Name: "Tiny", Size: "~39 MB", RAM: "~390 MB", Description: "Fastest, lowest accuracy"},
				{ID: "base", Name: "Base", Size: "~142 MB", RAM: "~500 MB", Description: "Balanced default", Default: true},
				{ID: "small", Name: "Small", Size: "~466 MB", RAM: "~1 GB", Description: "Better accuracy"},
				{ID: "medium", Name: "Medium", Size: "~1.5 GB", RAM: "~2.6 GB", Description: "Heavy local model"},
				{ID: "large-v3", Name: "Large v3", Size: "~3 GB", RAM: "~4 GB", Description: "Very heavy"},
			}},
		}
	case VoiceModuleRealtime:
		return []VoiceProviderOption{
			{ID: "gemini_live", Name: "Gemini Live", Local: false, Status: "Cloud · API key required"},
			{ID: "grok_voice", Name: "Grok Voice", Local: false, Status: "Cloud · API key required"},
		}
	default:
		return nil
	}
}

func voiceProviderRuntimeStatus(provider VoiceProviderOption) string {
	if !provider.Local {
		return provider.Status
	}
	return "Local · not installed"
}
