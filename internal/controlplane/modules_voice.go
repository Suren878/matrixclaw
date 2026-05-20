package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleVoiceModule(ctx context.Context, moduleID string, args string) (Result, error) {
	if d.voiceModules == nil {
		return unsupportedRuntime("voice modules"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.voiceModulePicker(ctx, moduleID)
	case "enabled":
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	case "set-enabled":
		return d.setVoiceModuleEnabled(ctx, moduleID, rest)
	case "provider":
		if strings.TrimSpace(rest) == "" {
			return d.voiceModuleProviderPicker(ctx, moduleID)
		}
		return d.voiceModuleProviderForm(ctx, moduleID, rest)
	case "provider-form":
		return d.voiceModuleProviderFormFromToken(ctx, moduleID, rest)
	case "provider-field":
		return d.voiceModuleProviderField(ctx, moduleID, rest)
	case "provider-set":
		return d.voiceModuleProviderSet(ctx, moduleID, rest)
	case "provider-save":
		return d.saveVoiceModuleProvider(ctx, moduleID, rest)
	case "provider-model":
		return d.voiceLocalProviderModelPicker(ctx, moduleID, rest)
	case "provider-language":
		return d.voiceLocalProviderLanguagePicker(ctx, moduleID, rest)
	case "provider-installed":
		return d.voiceInstalledLocalPicker(ctx, moduleID, rest)
	case "provider-installed-action":
		return d.voiceInstalledLocalActionPicker(ctx, moduleID, rest)
	case "provider-use":
		return d.useInstalledLocalModel(ctx, moduleID, rest)
	case "provider-threads":
		return d.voiceLocalProviderThreadsPicker(ctx, moduleID, rest)
	case "provider-autostart", "provider-run-mode":
		return d.voiceLocalProviderRunModePicker(ctx, moduleID, rest)
	case "provider-set-local":
		return d.setVoiceLocalProviderConfig(ctx, moduleID, rest)
	case "provider-status":
		return d.voiceLocalProviderStatus(ctx, moduleID, rest)
	case "provider-action":
		return d.voiceLocalProviderAction(ctx, moduleID, rest)
	case "info":
		return d.voiceModuleInfo(ctx, moduleID)
	default:
		return d.voiceModulePicker(ctx, moduleID)
	}
}

func (d *Dispatcher) voiceModulePicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	providerInfo := module.ProviderName
	providerDisabled := !module.Enabled
	if providerDisabled {
		providerInfo = "Turn on " + module.Title + " first"
	}
	picker := NewPickerData(voiceModulePickerKind(module.ID), module.Title).
		Context(module.ID).
		Back(modulesCommand()).
		Row("enabled", module.Title, formatEnabled(module.Enabled), voiceModuleCommand(module.ID, "enabled"))
	picker.Item(PickerItem{
		ID:       "provider",
		Title:    "Provider",
		Info:     providerInfo,
		Command:  voiceModuleCommand(module.ID, "provider"),
		Disabled: providerDisabled,
	})
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceModuleEnabledPicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerVoiceEnabled, module.Title).
			Context(module.ID).
			Meta("Module is " + strings.ToLower(formatEnabled(module.Enabled))).
			Back(voiceModuleCommand(module.ID)).
			Item(PickerItem{ID: "enable", Title: "Turn on", Info: module.Title, Selected: module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "enable")}).
			Item(PickerItem{ID: "disable", Title: "Turn off", Info: module.Title, Selected: !module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "disable")}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) voiceModuleProviderPicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if !module.Enabled {
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	}
	picker := NewPickerData(PickerVoiceProvider, module.Title+" Provider").
		Context(module.ID).
		Meta("Currently " + module.ProviderName).
		Back(voiceModuleCommand(module.ID))
	for _, provider := range module.Providers {
		picker.Item(PickerItem{
			ID:       provider.ID,
			Title:    voiceProviderPickerTitle(module, provider),
			Info:     voiceProviderPickerInfo(module, provider),
			Selected: provider.ID == module.ProviderID,
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) setVoiceModuleEnabled(ctx context.Context, moduleID string, value string) (Result, error) {
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "enable", "enabled":
		enabled = true
	case "no", "off", "false", "disable", "disabled":
		enabled = false
	default:
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	}
	update := setup.VoiceModuleUpdate{Enabled: &enabled}
	if enabled && moduleID == setup.VoiceModuleTTS {
		module, err := d.voiceModule(ctx, moduleID)
		if err != nil {
			return Result{}, err
		}
		if module.ProviderID == "piper" {
			cfg := module.Config
			cfg.RuntimeMode = voiceRuntimeModePerTask
			update.ProviderID = module.ProviderID
			update.ProviderConfig = &cfg
		}
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, update); err != nil {
		return Result{}, err
	}
	return d.voiceModulePicker(ctx, moduleID)
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
	if module.ID == setup.VoiceModuleTTS {
		return voiceLocalTTSPicker(module, provider), nil
	}
	if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		return voiceLocalSTTPicker(module, provider), nil
	}
	cfg := provider.Config
	title := voiceProviderTitle(module, provider)
	downloaded := voiceProviderDownloaded(provider)
	runtimeState := voiceRuntimeState(provider)
	runtimeActionsAvailable := voiceRuntimeActionsAvailable(provider)
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
		picker.Item(PickerItem{ID: "delete", Title: deleteTitle, Info: deleteActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "delete"), Disabled: runtimeState == "running", Role: PickerItemRoleDanger})
		if voiceRunModeAlways(provider) {
			picker.Item(PickerItem{ID: "runtime", Title: "Runtime manager", Info: voiceRuntimeManagerInfo(provider), Command: voiceModuleCommand(module.ID, "provider-status", provider.ID), Role: PickerItemRoleAction})
			picker.Item(PickerItem{ID: "start", Title: "Start runtime", Info: startActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "start"), Disabled: !runtimeActionsAvailable || runtimeState == "running" || runtimeState == "starting"})
			picker.Item(PickerItem{ID: "stop", Title: "Stop runtime", Info: stopActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "stop"), Disabled: !runtimeActionsAvailable || runtimeState != "running"})
			picker.Item(PickerItem{ID: "restart", Title: "Restart runtime", Info: restartActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "restart"), Disabled: !runtimeActionsAvailable || runtimeState != "running"})
		}
		picker.Item(PickerItem{ID: "run-mode", Title: "Run mode", Info: voiceRunModeLabel(provider), Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID)})
	} else {
		picker.Item(PickerItem{ID: "download", Title: downloadTitle, Info: downloadActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "download")})
	}
	picker.Item(PickerItem{ID: "provider", Title: "Provider", Info: provider.Name, Command: voiceModuleCommand(module.ID, "provider"), Role: PickerItemRoleAction})
	picker.Row("module", module.Title, formatEnabled(module.Enabled), voiceModuleCommand(module.ID, "enabled"))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}
