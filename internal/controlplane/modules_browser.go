package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	browserRuntimeModePerTask = "per_task"
	browserRuntimeModeAlways  = "always_running"
)

func (d *Dispatcher) handleBrowserModule(ctx context.Context, args string) (Result, error) {
	if d.browserModules == nil {
		return unsupportedRuntime("browser"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.browserModulePicker(ctx)
	case "provider-select":
		return d.browserProviderSelectPicker(ctx)
	case "set-provider":
		return d.setBrowserProvider(ctx, rest)
	case "run-mode":
		return d.browserRunModePicker(ctx)
	case "set-runtime-mode":
		return d.setBrowserRuntimeMode(ctx, rest)
	case "provider-action":
		return d.browserProviderAction(ctx, rest)
	default:
		return d.browserModulePicker(ctx)
	}
}

func (d *Dispatcher) browserModulePicker(ctx context.Context) (Result, error) {
	module, err := d.browserModules.BrowserModule(ctx)
	if err == nil {
		provider, _ := selectedBrowserProvider(module)
		picker := NewPickerData(PickerBrowser, "Browser").
			Context(module.ID).
			Back(modulesCommand())
		providerItem := PickerItem{
			ID:       "provider",
			Title:    "Browser Provider",
			Info:     browserActiveProviderStatus(module, provider),
			Command:  browserCommand("provider-select"),
			Selected: module.Enabled,
		}
		if provider.ID == "" {
			picker.Item(providerItem)
			return Result{Handled: true, Picker: picker.Ptr()}, nil
		}
		action := browserRuntimeInstallAction(provider)
		picker.Item(PickerItem{
			ID:      "engine",
			Title:   "Engine",
			Info:    browserRuntimeInstallInfo(provider),
			Command: browserCommand("provider-action", provider.ID, action),
			Role:    browserRuntimeActionRole(provider, action),
		})
		picker.Item(providerItem)
		picker.Row("run-mode", "Runtime Mode", browserRunModeLabel(provider), browserCommand("run-mode"))
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	return Result{}, err
}

func (d *Dispatcher) browserProviderSelectPicker(ctx context.Context) (Result, error) {
	module, err := d.browserModules.BrowserModule(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerBrowser, "Browser Provider").
		Context(module.ID).
		Select(browserCommand()).
		Item(PickerItem{
			ID:       "disabled",
			Title:    "Disabled",
			Selected: !module.Enabled,
			Command:  browserCommand("set-provider", "disabled"),
		})
	for _, provider := range module.Providers {
		picker.Item(PickerItem{
			ID:       provider.ID,
			Title:    firstNonEmptyTrimmed(provider.Name, provider.ID),
			Info:     browserProviderSelectionInfo(provider),
			Selected: module.Enabled && provider.ID == module.ProviderID,
			Command:  browserCommand("set-provider", provider.ID),
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) setBrowserProvider(ctx context.Context, value string) (Result, error) {
	module, err := d.browserModules.BrowserModule(ctx)
	if err != nil {
		return Result{}, err
	}
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "disabled") || value == "" {
		enabled := false
		if _, err := d.browserModules.UpdateBrowserModule(ctx, setup.BrowserModuleUpdate{Enabled: &enabled}); err != nil {
			return Result{}, err
		}
		return d.browserModulePicker(ctx)
	}
	provider, ok := browserProviderFromModule(module, value)
	if !ok {
		return d.browserProviderSelectPicker(ctx)
	}
	enabled := true
	if _, err := d.browserModules.UpdateBrowserModule(ctx, setup.BrowserModuleUpdate{Enabled: &enabled, ProviderID: provider.ID, ProviderConfig: &provider.Config}); err != nil {
		return Result{}, err
	}
	return d.browserModulePicker(ctx)
}

func (d *Dispatcher) browserRunModePicker(ctx context.Context) (Result, error) {
	module, err := d.browserModules.BrowserModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider, _ := selectedBrowserProvider(module)
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerBrowser, "Browser Runtime Mode").
			Context(module.ID).
			Meta(browserRunModeLabel(provider)).
			Select(browserCommand()).
			Item(PickerItem{ID: "per-task", Title: "Run Per Task", Selected: normalizeBrowserRunMode(provider.Config.RuntimeMode) == browserRuntimeModePerTask, Command: browserCommand("set-runtime-mode", browserRuntimeModePerTask)}).
			Item(PickerItem{ID: "always-running", Title: "Always Running", Selected: normalizeBrowserRunMode(provider.Config.RuntimeMode) == browserRuntimeModeAlways, Command: browserCommand("set-runtime-mode", browserRuntimeModeAlways)}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) setBrowserRuntimeMode(ctx context.Context, value string) (Result, error) {
	module, err := d.browserModules.BrowserModule(ctx)
	if err != nil {
		return Result{}, err
	}
	cfg := module.Config
	cfg.RuntimeMode = normalizeBrowserRunMode(value)
	if _, err := d.browserModules.UpdateBrowserModule(ctx, setup.BrowserModuleUpdate{ProviderConfig: &cfg}); err != nil {
		return Result{}, err
	}
	return d.browserModulePicker(ctx)
}

func (d *Dispatcher) browserProviderAction(ctx context.Context, args string) (Result, error) {
	providerID, rest := firstCommandStep(args)
	action, _ := firstCommandStep(rest)
	if providerID == "" || action == "" {
		return d.browserModulePicker(ctx)
	}
	module, err := d.browserModules.BrowserModule(ctx)
	if err != nil {
		return Result{}, err
	}
	provider, ok := browserProviderFromModule(module, providerID)
	if !ok {
		return d.browserModulePicker(ctx)
	}
	confirmed := strings.HasSuffix(action, "-confirm")
	action = strings.TrimSuffix(action, "-confirm")
	if !confirmed && (action == provider.ActionIDs.InstallRuntime || action == provider.ActionIDs.DeleteRuntime) {
		return Result{Handled: true, Confirm: &ConfirmData{
			Title:          browserActionConfirmTitle(provider, action),
			Message:        browserActionConfirmMessage(provider, action),
			ConfirmLabel:   browserActionConfirmLabel(action),
			CancelLabel:    "Close",
			ConfirmCommand: browserCommand("provider-action", provider.ID, action+"-confirm"),
			CancelCommand:  browserCommand(),
			ConfirmDanger:  action == provider.ActionIDs.DeleteRuntime,
		}}, nil
	}
	updated, err := d.browserModules.BrowserProviderAction(ctx, provider.ID, setup.BrowserProviderActionRequest{Action: action})
	if err != nil {
		return Result{}, err
	}
	provider = updated
	return d.browserModulePicker(ctx)
}

func selectedBrowserProvider(module setup.BrowserModuleDescriptor) (setup.BrowserProviderOption, bool) {
	return browserProviderFromModule(module, module.ProviderID)
}

func browserProviderFromModule(module setup.BrowserModuleDescriptor, providerID string) (setup.BrowserProviderOption, bool) {
	for _, provider := range module.Providers {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return setup.BrowserProviderOption{}, false
}

func browserModuleListInfo(module setup.BrowserModuleDescriptor) string {
	if !module.Enabled {
		return ""
	}
	return strings.TrimSpace(module.ProviderName)
}

func browserActiveProviderStatus(module setup.BrowserModuleDescriptor, provider setup.BrowserProviderOption) string {
	if !module.Enabled {
		return "Disabled"
	}
	providerName := firstNonEmptyTrimmed(provider.Name, module.ProviderName, module.ProviderID)
	if provider.RuntimeInstalled && provider.BrowserInstalled {
		return strings.Join(nonEmptyStrings(providerName, browserRunModeLabel(provider)), " · ")
	}
	return strings.Join(nonEmptyStrings(providerName, browserRuntimeInstallInfo(provider)), " · ")
}

func browserProviderSelectionInfo(provider setup.BrowserProviderOption) string {
	return browserRuntimeInstallInfo(provider)
}

func browserRuntimeInstallAction(provider setup.BrowserProviderOption) string {
	if provider.RuntimeInstalled && provider.BrowserInstalled {
		return provider.ActionIDs.DeleteRuntime
	}
	return provider.ActionIDs.InstallRuntime
}

func browserRuntimeInstallInfo(provider setup.BrowserProviderOption) string {
	if provider.RuntimeInstalled && provider.BrowserInstalled {
		return "Installed"
	}
	if strings.Contains(strings.ToLower(provider.Status), "repair required") {
		return "Repair Required"
	}
	if provider.RuntimeInstalled {
		return "Browser Missing"
	}
	return "Not Installed"
}

func browserRuntimeActionRole(provider setup.BrowserProviderOption, action string) PickerItemRole {
	if action == provider.ActionIDs.DeleteRuntime {
		return PickerItemRoleDanger
	}
	return PickerItemRoleAction
}

func normalizeBrowserRunMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "always_running", "persistent", "server":
		return browserRuntimeModeAlways
	default:
		return browserRuntimeModePerTask
	}
}

func browserRunModeLabel(provider setup.BrowserProviderOption) string {
	if normalizeBrowserRunMode(provider.Config.RuntimeMode) == browserRuntimeModeAlways {
		return "Always running"
	}
	return "Run per task"
}

func browserActionConfirmTitle(provider setup.BrowserProviderOption, action string) string {
	if action == provider.ActionIDs.DeleteRuntime {
		return "Delete Browser Runtime"
	}
	return "Install Browser Runtime"
}

func browserActionConfirmMessage(provider setup.BrowserProviderOption, action string) string {
	if action == provider.ActionIDs.DeleteRuntime {
		return "Delete " + provider.Name + " runtime and managed Chromium files?"
	}
	if provider.RuntimeInstalled && !provider.BrowserInstalled {
		return "Download the managed Chromium browser required by " + provider.Name + "?"
	}
	return "Download " + provider.Name + " runtime and managed Chromium?"
}

func browserActionConfirmLabel(action string) string {
	if strings.Contains(action, "delete") {
		return "Delete"
	}
	return "Download"
}
