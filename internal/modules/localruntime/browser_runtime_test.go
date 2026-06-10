package localruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestDecorateBrowserProviderReportsPlaywrightInstallState(t *testing.T) {
	r := New(t.TempDir())
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	provider := setup.BrowserProviderOption{
		ID: "playwright", Name: "Local Playwright", Local: true,
		Config: setup.BrowserProviderConfig{RuntimeMode: "per_task"},
	}

	missing := r.DecorateBrowserProvider(provider)
	if missing.RuntimeInstalled {
		t.Fatal("RuntimeInstalled = true, want false before files exist")
	}
	if missing.RuntimeState != RuntimeUnavailable {
		t.Fatalf("RuntimeState = %q, want %q", missing.RuntimeState, RuntimeUnavailable)
	}
	if missing.Status != "Local · not installed" {
		t.Fatalf("Status = %q, want Local · not installed", missing.Status)
	}
	if missing.ActionIDs.InstallRuntime != ActionInstallRuntime {
		t.Fatalf("InstallRuntime action = %q, want %q", missing.ActionIDs.InstallRuntime, ActionInstallRuntime)
	}

	writeExecutable(t, r.managedPlaywrightMCPBinaryPath())
	writeFile(t, filepath.Join(r.playwrightBrowsersDir(), ".installed"), "chromium")

	legacy := r.DecorateBrowserProvider(provider)
	if !legacy.RuntimeInstalled {
		t.Fatal("RuntimeInstalled = false, want true after managed runtime files exist")
	}
	if legacy.BrowserInstalled {
		t.Fatal("BrowserInstalled = true for legacy chromium marker, want false for Playwright MCP")
	}
	if legacy.Status != "Local · browser missing" {
		t.Fatalf("Status = %q, want Local · browser missing", legacy.Status)
	}

	writeFile(t, r.managedPlaywrightMCPBrowsersJSONPath(), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	if err := os.MkdirAll(filepath.Join(r.playwrightBrowsersDir(), "chromium-1224"), 0o755); err != nil {
		t.Fatal(err)
	}

	installed := r.DecorateBrowserProvider(provider)
	if !installed.RuntimeInstalled {
		t.Fatal("RuntimeInstalled = false, want true after managed runtime files exist")
	}
	if !installed.BrowserInstalled {
		t.Fatal("BrowserInstalled = false, want true after required Chromium revision exists")
	}
	if installed.RuntimeState != RuntimeStopped {
		t.Fatalf("RuntimeState = %q, want %q", installed.RuntimeState, RuntimeStopped)
	}
	if installed.Status != "Local · run per task" {
		t.Fatalf("Status = %q, want Local · run per task", installed.Status)
	}
	if installed.RuntimePath != r.managedPlaywrightMCPBinaryPath() {
		t.Fatalf("RuntimePath = %q, want %q", installed.RuntimePath, r.managedPlaywrightMCPBinaryPath())
	}
}

func TestDecorateBrowserProviderAcceptsHoistedPlaywrightCoreCatalog(t *testing.T) {
	r := New(t.TempDir())
	provider := setup.BrowserProviderOption{
		ID: "playwright", Name: "Local Playwright", Local: true,
		Config: setup.BrowserProviderConfig{RuntimeMode: "per_task"},
	}

	writeExecutable(t, r.managedPlaywrightMCPBinaryPath())
	writeFile(t, filepath.Join(r.playwrightRuntimeDir(), "node_modules", "playwright-core", "browsers.json"), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	if err := os.MkdirAll(filepath.Join(r.playwrightBrowsersDir(), "chromium-1224"), 0o755); err != nil {
		t.Fatal(err)
	}

	installed := r.DecorateBrowserProvider(provider)
	if !installed.BrowserInstalled {
		t.Fatal("BrowserInstalled = false, want true for hoisted playwright-core browsers.json")
	}
}

func TestApplyBrowserActionRejectsRemoteProvider(t *testing.T) {
	r := New(t.TempDir())
	_, err := r.ApplyBrowserAction(context.Background(), setup.BrowserProviderOption{ID: "remote", Name: "Remote"}, setup.BrowserProviderActionRequest{Action: ActionInstallRuntime})
	if err == nil || err.Error() != "browser provider is not local" {
		t.Fatalf("error = %v, want browser provider is not local", err)
	}
}

func TestPlaywrightMCPServerArgsAddsNoSandboxForLinuxRoot(t *testing.T) {
	r := New(t.TempDir())
	provider := setup.BrowserProviderOption{
		Config: setup.BrowserProviderConfig{RuntimeMode: "per_task"},
	}

	rootArgs := r.playwrightMCPServerArgsForPlatform(provider, "linux", 0)
	if !stringSliceContains(rootArgs, "--no-sandbox") {
		t.Fatalf("args = %#v, want --no-sandbox for linux root", rootArgs)
	}
	if !stringSliceContains(rootArgs, "--isolated") {
		t.Fatalf("args = %#v, want per-task isolated profile", rootArgs)
	}

	nonRootArgs := r.playwrightMCPServerArgsForPlatform(provider, "linux", 1000)
	if stringSliceContains(nonRootArgs, "--no-sandbox") {
		t.Fatalf("args = %#v, want sandbox enabled for non-root linux", nonRootArgs)
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
