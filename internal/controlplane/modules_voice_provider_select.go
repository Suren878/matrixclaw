package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voiceModuleProviderPicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if module.ID == setup.VoiceModuleTTS || module.ID == setup.VoiceModuleSTT {
		return d.voiceModuleProviderSelectPicker(ctx, moduleID)
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
			Title:    voiceProviderPickerTitle(provider),
			Info:     voiceProviderPickerInfo(module, provider),
			Selected: provider.ID == module.ProviderID,
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceModuleProviderSelectPicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if module.ID != setup.VoiceModuleTTS && module.ID != setup.VoiceModuleSTT {
		return d.voiceModuleProviderPicker(ctx, moduleID)
	}
	title := "TTS Provider"
	if module.ID == setup.VoiceModuleSTT {
		title = "STT Provider"
	}
	picker := NewPickerData(PickerVoiceProvider, title).
		Context(module.ID).
		Select(voiceModuleCommand(module.ID)).
		Item(PickerItem{
			ID:       "disabled",
			Title:    "Disabled",
			Selected: !module.Enabled,
			Command:  voiceModuleCommand(module.ID, "set-provider", "disabled"),
		})
	for _, provider := range module.Providers {
		selected := module.Enabled && provider.ID == module.ProviderID
		picker.Item(PickerItem{
			ID:       provider.ID,
			Title:    voiceProviderPickerTitle(provider),
			Info:     voiceProviderSelectionInfo(module.ID, provider, selected),
			Selected: selected,
			Command:  voiceModuleCommand(module.ID, "set-provider", provider.ID),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) setVoiceModuleProvider(ctx context.Context, moduleID string, value string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	value = strings.TrimSpace(value)
	if module.ID != setup.VoiceModuleTTS && module.ID != setup.VoiceModuleSTT {
		return d.voiceModuleProviderForm(ctx, moduleID, value)
	}
	if strings.EqualFold(value, "disabled") || strings.TrimSpace(value) == "" {
		if err := d.stopOtherVoiceModuleProviders(ctx, module, ""); err != nil {
			return Result{}, err
		}
		enabled := false
		if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, setup.VoiceModuleUpdate{Enabled: &enabled}); err != nil {
			return Result{}, err
		}
		return d.voiceModulePicker(ctx, moduleID)
	}
	provider, ok := voiceProviderFromModule(module, value)
	if !ok {
		return d.voiceModuleProviderSelectPicker(ctx, moduleID)
	}
	if provider.ID == "whispercpp" {
		if _, ok := activeInstalledModel(provider); !ok {
			return d.voiceLocalProviderModelPicker(ctx, module.ID, provider.ID)
		}
	}
	if voiceProviderNeedsRuntimeInstall(provider) {
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Download " + provider.Name + " engine?",
			ConfirmLabel:   "Download",
			CancelLabel:    "Close",
			ConfirmCommand: voiceModuleCommand(module.ID, "set-provider-install", provider.ID),
			CancelCommand:  voiceModuleCommand(module.ID, "provider-select"),
		}}, nil
	}
	if provider.ID == "piper" {
		if _, ok := activeInstalledVoice(provider); !ok {
			return voiceProviderNeedsVoiceResult(module, provider, voiceModuleCommand(module.ID, "provider-select")), nil
		}
	}
	return d.activateVoiceModuleProvider(ctx, module, provider, false)
}

func (d *Dispatcher) installAndSetVoiceModuleProvider(ctx context.Context, moduleID string, args string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if module.ID != setup.VoiceModuleTTS && module.ID != setup.VoiceModuleSTT {
		return d.voiceModuleProviderPicker(ctx, moduleID)
	}
	providerID := firstField(args)
	provider, ok := voiceProviderFromModule(module, providerID)
	if !ok {
		return d.voiceModuleProviderSelectPicker(ctx, moduleID)
	}
	if provider.Local && voicePersistentProvider(module.ID, provider.ID) && !provider.RuntimeInstalled {
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
	if provider.ID == "piper" {
		if _, ok := activeInstalledVoice(provider); !ok {
			return voiceProviderNeedsVoiceResult(module, provider, voiceModuleCommand(module.ID, "provider-select")), nil
		}
	}
	if provider.ID == "whispercpp" {
		if _, ok := activeInstalledModel(provider); !ok {
			return d.voiceLocalProviderModelPicker(ctx, module.ID, provider.ID)
		}
	}
	modules, err := d.activateVoiceModuleProviderState(ctx, module, provider, true)
	if err != nil {
		return Result{}, err
	}
	if activated, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
		return d.voiceLocalProviderPickerWithProvider(module, activated)
	}
	return d.voiceModulePicker(ctx, module.ID)
}

func voiceProviderNeedsVoiceResult(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, cancelCommand string) Result {
	if strings.TrimSpace(cancelCommand) == "" {
		cancelCommand = voiceModuleCommand(module.ID, "provider-select")
	}
	return Result{Handled: true, Confirm: &ConfirmData{
		Title:          "Voice Required",
		Message:        provider.Name + " needs at least one installed voice before it can be enabled.",
		ConfirmLabel:   "Add Voice",
		CancelLabel:    "Close",
		ConfirmCommand: addLocalModelCommand(module.ID, provider.ID),
		CancelCommand:  cancelCommand,
	}}
}

