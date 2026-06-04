package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"

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
			Popup().
			Close(voiceModuleCommand(module.ID)).
			Item(PickerItem{ID: "on", Title: "On", Info: module.Title, Selected: module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Info: module.Title, Selected: !module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "off")}).
			Ptr(),
	}, nil
}

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
		Popup().
		Close(voiceModuleCommand(module.ID)).
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
			CancelLabel:    "Cancel",
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
		CancelLabel:    "Cancel",
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
				result.Picker.CloseCommand = result.Picker.BackCommand
				result.Picker.HasBack = true
				result.Picker.HasClose = true
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
