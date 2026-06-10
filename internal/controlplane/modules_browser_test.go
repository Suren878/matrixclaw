package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

type browserRuntimeStub struct {
	module  setup.BrowserModuleDescriptor
	updates []setup.BrowserModuleUpdate
	actions []setup.BrowserProviderActionRequest
}

func (r *browserRuntimeStub) BrowserModule(context.Context) (setup.BrowserModuleDescriptor, error) {
	return r.module, nil
}

func (r *browserRuntimeStub) UpdateBrowserModule(_ context.Context, update setup.BrowserModuleUpdate) (setup.BrowserModuleDescriptor, error) {
	r.updates = append(r.updates, update)
	if update.Enabled != nil {
		r.module.Enabled = *update.Enabled
	}
	if update.ProviderID != "" {
		r.module.ProviderID = update.ProviderID
		r.module.ProviderName = update.ProviderID
	}
	if update.ProviderConfig != nil {
		r.module.Config = *update.ProviderConfig
		for i := range r.module.Providers {
			if r.module.Providers[i].ID == r.module.ProviderID {
				r.module.Providers[i].Config = *update.ProviderConfig
			}
		}
	}
	return r.module, nil
}

func (r *browserRuntimeStub) BrowserProviderAction(_ context.Context, providerID string, request setup.BrowserProviderActionRequest) (setup.BrowserProviderOption, error) {
	r.actions = append(r.actions, request)
	for _, provider := range r.module.Providers {
		if provider.ID == providerID {
			return provider, nil
		}
	}
	return setup.BrowserProviderOption{}, nil
}

func TestBrowserModulePickerShowsRuntimeAndMode(t *testing.T) {
	runtime := &browserRuntimeStub{module: browserModuleForProvider(browserPickerProvider(false))}
	result, err := New(runtime, "").handleBrowserModule(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("Picker = nil")
	}
	engine := requirePickerItem(t, result.Picker, "engine")
	if engine.Command != "/modules browser provider-action playwright install-runtime" || engine.Info != "Not Installed" || engine.Role != PickerItemRoleAction {
		t.Fatalf("engine item = %#v", engine)
	}
	if result.Picker.Items[0].ID == "engine" {
		t.Log("engine item is first")
	} else {
		t.Fatalf("first browser item = %q, want engine", result.Picker.Items[0].ID)
	}
	if pickerHasItem(result.Picker, "enabled", "Browser") {
		t.Fatalf("browser picker should not expose a separate Browser on/off item: %#v", result.Picker.Items)
	}
	provider := requirePickerItem(t, result.Picker, "provider")
	if provider.Command != "/modules browser provider-select" || provider.Info != "Local Playwright · Not Installed" || !provider.Selected {
		t.Fatalf("provider item = %#v", provider)
	}
	mode := requirePickerItem(t, result.Picker, "run-mode")
	if mode.Command != "/modules browser run-mode" || mode.Info != "Run per task" {
		t.Fatalf("run-mode item = %#v", mode)
	}
}

func TestBrowserModulePickerOffersInstallWhenBrowserMissing(t *testing.T) {
	provider := browserPickerProvider(true)
	provider.BrowserInstalled = false
	provider.Status = "Local · browser missing"
	runtime := &browserRuntimeStub{module: browserModuleForProvider(provider)}

	result, err := New(runtime, "").handleBrowserModule(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := requirePickerItem(t, result.Picker, "engine")
	if engine.Command != "/modules browser provider-action playwright install-runtime" {
		t.Fatalf("engine Command = %q, want install-runtime repair action", engine.Command)
	}
	if engine.Info != "Browser Missing" {
		t.Fatalf("engine Info = %q, want Browser Missing", engine.Info)
	}
	if engine.Role != PickerItemRoleAction {
		t.Fatalf("engine Role = %q, want action", engine.Role)
	}
}

func TestBrowserProviderPickerCanDisableOrSelectProvider(t *testing.T) {
	runtime := &browserRuntimeStub{module: browserModuleForProvider(browserPickerProvider(false))}

	result, err := New(runtime, "").handleBrowserModule(context.Background(), "provider-select")
	if err != nil {
		t.Fatal(err)
	}
	if result.Picker == nil {
		t.Fatal("Picker = nil")
	}
	disabled := requirePickerItem(t, result.Picker, "disabled")
	if disabled.Command != "/modules browser set-provider disabled" || disabled.Selected {
		t.Fatalf("disabled item = %#v", disabled)
	}
	playwright := requirePickerItem(t, result.Picker, "playwright")
	if playwright.Command != "/modules browser set-provider playwright" || playwright.Info != "Not Installed" || !playwright.Selected {
		t.Fatalf("playwright item = %#v", playwright)
	}

	_, err = New(runtime, "").handleBrowserModule(context.Background(), "set-provider disabled")
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.updates) != 1 || runtime.updates[0].Enabled == nil || *runtime.updates[0].Enabled {
		t.Fatalf("updates after disable = %#v", runtime.updates)
	}

	_, err = New(runtime, "").handleBrowserModule(context.Background(), "set-provider playwright")
	if err != nil {
		t.Fatal(err)
	}
	last := runtime.updates[len(runtime.updates)-1]
	if last.Enabled == nil || !*last.Enabled || last.ProviderID != "playwright" {
		t.Fatalf("updates after select = %#v", runtime.updates)
	}
}

