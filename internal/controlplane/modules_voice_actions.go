package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voicePostRuntimeInstallAction(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) (Result, error) {
	updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "install-runtime"})
	if err != nil {
		return Result{}, err
	}
	provider = updated
	if !provider.RuntimeInstalled {
		if refreshed, ok, err := d.waitForVoiceProviderRuntimeInstalled(ctx, module.ID, provider.ID); err != nil {
			return Result{}, err
		} else if ok {
			provider = refreshed
		}
	}
	if module.ID == setup.VoiceModuleTTS && provider.ID == "supertonic" {
		if err := d.ensureSupertonicDefaultVoice(ctx, module, provider); err != nil {
			return Result{}, err
		}
		if refreshed, ok, err := d.waitForVoiceProviderRuntimeInstalled(ctx, module.ID, provider.ID); err != nil {
			return Result{}, err
		} else if ok {
			provider = refreshed
		}
	}
	if module.ID == setup.VoiceModuleTTS && provider.ID == "piper" {
		if _, ok := activeInstalledVoice(provider); !ok {
			return voiceProviderNeedsVoiceResult(module, provider, voiceProviderSettingsBackCommand(module.ID, provider.ID)), nil
		}
	}
	if module.ID == setup.VoiceModuleTTS && provider.ID == "supertonic" && provider.RuntimeInstalled {
		modules, err := d.activateVoiceModuleProviderState(ctx, module, provider, false)
		if err != nil {
			return Result{}, err
		}
		if activated, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
			return d.voiceLocalProviderPickerWithProvider(module, activated)
		}
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
	}
	if voicePersistentProvider(module.ID, provider.ID) && normalizeVoiceRunMode(provider.Config.RuntimeMode) == voiceRuntimeModeAlways && voiceLocalRuntimeStartReady(module.ID, provider, provider.Config) {
		updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"})
		if err != nil {
			return Result{}, err
		}
		provider = updated
	}
	return d.voiceLocalProviderPickerWithProvider(module, provider)
}

func (d *Dispatcher) voicePostDownloadAction(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) (Result, error) {
	if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" && !provider.RuntimeInstalled {
		updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "install-runtime"})
		if err != nil {
			return Result{}, err
		}
		provider = updated
		if !provider.RuntimeInstalled {
			if refreshed, ok, err := d.waitForVoiceProviderRuntimeInstalled(ctx, module.ID, provider.ID); err != nil {
				return Result{}, err
			} else if ok {
				provider = refreshed
			}
		}
	}
	updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "download", ModelID: modelID})
	if err != nil {
		return Result{}, err
	}
	provider = updated
	if module.ID == setup.VoiceModuleTTS && strings.TrimSpace(modelID) != "" {
		cfg := provider.Config
		cfg.VoiceID = strings.TrimSpace(modelID)
		if provider.ID == "piper" {
			cfg.Language = voiceLanguageFromVoiceID(modelID)
		}
		modules, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
		if err != nil {
			return Result{}, err
		}
		if refreshed, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
			provider = refreshed
		}
		if provider.ID == "piper" && provider.RuntimeInstalled {
			modules, err := d.activateVoiceModuleProviderState(ctx, module, provider, false)
			if err != nil {
				return Result{}, err
			}
			if activated, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
				return d.voiceLocalProviderPickerWithProvider(module, activated)
			}
			return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
		}
	}
	if module.ID == setup.VoiceModuleTTS {
		return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
	}
	if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		cfg := provider.Config
		if strings.TrimSpace(modelID) != "" {
			cfg.ModelID = strings.TrimSpace(modelID)
			if strings.TrimSpace(cfg.RuntimeMode) == "" {
				cfg.RuntimeMode = voiceRuntimeModePerTask
			}
			enabled := true
			modules, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{Enabled: &enabled, ProviderID: provider.ID, ProviderConfig: &cfg})
			if err != nil {
				return Result{}, err
			}
			if refreshed, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
				provider = refreshed
			}
		}
		if err := d.stopOtherVoiceModuleProviders(ctx, module, provider.ID); err != nil {
			return Result{}, err
		}
		if normalizeVoiceRunMode(cfg.RuntimeMode) == voiceRuntimeModeAlways && voiceLocalRuntimeStartReady(module.ID, provider, cfg) {
			if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
				return Result{}, err
			}
		}
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
	}
	return Result{Handled: true, Confirm: &ConfirmData{
		Title:          "Installed",
		Message:        voiceDownloadedMessage(module, provider, modelID),
		ConfirmLabel:   confirmLabelAfterDownload(module.ID),
		CancelLabel:    "Close",
		ConfirmCommand: voiceCommandAfterDownload(module.ID, provider.ID),
		CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
	}}, nil
}

func (d *Dispatcher) voicePostRuntimeAction(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, action string) (Result, error) {
	action = strings.TrimSuffix(action, "-confirm")
	updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: action})
	if err != nil {
		return Result{}, err
	}
	return d.voiceLocalProviderPickerWithProvider(module, updated)
}

func (d *Dispatcher) voicePostDeleteAction(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) (Result, error) {
	updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "delete", ModelID: modelID})
	if err != nil {
		return Result{}, err
	}
	if module.ID == setup.VoiceModuleTTS {
		if disabled, err := d.reselectTTSVoiceAfterDelete(ctx, module, updated, modelID); err != nil {
			return Result{}, err
		} else if disabled {
			return d.voiceModulePicker(ctx, module.ID)
		}
		return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
	}
	if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		if disabled, err := d.reselectSTTModelAfterDelete(ctx, module, updated, modelID); err != nil {
			return Result{}, err
		} else if disabled {
			return d.voiceModulePicker(ctx, module.ID)
		}
		return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
	}
	return Result{Handled: true, Confirm: &ConfirmData{
		Title:          "Deleted",
		Message:        voiceDeletedMessage(module, provider, modelID),
		ConfirmLabel:   confirmLabelAfterDelete(module.ID),
		CancelLabel:    "Close",
		ConfirmCommand: voiceCommandAfterDelete(module.ID, provider.ID),
		CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
	}}, nil
}
