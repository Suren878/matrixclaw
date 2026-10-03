package localruntime

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

// VoiceModule is the TTS or the STT module: its settings, its providers and,
// once decorated, their install and run state.
type VoiceModule struct {
	ID           string
	Title        string
	Enabled      bool
	ProviderID   string
	ProviderName string
	Local        bool
	Status       string
	Config       setup.VoiceProviderConfig
	Providers    []VoiceProvider
}

// VoiceProvider is a local voice engine with its effective settings.
type VoiceProvider struct {
	ID               string
	Name             string
	Local            bool
	Status           string
	Endpoint         string
	Downloaded       bool // the selected model or voice is installed
	ModelPath        string
	RuntimeState     string
	RuntimeDetail    string
	RuntimePath      string
	RuntimeInstalled bool
	RuntimeRSS       uint64
	Config           setup.VoiceProviderConfig
	Models           []VoiceModel
}

// VoiceModel is a model (STT) or a voice (TTS) a provider can download.
type VoiceModel struct {
	ID           string
	Name         string
	Size         string
	RAM          string
	Description  string
	Default      bool
	LanguageCode string
	LanguageName string
	Country      string
	Quality      string
	Installed    bool
	Path         string
}

// VoiceActionRequest is a provider action and the model it applies to.
type VoiceActionRequest struct {
	Action  string
	ModelID string
}

// VoiceModules are the TTS and STT modules of the config, undecorated.
func VoiceModules(modules setup.ModulesConfig) []VoiceModule {
	return []VoiceModule{
		voiceModule(setup.VoiceModuleTTS, "Text to Speech", modules.TextToSpeech),
		voiceModule(setup.VoiceModuleSTT, "Speech to Text", modules.SpeechToText),
	}
}

func voiceModule(id string, title string, cfg setup.VoiceModuleConfig) VoiceModule {
	providers := voiceProviders(id)
	selected := providers[0]
	for i := range providers {
		providers[i].Config = setup.EffectiveVoiceConfig(id, providers[i].ID, cfg.Providers[providers[i].ID])
		if providers[i].ID == cfg.ProviderID {
			selected = providers[i]
		}
	}
	status := "Disabled"
	if cfg.Enabled {
		status = "Local · not installed"
	}
	return VoiceModule{
		ID:           id,
		Title:        title,
		Enabled:      cfg.Enabled,
		ProviderID:   selected.ID,
		ProviderName: selected.Name,
		Local:        selected.Local,
		Status:       status,
		Config:       selected.Config,
		Providers:    providers,
	}
}

// voiceProviders are a module's providers with their fallback catalogs; the
// drivers replace them with the downloaded ones.
func voiceProviders(moduleID string) []VoiceProvider {
	if moduleID == setup.VoiceModuleSTT {
		return []VoiceProvider{
			{ID: "whispercpp", Name: "Whisper.cpp", Local: true, Status: "Local · not downloaded", Models: []VoiceModel{
				{ID: "tiny", Name: "Tiny", Size: "~39 MB", RAM: "~390 MB", Description: "Fastest, lowest accuracy"},
				{ID: "base", Name: "Base", Size: "~142 MB", RAM: "~500 MB", Description: "Balanced default", Default: true},
				{ID: "small", Name: "Small", Size: "~466 MB", RAM: "~1 GB", Description: "Better accuracy"},
				{ID: "medium", Name: "Medium", Size: "~1.5 GB", RAM: "~2.6 GB", Description: "Heavy local model"},
				{ID: "large-v3", Name: "Large v3", Size: "~3 GB", RAM: "~4 GB", Description: "Very heavy"},
			}},
		}
	}
	return []VoiceProvider{
		{ID: "piper", Name: "Piper", Local: true, Status: "Local · not installed", Models: []VoiceModel{
			{ID: "en_US-lessac-medium", Name: "Lessac Medium", Size: "~60 MB", Description: "Fallback English voice", Default: true, LanguageCode: "en_US", LanguageName: "English", Quality: "medium"},
			{ID: "ru_RU-ruslan-medium", Name: "Ruslan Medium", Size: "~60 MB", Description: "Fallback Russian voice", LanguageCode: "ru_RU", LanguageName: "Russian", Quality: "medium"},
		}},
		{ID: "supertonic", Name: "Supertonic 3", Local: true, Status: "Local · not installed"},
	}
}

// VoiceLanguageOfVoice is the language code a Piper voice id starts with.
func VoiceLanguageOfVoice(voiceID string) string {
	if before, _, ok := strings.Cut(strings.TrimSpace(voiceID), "-"); ok && before != "" {
		return setup.NormalizeVoiceLanguage(before)
	}
	return "en_US"
}