func (d *Dispatcher) activateVoiceModuleProvider(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, forcePerTask bool) (Result, error) {
	if _, err := d.activateVoiceModuleProviderState(ctx, module, provider, forcePerTask); err != nil {
		return Result{}, err
	}
	return d.voiceModulePicker(ctx, module.ID)
}

func (d *Dispatcher) activateVoiceModuleProviderState(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, forcePerTask bool) ([]setup.VoiceModuleDescriptor, error) {
	if err := d.stopOtherVoiceModuleProviders(ctx, module, provider.ID); err != nil {
		return nil, err
	}
	enabled := true
	update := setup.VoiceModuleUpdate{Enabled: &enabled, ProviderID: provider.ID}
	if provider.Local && voicePersistentProvider(module.ID, provider.ID) {
		cfg := provider.Config
		if forcePerTask || strings.TrimSpace(cfg.RuntimeMode) == "" {
			cfg.RuntimeMode = voiceRuntimeModePerTask
			cfg.Autostart = false
		}
		cfg = defaultLocalTTSProviderConfig(provider, cfg)
		update.ProviderConfig = &cfg
	}
	modules, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, update)
	if err != nil {
		return nil, err
	}
	if provider, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok && provider.Local && voiceRunModeAlways(provider) && voiceLocalRuntimeStartReady(module.ID, provider, provider.Config) {
		if err := d.stopOtherVoiceModuleProviders(ctx, module, provider.ID); err != nil {
			return nil, err
		}
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
			return nil, err
		}
	}
	return modules, nil
}

func defaultLocalTTSProviderConfig(provider setup.VoiceProviderOption, cfg setup.VoiceProviderConfig) setup.VoiceProviderConfig {
	if provider.ID == "supertonic" {
		if strings.TrimSpace(cfg.VoiceID) == "" {
			cfg.VoiceID = defaultSupertonicVoiceID(provider)
		}
		if strings.TrimSpace(cfg.Language) == "" {
			cfg.Language = "auto"
		}
		if strings.TrimSpace(cfg.Endpoint) == "" {
			cfg.Endpoint = "http://127.0.0.1:7788"
		}
	}
	return cfg
}

func voiceProviderSettingsBackCommand(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS || moduleID == setup.VoiceModuleSTT {
		return voiceModuleCommand(moduleID, "provider-setup", providerID)
	}
	return voiceModuleCommand(moduleID, "provider", providerID)
}

func voiceProviderFromModule(module setup.VoiceModuleDescriptor, providerID string) (setup.VoiceProviderOption, bool) {
	providerID = strings.TrimSpace(providerID)
	for _, provider := range module.Providers {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return setup.VoiceProviderOption{}, false
}

func voiceProviderNeedsRuntimeInstall(provider setup.VoiceProviderOption) bool {
	return provider.Local && (provider.ID == "piper" || provider.ID == "supertonic" || provider.ID == "whispercpp") && !provider.RuntimeInstalled
}

func (d *Dispatcher) stopOtherVoiceModuleProviders(ctx context.Context, module setup.VoiceModuleDescriptor, keepProviderID string) error {
	keepProviderID = strings.TrimSpace(keepProviderID)
	for _, provider := range module.Providers {
		if provider.ID == keepProviderID || !provider.Local || !voiceRunModeAlways(provider) || !provider.RuntimeInstalled {
			continue
		}
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "stop"}); err != nil {
			return err
		}
	}
	return nil
}

func voiceProviderFromModules(modules []setup.VoiceModuleDescriptor, moduleID string, providerID string) (setup.VoiceProviderOption, bool) {
	for _, module := range modules {
		if module.ID != moduleID {
			continue
		}
		for _, provider := range module.Providers {
			if provider.ID == providerID {
				return provider, true
			}
		}
	}
	return setup.VoiceProviderOption{}, false
}

func (d *Dispatcher) refreshedVoiceProvider(ctx context.Context, moduleID string, providerID string) (setup.VoiceProviderOption, bool, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return setup.VoiceProviderOption{}, false, err
	}
	provider, ok := voiceProviderFromModule(module, providerID)
	return provider, ok, nil
}

func (d *Dispatcher) waitForVoiceProviderRuntimeInstalled(ctx context.Context, moduleID string, providerID string) (setup.VoiceProviderOption, bool, error) {
	var last setup.VoiceProviderOption
	for attempt := 0; attempt < 15; attempt++ {
		provider, ok, err := d.refreshedVoiceProvider(ctx, moduleID, providerID)
		if err != nil || !ok {
			return provider, ok, err
		}
		last = provider
		if !voiceProviderNeedsRuntimeInstall(provider) {
			return provider, true, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, true, ctx.Err()
		case <-timer.C:
		}
	}
	if voiceProviderNeedsRuntimeInstall(last) {
		return last, true, fmt.Errorf("%s engine installation did not finish; the engine is still reported as not installed", firstNonEmptyTrimmed(last.Name, providerID))
	}
	return last, true, nil
}
