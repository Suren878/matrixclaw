package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voiceModuleInfo(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if module.ID == setup.VoiceModuleTTS || module.ID == setup.VoiceModuleSTT {
		return voiceModuleStatus(module), nil
	}
	return Result{
		Handled: true,
		Info: &InfoData{
			Title: module.Title,
			Rows: []InfoRow{
				{Label: "Enabled", Value: formatEnabled(module.Enabled)},
				{Label: "Provider", Value: module.ProviderName},
				{Label: "Local", Value: formatYesNo(module.Local)},
				{Label: "Status", Value: module.Status},
			},
		},
	}, nil
}

func voiceModuleStatus(module setup.VoiceModuleDescriptor) Result {
	rows := []InfoRow{
		{Label: "Active provider", Value: "Disabled"},
		{Label: "Mode", Value: "Disabled"},
		{Label: "Used RAM", Value: "0 B"},
	}
	if module.Enabled {
		if provider, ok := selectedVoiceProvider(module); ok {
			rows = []InfoRow{
				{Label: "Active provider", Value: firstNonEmptyTrimmed(provider.Name, module.ProviderName, provider.ID)},
				{Label: "Mode", Value: voiceRunModeLabel(provider)},
				{Label: "Used RAM", Value: voiceRuntimeRAMStatus(provider)},
			}
		} else {
			rows[0].Value = firstNonEmptyTrimmed(module.ProviderName, module.ProviderID, "Unknown")
			rows[1].Value = "Unknown"
		}
	}
	return Result{Handled: true, Info: &InfoData{Title: module.Title + " Status", Rows: rows}}
}

func (d *Dispatcher) voiceModule(ctx context.Context, moduleID string) (setup.VoiceModuleDescriptor, error) {
	modules, err := d.voiceModules.VoiceModules(ctx)
	if err != nil {
		return setup.VoiceModuleDescriptor{}, err
	}
	for _, module := range modules {
		if module.ID == moduleID {
			return module, nil
		}
	}
	return setup.VoiceModuleDescriptor{}, nil
}

func voiceModulePickerKind(moduleID string) PickerKind {
	switch moduleID {
	case setup.VoiceModuleTTS:
		return PickerTextToSpeech
	case setup.VoiceModuleSTT:
		return PickerSpeechToText
	default:
		return PickerModules
	}
}

func voiceProviderPickerTitle(provider setup.VoiceProviderOption) string {
	return provider.Name
}

func voiceProviderPickerInfo(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	if provider.ID != module.ProviderID || !module.Enabled {
		return ""
	}
	if provider.Local {
		if provider.ID == "supertonic" {
			if provider.RuntimeInstalled && (voiceRunModePerTaskSelected(provider) || voiceRuntimeState(provider) == "running") {
				return "Active"
			}
			return ""
		}
		if provider.ID == "piper" && (voiceRunModePerTaskSelected(provider) || voiceRuntimeState(provider) == "running") {
			if _, ok := activeInstalledVoice(provider); ok {
				return "Active"
			}
		}
		if provider.ID == "whispercpp" && (voiceRunModePerTaskSelected(provider) || voiceRuntimeState(provider) == "running") {
			if _, ok := activeInstalledModel(provider); ok {
				return "Active"
			}
		}
		return ""
	}
	return "Active"
}

func voiceProviderTitle(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	return module.Title + " · " + provider.Name
}

func voiceLocalProviderMeta(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	parts := nonEmptyStrings(
		"Module "+formatEnabled(module.Enabled),
		"Provider "+firstNonEmptyTrimmed(provider.Name, module.ProviderName),
		"Installation "+strings.ToLower(voiceDownloadState(provider)),
	)
	if voiceProviderDownloaded(provider) {
		parts = append(parts, "Runtime "+strings.ToLower(voiceRuntimeManagerInfo(provider)))
	}
	return strings.Join(parts, " · ")
}

func voiceLocalFileActionTitles(moduleID string) (string, string) {
	if moduleID == setup.VoiceModuleTTS {
		return "Install voice", "Remove voice"
	}
	return "Download model", "Delete model"
}
