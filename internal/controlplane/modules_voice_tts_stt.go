package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func voiceLocalTTSPicker(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) Result {
	installed := installedVoiceModels(provider)
	active, hasActive := activeInstalledVoice(provider)
	voiceInfo := "No voices installed"
	if len(installed) > 0 {
		voiceInfo = "Choose active voice"
	}
	if hasActive {
		voiceInfo = voiceModelName(provider, active.ID)
	}
	runtimeAction := ttsRuntimeAction(provider)
	picker := NewPickerData(PickerVoiceProvider, provider.Name).
		Context(module.ID).
		Meta(strings.Join(nonEmptyStrings("Local text to speech", voiceCatalogInfo(provider), installedVoicesSummary(installed)), " · ")).
		Back(voiceModuleCommand(module.ID, "provider"))
	picker.Item(PickerItem{
		ID:      "add-voice",
		Title:   "Add voice",
		Command: voiceModuleCommand(module.ID, "provider-language", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "voice",
		Title:   "Voice",
		Info:    voiceInfo,
		Command: voiceModuleCommand(module.ID, "provider-installed", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "status",
		Title:   "Status",
		Command: voiceModuleCommand(module.ID, "provider-status", provider.ID),
	})
	if provider.ID == "piper" {
		runtimeAction := piperRuntimeInstallAction(provider)
		picker.Item(PickerItem{
			ID:       "piper-runtime",
			Title:    "Piper runtime",
			Info:     voiceRuntimeInstallInfo(provider),
			Command:  voiceModuleCommand(module.ID, "provider-action", provider.ID, runtimeAction),
			Disabled: runtimeAction == "delete-runtime" && voiceRuntimeState(provider) == "running",
			Role:     piperRuntimeActionRole(runtimeAction),
		})
	}
	if voiceRunModeAlways(provider) {
		picker.Item(PickerItem{
			ID:       "runtime",
			Title:    ttsRuntimeActionTitle(provider),
			Command:  voiceModuleCommand(module.ID, "provider-action", provider.ID, runtimeAction),
			Disabled: !hasActive,
		})
	}
	picker.Item(PickerItem{
		ID:      "run-mode",
		Title:   "Run mode",
		Info:    voiceRunModeLabel(provider),
		Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID),
	})
	return Result{Handled: true, Picker: picker.Ptr()}
}

func voiceLocalSTTPicker(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) Result {
	installed := installedVoiceModels(provider)
	active, hasActive := activeInstalledModel(provider)
	runtimeAction := ttsRuntimeAction(provider)
	modelInfo := "No models installed"
	if len(installed) > 0 {
		modelInfo = "Choose active model"
	}
	if hasActive {
		modelInfo = voiceModelName(provider, active.ID)
	}
	picker := NewPickerData(PickerVoiceProvider, provider.Name).
		Context(module.ID).
		Back(voiceModuleCommand(module.ID, "provider"))
	picker.Item(PickerItem{
		ID:      "add-model",
		Title:   "Add model",
		Command: voiceModuleCommand(module.ID, "provider-model", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "model",
		Title:   "Model",
		Info:    modelInfo,
		Command: voiceModuleCommand(module.ID, "provider-installed", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "status",
		Title:   "Status",
		Command: voiceModuleCommand(module.ID, "provider-status", provider.ID),
	})
	if voiceRunModeAlways(provider) {
		picker.Item(PickerItem{
			ID:       "runtime",
			Title:    ttsRuntimeActionTitle(provider),
			Command:  voiceModuleCommand(module.ID, "provider-action", provider.ID, runtimeAction),
			Disabled: !hasActive || !voiceRuntimeActionsAvailable(provider),
		})
	}
	picker.Item(PickerItem{
		ID:      "run-mode",
		Title:   "Run mode",
		Info:    voiceRunModeLabel(provider),
		Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID),
	})
	picker.Row("language", "Language", voiceLanguageStatus(provider.Config.Language), voiceModuleCommand(module.ID, "provider-language", provider.ID))
	picker.Row("threads", "Threads", voiceThreadsStatus(provider.Config.Threads), voiceModuleCommand(module.ID, "provider-threads", provider.ID))
	return Result{Handled: true, Picker: picker.Ptr()}
}
