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
	case "provider-select":
		return d.voiceModuleProviderSelectPicker(ctx, moduleID)
	case "set-provider":
		return d.setVoiceModuleProvider(ctx, moduleID, rest)
	case "set-provider-install":
		return d.installAndSetVoiceModuleProvider(ctx, moduleID, rest)
	case "provider-setup":
		return d.voiceModuleProviderSetup(ctx, moduleID, rest)
	case "provider-setup-form":
		return d.voiceModuleProviderSetupFormFromToken(ctx, moduleID, rest)
	case "provider-setup-field":
		return d.voiceModuleProviderSetupField(ctx, moduleID, rest)
	case "provider-setup-set":
		return d.voiceModuleProviderSetupSet(ctx, moduleID, rest)
	case "provider-setup-save":
		return d.saveVoiceModuleProviderSetup(ctx, moduleID, rest)
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
	if module.ID == setup.VoiceModuleTTS {
		return d.ttsModulePicker(module), nil
	}
	if module.ID == setup.VoiceModuleSTT {
		return d.sttModulePicker(module), nil
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

func (d *Dispatcher) sttModulePicker(module setup.VoiceModuleDescriptor) Result {
	picker := NewPickerData(PickerSpeechToText, module.Title).
		Context(module.ID).
		Back(modulesCommand()).
		Item(PickerItem{
			ID:       "provider",
			Title:    "STT Provider",
			Info:     voiceActiveProviderStatus(module),
			Command:  voiceModuleCommand(module.ID, "provider-select"),
			Selected: module.Enabled,
		}).
		Row("setup-provider", "Setup Provider", "", voiceModuleCommand(module.ID, "provider-setup")).
		Row("status", "Status", "", voiceModuleCommand(module.ID, "info"))
	return Result{
		Handled: true,
		Picker:  picker.Ptr(),
	}
}

func (d *Dispatcher) ttsModulePicker(module setup.VoiceModuleDescriptor) Result {
	picker := NewPickerData(PickerTextToSpeech, module.Title).
		Context(module.ID).
		Back(modulesCommand()).
		Item(PickerItem{
			ID:       "provider",
			Title:    "TTS Provider",
			Info:     voiceActiveProviderStatus(module),
			Command:  voiceModuleCommand(module.ID, "provider-select"),
			Selected: module.Enabled,
		}).
		Row("setup-provider", "Setup Provider", "", voiceModuleCommand(module.ID, "provider-setup")).
		Row("status", "Status", "", voiceModuleCommand(module.ID, "info"))
	return Result{
		Handled: true,
		Picker:  picker.Ptr(),
	}
}

func voiceActiveProviderStatus(module setup.VoiceModuleDescriptor) string {
	if !module.Enabled {
		return "Disabled"
	}
	providerName := firstNonEmptyTrimmed(module.ProviderName, module.ProviderID)
	if provider, ok := selectedVoiceProvider(module); ok {
		providerName = firstNonEmptyTrimmed(provider.Name, providerName)
		if provider.Local {
			return strings.Join(nonEmptyStrings(providerName, voiceRunModeLabel(provider)), " · ")
		}
	}
	return providerName
}

func selectedVoiceProvider(module setup.VoiceModuleDescriptor) (setup.VoiceProviderOption, bool) {
	providerID := strings.TrimSpace(module.ProviderID)
	for _, provider := range module.Providers {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return setup.VoiceProviderOption{}, false
}

func (d *Dispatcher) voiceModuleEnabledPicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerVoiceProvider, module.Title).
			Context(module.ID).
			Meta("Module is " + strings.ToLower(formatEnabled(module.Enabled))).
			Select(voiceModuleCommand(module.ID)).
			Item(PickerItem{ID: "on", Title: "On", Info: module.Title, Selected: module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Info: module.Title, Selected: !module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "off")}).
			Ptr(),
	}, nil
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
	if enabled && (moduleID == setup.VoiceModuleTTS || moduleID == setup.VoiceModuleSTT) {
		module, err := d.voiceModule(ctx, moduleID)
		if err != nil {
			return Result{}, err
		}
		return d.setVoiceModuleProvider(ctx, moduleID, module.ProviderID)
	}
	if !enabled {
		module, err := d.voiceModule(ctx, moduleID)
		if err != nil {
			return Result{}, err
		}
		if err := d.stopOtherVoiceModuleProviders(ctx, module, ""); err != nil {
			return Result{}, err
		}
	}
	update := setup.VoiceModuleUpdate{Enabled: &enabled}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, update); err != nil {
		return Result{}, err
	}
	return d.voiceModulePicker(ctx, moduleID)
}