func TestBrowserProviderActionUsesConfirmFlow(t *testing.T) {
	runtime := &browserRuntimeStub{module: browserModuleForProvider(browserPickerProvider(false))}
	result, err := New(runtime, "").handleBrowserModule(context.Background(), "provider-action playwright install-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if result.Confirm == nil {
		t.Fatal("Confirm = nil")
	}
	if result.Confirm.ConfirmCommand != "/modules browser provider-action playwright install-runtime-confirm" {
		t.Fatalf("ConfirmCommand = %q", result.Confirm.ConfirmCommand)
	}

	result, err = New(runtime, "").handleBrowserModule(context.Background(), "provider-action playwright install-runtime-confirm")
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.actions) != 1 || runtime.actions[0].Action != "install-runtime" {
		t.Fatalf("actions = %#v, want install-runtime", runtime.actions)
	}
}

func TestBrowserProviderInstallSkipsNoopEnableUpdate(t *testing.T) {
	provider := browserPickerProvider(true)
	runtime := &browserRuntimeStub{module: browserModuleForProvider(provider)}
	_, err := New(runtime, "").handleBrowserModule(context.Background(), "provider-action playwright install-runtime-confirm")
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.actions) != 1 || runtime.actions[0].Action != "install-runtime" {
		t.Fatalf("actions = %#v, want install-runtime", runtime.actions)
	}
	if len(runtime.updates) != 0 {
		t.Fatalf("updates = %#v, want no no-op enable update", runtime.updates)
	}
}

func TestBrowserProviderInstallDoesNotEnableDisabledModule(t *testing.T) {
	provider := browserPickerProvider(true)
	module := browserModuleForProvider(provider)
	module.Enabled = false
	runtime := &browserRuntimeStub{module: module}
	_, err := New(runtime, "").handleBrowserModule(context.Background(), "provider-action playwright install-runtime-confirm")
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.updates) == 0 {
		return
	}
	t.Fatalf("updates = %#v, want no enable update from runtime install", runtime.updates)
}

func browserModuleForProvider(provider setup.BrowserProviderOption) setup.BrowserModuleDescriptor {
	return setup.BrowserModuleDescriptor{
		ID: setup.BrowserModuleBrowser, Title: "Browser", Enabled: true,
		ProviderID: provider.ID, ProviderName: provider.Name, Local: provider.Local,
		Status: provider.Status, Config: provider.Config, Providers: []setup.BrowserProviderOption{provider},
	}
}

func browserPickerProvider(runtimeInstalled bool) setup.BrowserProviderOption {
	status := "Local · not installed"
	if runtimeInstalled {
		status = "Local · run per task"
	}
	return setup.BrowserProviderOption{
		ID: "playwright", Name: "Local Playwright", Local: true,
		Status: status, RuntimeInstalled: runtimeInstalled, BrowserInstalled: runtimeInstalled,
		RuntimeState: "stopped",
		ActionIDs: setup.BrowserProviderActionIDs{
			InstallRuntime: "install-runtime",
			DeleteRuntime:  "delete-runtime",
			Start:          "start",
			Stop:           "stop",
		},
		Config: setup.BrowserProviderConfig{RuntimeMode: "per_task"},
	}
}
