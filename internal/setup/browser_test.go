package setup

import "testing"

func TestBrowserModuleDescriptorsDefaultToLocalPlaywrightPerTask(t *testing.T) {
	module := BrowserModuleFromConfig(ModulesConfig{})

	if module.ID != BrowserModuleBrowser {
		t.Fatalf("ID = %q, want %q", module.ID, BrowserModuleBrowser)
	}
	if module.Title != "Browser" {
		t.Fatalf("Title = %q, want Browser", module.Title)
	}
	if module.Enabled {
		t.Fatal("Enabled = true, want false by default")
	}
	if module.ProviderID != BrowserProviderPlaywright {
		t.Fatalf("ProviderID = %q, want %q", module.ProviderID, BrowserProviderPlaywright)
	}
	if module.Config.RuntimeMode != "per_task" {
		t.Fatalf("RuntimeMode = %q, want per_task", module.Config.RuntimeMode)
	}
	if len(module.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(module.Providers))
	}
	provider := module.Providers[0]
	if provider.ID != BrowserProviderPlaywright || provider.Name != "Local Playwright" || !provider.Local {
		t.Fatalf("provider = %#v, want local playwright", provider)
	}
	if provider.ActionIDs.InstallRuntime != "install-runtime" || provider.ActionIDs.DeleteRuntime != "delete-runtime" {
		t.Fatalf("ActionIDs = %#v, want install/delete runtime actions", provider.ActionIDs)
	}
}

func TestUpdateBrowserModulePersistsEnabledProviderAndRuntimeMode(t *testing.T) {
	store := NewFileStore(t.TempDir() + "/setup.json")
	service := NewService(store)
	cfg := Config{
		Version: CurrentVersion,
		Daemon:  DaemonConfig{HTTPAddr: "127.0.0.1:8080", DBPath: t.TempDir() + "/matrixclaw.db"},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}

	enabled := true
	updated, err := service.UpdateBrowserModule(BrowserModuleUpdate{
		Enabled:    &enabled,
		ProviderID: BrowserProviderPlaywright,
		ProviderConfig: &BrowserProviderConfig{
			RuntimeMode: "always_running",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !updated.Enabled {
		t.Fatal("updated.Enabled = false, want true")
	}
	if updated.Config.RuntimeMode != "always_running" {
		t.Fatalf("RuntimeMode = %q, want always_running", updated.Config.RuntimeMode)
	}

	loaded, err := service.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Modules.Browser.Enabled {
		t.Fatal("persisted browser Enabled = false, want true")
	}
	if loaded.Modules.Browser.ProviderConfig.RuntimeMode != "always_running" {
		t.Fatalf("persisted RuntimeMode = %q, want always_running", loaded.Modules.Browser.ProviderConfig.RuntimeMode)
	}
}
