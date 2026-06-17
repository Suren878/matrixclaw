package localruntime

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
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
	writeExecutable(t, managedChromiumExecutableForTest(r, "1224"))

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
	if installed.BrowserPath != managedChromiumExecutableForTest(r, "1224") {
		t.Fatalf("BrowserPath = %q, want managed Chromium executable", installed.BrowserPath)
	}
}

func TestDecorateBrowserProviderReportsStalePlaywrightBrowserRevision(t *testing.T) {
	r := New(t.TempDir())
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	provider := setup.BrowserProviderOption{
		ID: "playwright", Name: "Local Playwright", Local: true,
		Config: setup.BrowserProviderConfig{RuntimeMode: "per_task"},
	}
	writeExecutable(t, r.managedPlaywrightMCPBinaryPath())
	writeFile(t, r.managedPlaywrightMCPBrowsersJSONPath(), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	if err := os.MkdirAll(filepath.Join(r.playwrightBrowsersDir(), "chromium-1223"), 0o755); err != nil {
		t.Fatal(err)
	}

	decorated := r.DecorateBrowserProvider(provider)
	if !decorated.RuntimeInstalled {
		t.Fatal("RuntimeInstalled = false, want true after managed runtime files exist")
	}
	if decorated.BrowserInstalled {
		t.Fatal("BrowserInstalled = true, want false for stale browser revision")
	}
	if decorated.Status != "Local · browser repair required" {
		t.Fatalf("Status = %q, want Local · browser repair required", decorated.Status)
	}
	for _, want := range []string{"requires chromium-1224", "found chromium-1223", "Install/Repair"} {
		if !strings.Contains(decorated.RuntimeDetail, want) {
			t.Fatalf("RuntimeDetail missing %q:\n%s", want, decorated.RuntimeDetail)
		}
	}
}

func TestRepairPlaywrightBrowserCacheRemovesStaleRevisions(t *testing.T) {
	r := New(t.TempDir())
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	writeFile(t, r.managedPlaywrightMCPBrowsersJSONPath(), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	stale := filepath.Join(r.playwrightBrowsersDir(), "chromium-1223")
	required := filepath.Join(r.playwrightBrowsersDir(), "chromium-1224")
	other := filepath.Join(r.playwrightBrowsersDir(), "ffmpeg-1011")
	for _, path := range []string{stale, required, other} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.repairPlaywrightBrowserCache(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale browser dir still exists: %v", err)
	}
	for _, path := range []string{required, other} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("path %s = info %#v err %v, want preserved dir", path, info, err)
		}
	}
}

func TestPlaywrightMCPServerConfigUsesManagedChromiumExecutable(t *testing.T) {
	r := New(t.TempDir())
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	provider := setup.BrowserProviderOption{
		ID: "playwright", Name: "Local Playwright", Local: true,
		Config: setup.BrowserProviderConfig{RuntimeMode: "per_task"},
	}
	writeExecutable(t, r.managedPlaywrightMCPBinaryPath())
	writeFile(t, r.managedPlaywrightMCPBrowsersJSONPath(), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	executable := managedChromiumExecutableForTest(r, "1224")
	writeExecutable(t, executable)

	server, ok := r.PlaywrightMCPServerConfig(provider)
	if !ok {
		t.Fatal("PlaywrightMCPServerConfig ok = false, want true for installed managed Chromium")
	}
	if containsExactArg(server.Args, "--browser=chrome") {
		t.Fatalf("Args = %#v, must not require system Google Chrome", server.Args)
	}
	if !containsExactArg(server.Args, "--browser=chromium") {
		t.Fatalf("Args = %#v, want --browser=chromium", server.Args)
	}
	if !containsExactArg(server.Args, "--executable-path") || !containsExactArg(server.Args, executable) {
		t.Fatalf("Args = %#v, want executable path %q", server.Args, executable)
	}
	if got := server.Env["PLAYWRIGHT_BROWSERS_PATH"]; got != r.playwrightBrowsersDir() {
		t.Fatalf("PLAYWRIGHT_BROWSERS_PATH = %q, want %q", got, r.playwrightBrowsersDir())
	}
}

func TestApplyBrowserActionRejectsRemoteProvider(t *testing.T) {
	r := New(t.TempDir())
	_, err := r.ApplyBrowserAction(context.Background(), setup.BrowserProviderOption{ID: "remote", Name: "Remote"}, setup.BrowserProviderActionRequest{Action: ActionInstallRuntime})
	if err == nil || err.Error() != "browser provider is not local" {
		t.Fatalf("error = %v, want browser provider is not local", err)
	}
}

func managedChromiumExecutableForTest(r *Runtime, revision string) string {
	base := filepath.Join(r.playwrightBrowsersDir(), "chromium-"+revision)
	switch goruntime.GOOS {
	case "windows":
		return filepath.Join(base, "chrome-win", "chrome.exe")
	case "darwin":
		return filepath.Join(base, "chrome-mac", "Chromium.app", "Contents", "MacOS", "Chromium")
	default:
		return filepath.Join(base, "chrome-linux64", "chrome")
	}
}

func containsExactArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
