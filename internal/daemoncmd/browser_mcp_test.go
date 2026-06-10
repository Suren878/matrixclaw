package daemoncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestMCPConfigWithBrowserAddsManagedPlaywrightServer(t *testing.T) {
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	runtime := localruntime.New("")
	writeExecutable(t, runtime.ManagedPlaywrightMCPBinaryPathForTest())
	writeFile(t, runtime.ManagedPlaywrightMCPBrowsersJSONPathForTest(), `{"browsers":[{"name":"chromium","revision":"1224"}]}`)
	if err := os.MkdirAll(filepath.Join(runtime.PlaywrightBrowsersDirForTest(), "chromium-1224"), 0o755); err != nil {
		t.Fatal(err)
	}

	modules := setup.ModulesConfig{
		MCP: setup.MCPConfig{Enabled: false},
		Browser: setup.BrowserConfig{
			Enabled:    true,
			ProviderID: setup.BrowserProviderPlaywright,
			ProviderConfig: setup.BrowserProviderConfig{
				RuntimeMode: "per_task",
			},
		},
	}

	got := mcpConfigWithBrowser(modules)
	if !got.Enabled {
		t.Fatal("MCP Enabled = false, want true when browser is active")
	}
	if len(got.Servers) != 1 {
		t.Fatalf("len(Servers) = %d, want 1", len(got.Servers))
	}
	server := got.Servers[0]
	if server.ID != "browser" || server.Transport != "stdio" || !server.Enabled {
		t.Fatalf("server basics = %#v, want enabled stdio browser server", server)
	}
	if server.Command != runtime.ManagedPlaywrightMCPBinaryPathForTest() {
		t.Fatalf("Command = %q, want managed playwright mcp path", server.Command)
	}
	if len(server.Args) == 0 || server.Args[0] != "--headless" {
		t.Fatalf("Args = %#v, want headless args", server.Args)
	}
	if !stringSliceContains(server.Args, "--browser=chromium") {
		t.Fatalf("Args = %#v, want Playwright MCP chromium browser", server.Args)
	}
	if server.Env["PLAYWRIGHT_BROWSERS_PATH"] != runtime.PlaywrightBrowsersDirForTest() {
		t.Fatalf("PLAYWRIGHT_BROWSERS_PATH = %q, want browser cache path", server.Env["PLAYWRIGHT_BROWSERS_PATH"])
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
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

func TestMCPConfigWithBrowserSkipsMissingRuntime(t *testing.T) {
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	modules := setup.ModulesConfig{
		MCP: setup.MCPConfig{Enabled: false},
		Browser: setup.BrowserConfig{
			Enabled:    true,
			ProviderID: setup.BrowserProviderPlaywright,
		},
	}

	got := mcpConfigWithBrowser(modules)
	if got.Enabled {
		t.Fatal("MCP Enabled = true, want false when browser runtime is missing")
	}
	if len(got.Servers) != 0 {
		t.Fatalf("len(Servers) = %d, want 0", len(got.Servers))
	}
}

func TestMCPConfigWithBrowserSkipsLegacyChromiumInstall(t *testing.T) {
	t.Setenv("MATRIXCLAW_RUNTIME_DIR", filepath.Join(t.TempDir(), "runtime"))
	runtime := localruntime.New("")
	writeExecutable(t, runtime.ManagedPlaywrightMCPBinaryPathForTest())
	writeFile(t, filepath.Join(runtime.PlaywrightBrowsersDirForTest(), ".installed"), "chromium")

	modules := setup.ModulesConfig{
		MCP: setup.MCPConfig{Enabled: false},
		Browser: setup.BrowserConfig{
			Enabled:    true,
			ProviderID: setup.BrowserProviderPlaywright,
		},
	}

	got := mcpConfigWithBrowser(modules)
	if got.Enabled {
		t.Fatal("MCP Enabled = true, want false when only legacy chromium install is present")
	}
	if len(got.Servers) != 0 {
		t.Fatalf("len(Servers) = %d, want 0", len(got.Servers))
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
