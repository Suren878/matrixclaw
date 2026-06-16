package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voiceModuleProviderSetup(ctx context.Context, moduleID string, args string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	providerID := firstField(args)
	if providerID == "" {
		picker := NewPickerData(PickerVoiceProvider, "Setup Provider").
			Context(module.ID).
			Back(voiceModuleCommand(module.ID))
		for _, provider := range module.Providers {
			picker.Item(PickerItem{
				ID:      provider.ID,
				Title:   voiceProviderPickerTitle(provider),
				Info:    voiceProviderSetupInfo(module.ID, provider),
				Command: voiceModuleCommand(module.ID, "provider-setup", provider.ID),
			})
		}
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	for _, provider := range module.Providers {
		if provider.ID != providerID {
			continue
		}
		if provider.Local {
			result, err := d.voiceLocalProviderPickerWithProvider(module, provider)
			if err != nil {
				return Result{}, err
			}
			if result.Picker != nil {
				result.Picker.BackCommand = voiceModuleCommand(module.ID, "provider-setup")
				result.Picker.HasBack = true
			}
			return result, nil
		}
		setupProvider, err := d.voiceSetupProvider(ctx, providerID)
		if err != nil {
			return Result{}, err
		}
		return d.voiceProviderSetupFormResult(ctx, moduleID, providerID, setupProvider, formFromProvider(setupProvider), "")
	}
	return d.voiceModuleProviderSetup(ctx, moduleID, "")
}

func voiceProviderSelectionInfo(moduleID string, provider setup.VoiceProviderOption, _ bool) string {
	parts := []string{}
	parts = append(parts, voiceProviderMemoryInfo(moduleID, provider))
	return strings.Join(nonEmptyStrings(parts...), " · ")
}

func voiceProviderSetupInfo(moduleID string, provider setup.VoiceProviderOption) string {
	parts := []string{}
	if voiceProviderReady(moduleID, provider) {
		parts = append(parts, "Installed")
	}
	parts = append(parts, voiceProviderMemoryInfo(moduleID, provider))
	return strings.Join(nonEmptyStrings(parts...), " · ")
}

func voiceProviderReady(moduleID string, provider setup.VoiceProviderOption) bool {
	if !provider.Local {
		return true
	}
	if moduleID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		if !provider.RuntimeInstalled {
			return false
		}
		_, ok := activeInstalledModel(provider)
		return ok
	}
	switch provider.ID {
	case "piper":
		if !provider.RuntimeInstalled {
			return false
		}
		_, ok := activeInstalledVoice(provider)
		return ok
	case "supertonic":
		return provider.RuntimeInstalled
	default:
		return voiceProviderDownloaded(provider)
	}
}

func voiceProviderMemoryInfo(moduleID string, provider setup.VoiceProviderOption) string {
	if !provider.Local {
		return "Cloud"
	}
	if moduleID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		if model, ok := selectedOrDefaultVoiceModel(provider); ok {
			return firstNonEmptyTrimmed(model.RAM, persistentRuntimeRAMEstimate(provider))
		}
	}
	if estimate := persistentRuntimeRAMEstimate(provider); estimate != "" {
		return estimate
	}
	return "0 B RAM"
}

func selectedOrDefaultVoiceModel(provider setup.VoiceProviderOption) (setup.VoiceModelOption, bool) {
	if model, ok := voiceModelByID(provider.Models, provider.Config.ModelID); ok {
		return model, true
	}
	for _, model := range provider.Models {
		if model.Default {
			return model, true
		}
	}
	if len(provider.Models) > 0 {
		return provider.Models[0], true
	}
	return setup.VoiceModelOption{}, false
}

func (d *Dispatcher) voiceModuleProviderForm(ctx context.Context, moduleID string, providerID string) (Result, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return d.voiceModuleProviderPicker(ctx, moduleID)
	}
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if !module.Enabled {
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, setup.VoiceModuleUpdate{ProviderID: providerID}); err != nil {
		return Result{}, err
	}
	if setupProviderIDForVoiceProvider(providerID) == "" {
		return d.voiceLocalProviderPicker(ctx, moduleID, providerID)
	}
	provider, err := d.voiceSetupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, formFromProvider(provider), "")
}

func (d *Dispatcher) voiceLocalProviderPicker(ctx context.Context, moduleID string, providerID string) (Result, error) {
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	return d.voiceLocalProviderPickerWithProvider(module, provider)
}

func (d *Dispatcher) voiceLocalProviderPickerWithProvider(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) (Result, error) {
	if module.ID == setup.VoiceModuleTTS {
		return voiceLocalTTSPicker(module, provider), nil
	}
	if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		return voiceLocalSTTPicker(module, provider), nil
	}
	cfg := provider.Config
	title := voiceProviderTitle(module, provider)
	downloaded := voiceProviderDownloaded(provider)
	downloadTitle, deleteTitle := voiceLocalFileActionTitles(module.ID)
	picker := NewPickerData(PickerVoiceProvider, title).
		Context(module.ID).
		Meta(voiceLocalProviderMeta(module, provider)).
		Back(voiceModuleCommand(module.ID, "provider"))
	if module.ID == setup.VoiceModuleTTS {
		picker.Item(PickerItem{ID: "voice", Title: "Voice / language", Info: voiceLocalModelStatus(provider, cfg.VoiceID), Command: voiceModuleCommand(module.ID, "provider-model", provider.ID), Role: PickerItemRoleAction})
	} else {
		picker.Item(PickerItem{ID: "model", Title: "Model", Info: voiceLocalModelStatus(provider, cfg.ModelID), Command: voiceModuleCommand(module.ID, "provider-model", provider.ID), Role: PickerItemRoleAction})
		picker.Row("language", "Language", voiceLanguageStatus(cfg.Language), voiceModuleCommand(module.ID, "provider-language", provider.ID))
		picker.Row("threads", "Threads", voiceThreadsStatus(cfg.Threads), voiceModuleCommand(module.ID, "provider-threads", provider.ID))
	}
	picker.Item(PickerItem{ID: "files", Title: "Installation", Info: voiceDownloadState(provider), Command: voiceModuleCommand(module.ID, "provider-status", provider.ID), Role: PickerItemRoleAction})
	if downloaded {
		picker.Item(PickerItem{ID: "delete", Title: deleteTitle, Info: deleteActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, provider.ActionIDs.DeleteModel), Role: PickerItemRoleDanger})
		picker.Item(PickerItem{ID: "run-mode", Title: "Run Mode", Info: voiceRunModeLabel(provider), Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID)})
	} else {
		picker.Item(PickerItem{ID: "download", Title: downloadTitle, Info: downloadActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, provider.ActionIDs.DownloadModel)})
	}
	picker.Item(PickerItem{ID: "provider", Title: "Provider", Info: provider.Name, Command: voiceModuleCommand(module.ID, "provider"), Role: PickerItemRoleAction})
	picker.Row("module", module.Title, formatEnabled(module.Enabled), voiceModuleCommand(module.ID, "enabled"))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}
