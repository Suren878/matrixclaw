package setup

import (
	"cmp"
	"fmt"
	"strings"
)

const (
	VoiceModuleTTS = "tts"
	VoiceModuleSTT = "stt"
)

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
	cfg, err := s.Update(func(cfg *Config) error {
		current := voiceModuleConfigByID(cfg.Modules, id)
		if update.Enabled != nil {
			current.Enabled = *update.Enabled
		}
		if providerID := normalizeVoiceProviderID(update.ProviderID); providerID != "" {
			if !voiceProviderExists(id, providerID) {
				return fmt.Errorf("voice provider %q is not available for %s", providerID, id)
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
				return fmt.Errorf("voice provider %q is not available for %s", providerID, id)
			}
			if current.Providers == nil {
				current.Providers = map[string]VoiceProviderConfig{}
			}
			current.Providers[providerID] = *update.ProviderConfig
		}
		current = normalizeVoiceModuleConfig(id, current)
		setVoiceModuleConfigByID(&cfg.Modules, id, current)
		return nil
	})
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

// voiceProviderConfigByID is a provider's effective settings in module.
func voiceProviderConfigByID(moduleID string, module VoiceModuleConfig, providerID string) VoiceProviderConfig {
	providerID = normalizeVoiceProviderID(providerID)
	return effectiveVoiceProviderConfig(moduleID, providerID, module.Providers[providerID])
}

func storedVoiceProviderConfig(moduleID string, providerID string, cfg VoiceProviderConfig) VoiceProviderConfig {
	cfg = effectiveVoiceProviderConfig(moduleID, providerID, cfg)
	defaults := defaultVoiceProviderConfig(providerID)
	cfg.ModelID = omitDefault(cfg.ModelID, defaults.ModelID)
	cfg.VoiceID = omitDefault(cfg.VoiceID, defaults.VoiceID)
	cfg.Language = omitDefault(cfg.Language, defaults.Language)
	cfg.BinaryPath = omitDefault(cfg.BinaryPath, defaults.BinaryPath)
	cfg.Endpoint = omitDefault(cfg.Endpoint, defaults.Endpoint)
	cfg.RuntimeMode = omitDefault(cfg.RuntimeMode, "per_task")
	return cfg
}

// effectiveVoiceProviderConfig normalizes cfg and fills the provider's
// defaults for what it leaves empty.
func effectiveVoiceProviderConfig(moduleID string, providerID string, cfg VoiceProviderConfig) VoiceProviderConfig {
	moduleID = normalizeVoiceModuleID(moduleID)
	providerID = normalizeVoiceProviderID(providerID)
	cfg.APIKey = ""
	cfg.APIKeyEnv = ""
	cfg.ModelID = strings.TrimSpace(cfg.ModelID)
	cfg.VoiceID = strings.TrimSpace(cfg.VoiceID)
	if moduleID == VoiceModuleTTS {
		if providerID == "supertonic" {
			cfg.Language = normalizeSupertonicLanguageCode(cfg.Language)
		} else {
			cfg.Language = normalizeVoiceLanguageCode(cfg.Language)
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

func voiceModuleConfigByID(modules ModulesConfig, id string) VoiceModuleConfig {
	switch normalizeVoiceModuleID(id) {
	case VoiceModuleTTS:
		return modules.TextToSpeech
	case VoiceModuleSTT:
		return modules.SpeechToText
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

func defaultVoiceProviderID(moduleID string) string {
	switch normalizeVoiceModuleID(moduleID) {
	case VoiceModuleTTS:
		return "piper"
	case VoiceModuleSTT:
		return "whispercpp"
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
