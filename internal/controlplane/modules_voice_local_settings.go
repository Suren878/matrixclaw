package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voiceLocalProviderThreadsPicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	options := []struct {
		id      string
		title   string
		threads int
	}{{"auto", "Auto", 0}, {"2", "2 threads", 2}, {"4", "4 threads", 4}, {"8", "8 threads", 8}}
	picker := NewPickerData(PickerVoiceProvider, "Threads").
		Context(module.ID).
		Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
	for _, option := range options {
		picker.Item(PickerItem{ID: option.id, Title: option.title, Selected: option.threads == provider.Config.Threads, Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "threads", option.id)})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceLocalProviderRunModePicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerVoiceProvider, "Run Mode").
			Context(module.ID).
			Meta(voiceRunModeLabel(provider)).
			Select(voiceProviderSettingsBackCommand(module.ID, provider.ID)).
			Item(PickerItem{ID: "per-task", Title: voiceRunPerTaskTitle(provider), Selected: voiceRunModePerTaskSelected(provider), Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "runtime-mode", voiceRuntimeModePerTask)}).
			Item(PickerItem{ID: "always-running", Title: "Always Running", Selected: voiceRunModeAlways(provider), Disabled: !voicePersistentRuntimeAvailable(provider), Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "runtime-mode", voiceRuntimeModeAlways)}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) setVoiceLocalProviderConfig(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	field, value := firstCommandStep(rest)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	cfg := provider.Config
	nextRuntimeMode := cfg.RuntimeMode
	switch field {
	case "voice":
		cfg.VoiceID = strings.TrimSpace(value)
		if module.ID == setup.VoiceModuleTTS && provider.ID == "piper" {
			cfg.Language = voiceLanguageFromVoiceID(cfg.VoiceID)
		}
	case "model":
		cfg.ModelID = strings.TrimSpace(value)
	case "language":
		if module.ID == setup.VoiceModuleTTS {
			if provider.ID == "supertonic" {
				cfg.Language = normalizeSupertonicLanguageCode(value)
			} else {
				cfg.Language = normalizeVoiceLanguageCode(value)
			}
		} else {
			cfg.Language = strings.TrimSpace(value)
		}
	case "threads":
		switch strings.TrimSpace(value) {
		case "2":
			cfg.Threads = 2
		case "4":
			cfg.Threads = 4
		case "8":
			cfg.Threads = 8
		default:
			cfg.Threads = 0
		}
	case "autostart":
		cfg.Autostart = isYes(value)
	case "runtime-mode":
		nextRuntimeMode = normalizeVoiceRunMode(value)
		if nextRuntimeMode == voiceRuntimeModeAlways && !voicePersistentRuntimeAvailable(provider) {
			nextRuntimeMode = voiceRuntimeModePerTask
		}
		if nextRuntimeMode == voiceRuntimeModePerTask && voiceRunModeAlways(provider) {
			if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "stop"}); err != nil {
				return Result{}, err
			}
		}
		cfg.RuntimeMode = nextRuntimeMode
		cfg.Autostart = cfg.RuntimeMode == voiceRuntimeModeAlways
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg}); err != nil {
		return Result{}, err
	}
	if voicePersistentProvider(module.ID, provider.ID) && nextRuntimeMode == voiceRuntimeModeAlways && (field == "runtime-mode" || field == "voice" || field == "model") && voiceLocalRuntimeStartReady(module.ID, provider, cfg) {
		if err := d.stopOtherVoiceModuleProviders(ctx, module, provider.ID); err != nil {
			return Result{}, err
		}
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
			return Result{}, err
		}
	}
	if module.ID == setup.VoiceModuleTTS && field == "language" && provider.ID != "supertonic" {
		return d.voiceLocalProviderModelPicker(ctx, module.ID, provider.ID)
	}
	return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
}

func (d *Dispatcher) disableVoiceModuleIfActiveProvider(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) (bool, error) {
	if !module.Enabled || !strings.EqualFold(strings.TrimSpace(module.ProviderID), strings.TrimSpace(provider.ID)) {
		return false, nil
	}
	enabled := false
	_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{Enabled: &enabled})
	return err == nil, err
}

func voiceLocalRuntimeStartReady(moduleID string, provider setup.VoiceProviderOption, cfg setup.VoiceProviderConfig) bool {
	if !provider.RuntimeInstalled {
		return false
	}
	provider.Config = cfg
	switch moduleID {
	case setup.VoiceModuleTTS:
		switch provider.ID {
		case "piper":
			_, ok := activeInstalledVoice(provider)
			return ok
		case "supertonic":
			return true
		}
	case setup.VoiceModuleSTT:
		if provider.ID == "whispercpp" {
			_, ok := activeInstalledModel(provider)
			return ok
		}
	}
	return voiceProviderDownloaded(provider)
}

func (d *Dispatcher) reselectTTSVoiceAfterDelete(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, deletedVoiceID string) (bool, error) {
	deletedVoiceID = strings.TrimSpace(deletedVoiceID)
	if deletedVoiceID == "" || !strings.EqualFold(provider.Config.VoiceID, deletedVoiceID) {
		return false, nil
	}
	cfg := provider.Config
	for _, model := range installedVoiceModels(provider) {
		if strings.EqualFold(model.ID, deletedVoiceID) {
			continue
		}
		cfg.VoiceID = model.ID
		cfg.Language = voiceLanguageFromVoiceID(model.ID)
		_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
		return false, err
	}
	return d.disableVoiceModuleIfActiveProvider(ctx, module, provider)
}

func (d *Dispatcher) ensureSupertonicDefaultVoice(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) error {
	cfg := provider.Config
	if strings.TrimSpace(cfg.VoiceID) == "" {
		cfg.VoiceID = defaultSupertonicVoiceID(provider)
	}
	if strings.TrimSpace(cfg.Language) == "" {
		cfg.Language = "auto"
	}
	if strings.TrimSpace(cfg.RuntimeMode) == "" {
		cfg.RuntimeMode = voiceRuntimeModePerTask
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = "http://127.0.0.1:7788"
	}
	_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
	return err
}

func defaultSupertonicVoiceID(provider setup.VoiceProviderOption) string {
	for _, model := range provider.Models {
		if model.Default && strings.TrimSpace(model.ID) != "" {
			return model.ID
		}
	}
	for _, model := range provider.Models {
		if strings.TrimSpace(model.ID) != "" {
			return model.ID
		}
	}
	return "M1"
}

func (d *Dispatcher) reselectSTTModelAfterDelete(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, deletedModelID string) (bool, error) {
	deletedModelID = strings.TrimSpace(deletedModelID)
	if deletedModelID == "" || !strings.EqualFold(provider.Config.ModelID, deletedModelID) {
		return false, nil
	}
	cfg := provider.Config
	for _, model := range installedVoiceModels(provider) {
		if strings.EqualFold(model.ID, deletedModelID) {
			continue
		}
		cfg.ModelID = model.ID
		_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
		return false, err
	}
	return d.disableVoiceModuleIfActiveProvider(ctx, module, provider)
}

func (d *Dispatcher) voiceLocalProvider(ctx context.Context, moduleID string, providerID string) (setup.VoiceModuleDescriptor, setup.VoiceProviderOption, bool, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return setup.VoiceModuleDescriptor{}, setup.VoiceProviderOption{}, false, err
	}
	providerID = strings.TrimSpace(providerID)
	for _, provider := range module.Providers {
		if provider.ID == providerID && provider.Local {
			return module, provider, true, nil
		}
	}
	return module, setup.VoiceProviderOption{}, false, nil
}
