package controlplane

import "github.com/Suren878/matrixclaw/internal/setup"

func voiceLocalTTSPicker(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) Result {
	if provider.ID == "supertonic" {
		return voiceLocalSharedRuntimeTTSPicker(module, provider)
	}
	installed := installedVoiceModels(provider)
	active, hasActive := activeInstalledVoice(provider)
	voiceInfo := "No voices installed"
	if len(installed) > 0 {
		voiceInfo = "Choose active voice"
	}
	if hasActive {
		voiceInfo = voiceModelName(provider, active.ID)
	}
	picker := NewPickerData(PickerVoiceProvider, provider.Name).
		Context(module.ID).
		Meta("Local TTS").
		Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
	runtimeAction := voiceRuntimeInstallAction(provider)
	picker.Item(PickerItem{
		ID:      "engine",
		Title:   "Engine",
		Info:    voiceRuntimeInstallInfo(provider),
		Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, runtimeAction),
		Role:    piperRuntimeActionRole(runtimeAction),
	})
	picker.Item(PickerItem{
		ID:      "voice",
		Title:   "Voice",
		Info:    voiceInfo,
		Command: voiceModuleCommand(module.ID, "provider-installed", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "run-mode",
		Title:   "Run Mode",
		Info:    voiceRunModeLabel(provider),
		Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID),
	})
	if provider.ID == "supertonic" {
		picker.Row("language", "Language", voiceLanguageStatus(provider.Config.Language), voiceModuleCommand(module.ID, "provider-language", provider.ID))
		picker.Row("threads", "Threads", voiceThreadsStatus(provider.Config.Threads), voiceModuleCommand(module.ID, "provider-threads", provider.ID))
	}
	return Result{Handled: true, Picker: picker.Ptr()}
}

func voiceLocalSharedRuntimeTTSPicker(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) Result {
	runtimeAction := voiceRuntimeInstallAction(provider)
	defaultVoice := "M1"
	languageInfo := voiceLanguageStatus(provider.Config.Language)
	voiceInfo := voiceModelName(provider, firstNonEmptyTrimmed(provider.Config.VoiceID, defaultVoice))
	picker := NewPickerData(PickerVoiceProvider, provider.Name).
		Context(module.ID).
		Meta("Local TTS").
		Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
	picker.Item(PickerItem{
		ID:      "engine",
		Title:   "Engine",
		Info:    voiceRuntimeInstallInfo(provider),
		Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, runtimeAction),
		Role:    piperRuntimeActionRole(runtimeAction),
	})
	picker.Item(PickerItem{
		ID:      "voice-style",
		Title:   "Select Voice",
		Info:    voiceInfo,
		Command: voiceModuleCommand(module.ID, "provider-model", provider.ID),
	})
	picker.Row("language", "Language", languageInfo, voiceModuleCommand(module.ID, "provider-language", provider.ID))
	picker.Item(PickerItem{
		ID:      "run-mode",
		Title:   "Run Mode",
		Info:    voiceRunModeLabel(provider),
		Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID),
	})
	picker.Row("threads", "Threads", voiceThreadsStatus(provider.Config.Threads), voiceModuleCommand(module.ID, "provider-threads", provider.ID))
	return Result{Handled: true, Picker: picker.Ptr()}
}

func voiceLocalSTTPicker(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) Result {
	installed := installedVoiceModels(provider)
	active, hasActive := activeInstalledModel(provider)
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
		Title:   "Add Model",
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
	picker.Item(PickerItem{
		ID:      "run-mode",
		Title:   "Run Mode",
		Info:    voiceRunModeLabel(provider),
		Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID),
	})
	picker.Row("language", "Language", voiceLanguageStatus(provider.Config.Language), voiceModuleCommand(module.ID, "provider-language", provider.ID))
	picker.Row("threads", "Threads", voiceThreadsStatus(provider.Config.Threads), voiceModuleCommand(module.ID, "provider-threads", provider.ID))
	return Result{Handled: true, Picker: picker.Ptr()}
}
