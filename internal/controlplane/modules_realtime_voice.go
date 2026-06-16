package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleRealtimeVoiceModule(ctx context.Context, args string) (Result, error) {
	if d.realtimeVoice == nil {
		return unsupportedRuntime("realtime voice"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.realtimeVoicePicker(ctx)
	case "enabled":
		return d.realtimeVoiceEnabledPicker(ctx)
	case "set-enabled":
		return d.setRealtimeVoiceEnabled(ctx, rest)
	case "provider", "provider-select":
		return d.realtimeVoiceProviderPicker(ctx)
	case "set-provider":
		return d.setRealtimeVoiceProvider(ctx, rest)
	case "setup", "provider-setup":
		return d.realtimeVoiceSetupPicker(ctx, rest)
	case "advanced":
		return d.realtimeVoiceAdvancedPicker(ctx, rest)
	case "voice":
		return d.realtimeVoiceVoicePicker(ctx, rest)
	case "model", "provider-model":
		return d.realtimeVoiceModelPicker(ctx, rest)
	case "language", "provider-language":
		return d.realtimeVoiceLanguagePicker(ctx, rest)
	case "setup-field", "provider-setup-field":
		return d.realtimeVoiceSetupField(ctx, rest)
	case "setup-set", "provider-setup-set":
		return d.realtimeVoiceSetupSet(ctx, rest)
	case "info", "status":
		return d.realtimeVoiceInfo(ctx)
	default:
		return d.realtimeVoicePicker(ctx)
	}
}

func (d *Dispatcher) realtimeVoicePicker(ctx context.Context) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerRealtimeVoice, module.Title).
			Context(module.ID).
			Back(modulesCommand()).
			Item(PickerItem{
				ID:       "provider",
				Title:    "Provider",
				Info:     realtimeVoiceProviderStatus(module),
				Command:  realtimeVoiceCommand("provider-select"),
				Selected: module.Enabled,
			}).
			Row("setup", "Provider Settings", realtimeVoiceSetupStatus(module), realtimeVoiceCommand("setup")).
			Row("status", "Status", module.Status, realtimeVoiceCommand("info")).
			Ptr(),
	}, nil
}

func (d *Dispatcher) realtimeVoiceEnabledPicker(ctx context.Context) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerRealtimeVoice, module.Title).
			Context(module.ID).
			Meta("Module is " + strings.ToLower(formatEnabled(module.Enabled))).
			Select(realtimeVoiceCommand()).
			Item(PickerItem{ID: "on", Title: "On", Info: realtimeVoiceEnableInfo(module), Selected: module.Enabled, Disabled: !realtimeVoiceModuleReady(module), Command: realtimeVoiceCommand("set-enabled", "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Info: module.Title, Selected: !module.Enabled, Command: realtimeVoiceCommand("set-enabled", "off")}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) setRealtimeVoiceEnabled(ctx context.Context, value string) (Result, error) {
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "enable", "enabled":
		enabled = true
	case "no", "off", "false", "disable", "disabled":
		enabled = false
	default:
		return d.realtimeVoiceEnabledPicker(ctx)
	}
	if enabled {
		module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
		if err != nil {
			return Result{}, err
		}
		if !realtimeVoiceModuleReady(module) {
			return d.realtimeVoiceSetupPicker(ctx, module.ProviderID)
		}
	}
	if _, err := d.realtimeVoice.UpdateRealtimeVoiceModule(ctx, setup.VoiceModuleUpdate{Enabled: &enabled}); err != nil {
		return Result{}, err
	}
	return d.realtimeVoicePicker(ctx)
}

func (d *Dispatcher) realtimeVoiceProviderPicker(ctx context.Context) (Result, error) {
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerVoiceProvider, "Realtime Voice Provider").
		Context(module.ID).
		Select(realtimeVoiceCommand()).
		Item(PickerItem{
			ID:       "disabled",
			Title:    "Disabled",
			Selected: !module.Enabled,
			Command:  realtimeVoiceCommand("set-provider", "disabled"),
		})
	for _, provider := range module.Providers {
		picker.Item(PickerItem{
			ID:       provider.ID,
			Title:    provider.Name,
			Info:     realtimeVoiceProviderSelectionInfo(provider),
			Selected: module.Enabled && provider.ID == module.ProviderID,
			Command:  realtimeVoiceCommand("set-provider", provider.ID),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) setRealtimeVoiceProvider(ctx context.Context, providerID string) (Result, error) {
	providerID = strings.TrimSpace(providerID)
	if strings.EqualFold(providerID, "disabled") || providerID == "" {
		enabled := false
		if _, err := d.realtimeVoice.UpdateRealtimeVoiceModule(ctx, setup.VoiceModuleUpdate{Enabled: &enabled}); err != nil {
			return Result{}, err
		}
		return d.realtimeVoicePicker(ctx)
	}
	module, err := d.realtimeVoice.RealtimeVoiceModule(ctx)
	if err != nil {
		return Result{}, err
	}
	if !realtimeVoiceProviderExists(module, providerID) {
		return d.realtimeVoiceProviderPicker(ctx)
	}
	provider := realtimeVoiceProviderByID(module, providerID)
	enabled := realtimeVoiceProviderConfigured(provider)
	if _, err := d.realtimeVoice.UpdateRealtimeVoiceModule(ctx, setup.VoiceModuleUpdate{Enabled: &enabled, ProviderID: providerID}); err != nil {
		return Result{}, err
	}
	if !enabled {
		return d.realtimeVoiceSetupPicker(ctx, providerID)
	}
	return d.realtimeVoicePicker(ctx)
}
