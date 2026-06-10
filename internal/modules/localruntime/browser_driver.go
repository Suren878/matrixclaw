package localruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const (
	playwrightMCPBrowserArg           = "chromium"
	playwrightMCPBrowserInstallTarget = "chromium"
	playwrightMCPChromiumBrowserName  = "chromium"
)

func (r *Runtime) DecorateBrowserModule(module setup.BrowserModuleDescriptor) setup.BrowserModuleDescriptor {
	out := module
	for i := range out.Providers {
		out.Providers[i] = r.DecorateBrowserProvider(out.Providers[i])
		if out.Providers[i].ID == out.ProviderID {
			out.Config = out.Providers[i].Config
			out.Local = out.Providers[i].Local
			out.ProviderName = out.Providers[i].Name
			if out.Enabled {
				out.Status = out.Providers[i].Status
			}
		}
	}
	return out
}

func (r *Runtime) DecorateBrowserProvider(provider setup.BrowserProviderOption) setup.BrowserProviderOption {
	if provider.ID != setup.BrowserProviderPlaywright || !provider.Local {
		return provider
	}
	provider.ActionIDs = setup.BrowserProviderActionIDs{
		InstallRuntime: ActionInstallRuntime,
		DeleteRuntime:  ActionDeleteRuntime,
		Start:          ActionStart,
		Stop:           ActionStop,
		Test:           "test",
	}
	provider.BrowserCachePath = r.playwrightBrowsersDir()
	if executableFileExists(r.managedPlaywrightMCPBinaryPath()) {
		provider.RuntimeInstalled = true
		provider.RuntimePath = r.managedPlaywrightMCPBinaryPath()
	}
	if r.playwrightMCPBrowserInstalled() {
		provider.BrowserInstalled = true
		provider.BrowserPath = r.playwrightBrowsersDir()
	}
	switch {
	case !provider.RuntimeInstalled:
		provider.RuntimeState = RuntimeUnavailable
		provider.RuntimeDetail = "Playwright MCP runtime is not installed"
		provider.Status = "Local · not installed"
	case !provider.BrowserInstalled:
		provider.RuntimeState = RuntimeUnavailable
		provider.RuntimeDetail = "Playwright Chromium is not installed"
		provider.Status = "Local · browser missing"
	case browserProviderRunsPerTask(provider):
		provider.RuntimeState = RuntimeStopped
		provider.RuntimeDetail = ""
		provider.Status = "Local · run per task"
	default:
		provider.RuntimeState = RuntimeStopped
		provider.RuntimeDetail = "Restart matrixclaw architect after enabling always running browser mode"
		provider.Status = "Local · installed"
	}
	return provider
}

func (r *Runtime) ApplyBrowserAction(ctx context.Context, provider setup.BrowserProviderOption, request setup.BrowserProviderActionRequest) (setup.BrowserProviderOption, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if !provider.Local {
		return provider, errors.New("browser provider is not local")
	}
	if provider.ID != setup.BrowserProviderPlaywright {
		return provider, fmt.Errorf("unsupported browser provider %q", provider.ID)
	}
	switch action {
	case ActionInstallRuntime:
		if err := r.installPlaywrightBrowserRuntime(ctx); err != nil {
			return provider, err
		}
	case ActionDeleteRuntime:
		if err := r.deletePlaywrightBrowserRuntime(); err != nil {
			return provider, err
		}
	case ActionStart, "test":
		decorated := r.DecorateBrowserProvider(provider)
		if !decorated.RuntimeInstalled || !decorated.BrowserInstalled {
			return provider, errors.New("browser runtime is not installed")
		}
	case ActionStop:
		// The MCP command transport owns the server process. Reloading/stopping the
		// daemon closes it; per-task mode uses an isolated browser profile.
	default:
		return provider, fmt.Errorf("unsupported local browser action %q", action)
	}
	return r.DecorateBrowserProvider(provider), nil
}

func browserProviderRunsPerTask(provider setup.BrowserProviderOption) bool {
	switch strings.ToLower(strings.TrimSpace(provider.Config.RuntimeMode)) {
	case "always", "always_running", "persistent", "server":
		return false
	default:
		return true
	}
}

func (r *Runtime) installPlaywrightBrowserRuntime(ctx context.Context) error {
	npm, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm is required to install Playwright browser runtime")
	}
	installDir := r.playwrightRuntimeDir()
	if err := os.RemoveAll(installDir); err != nil {
		return err
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	if err := runRuntimeCommand(ctx, npm, "install", "--prefix", installDir, "@playwright/mcp@latest"); err != nil {
		return err
	}
	playwrightMCP := r.managedPlaywrightMCPBinaryPath()
	if err := os.MkdirAll(r.playwrightBrowsersDir(), 0o755); err != nil {
		return err
	}
	if !executableFileExists(playwrightMCP) {
		return fmt.Errorf("playwright MCP installation finished without playwright-mcp binary")
	}
	env := append(os.Environ(), "PLAYWRIGHT_BROWSERS_PATH="+r.playwrightBrowsersDir())
	if err := runRuntimeCommandWithEnv(ctx, env, playwrightMCP, "install-browser", playwrightMCPBrowserInstallTarget); err != nil {
		return err
	}
	if !r.playwrightMCPBrowserInstalled() {
		return fmt.Errorf("playwright MCP browser installation finished without %s", playwrightMCPBrowserInstallTarget)
	}
	return os.WriteFile(filepath.Join(r.playwrightBrowsersDir(), ".installed"), []byte(playwrightMCPBrowserInstallTarget+"\n"), 0o644)
}

func (r *Runtime) deletePlaywrightBrowserRuntime() error {
	if err := os.RemoveAll(r.playwrightRuntimeDir()); err != nil {
		return err
	}
	return os.RemoveAll(r.playwrightBrowsersDir())
}

func (r *Runtime) playwrightRuntimeDir() string {
	return filepath.Join(r.runtimeDir(), "browser", "playwright-mcp")
}

func (r *Runtime) playwrightBrowsersDir() string {
	return filepath.Join(r.runtimeDir(), "browser", "ms-playwright")
}

func (r *Runtime) managedPlaywrightMCPBinaryPath() string {
	return filepath.Join(r.playwrightRuntimeDir(), "node_modules", ".bin", platformScriptName("playwright-mcp"))
}

func (r *Runtime) managedPlaywrightMCPBrowsersJSONPath() string {
	return filepath.Join(r.playwrightRuntimeDir(), "node_modules", "@playwright", "mcp", "node_modules", "playwright-core", "browsers.json")
}

func (r *Runtime) managedPlaywrightMCPBrowsersJSONPaths() []string {
	return []string{
		r.managedPlaywrightMCPBrowsersJSONPath(),
		filepath.Join(r.playwrightRuntimeDir(), "node_modules", "playwright-core", "browsers.json"),
	}
}

func platformScriptName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".cmd"
	}
	return name
}

func (r *Runtime) playwrightMCPBrowserInstalled() bool {
	revision := r.playwrightMCPChromiumRevision()
	if revision == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(r.playwrightBrowsersDir(), playwrightMCPChromiumBrowserName+"-"+revision))
	return err == nil && info.IsDir()
}

func (r *Runtime) playwrightMCPChromiumRevision() string {
	type browserEntry struct {
		Name     string `json:"name"`
		Revision string `json:"revision"`
	}
	type browserCatalog struct {
		Browsers []browserEntry `json:"browsers"`
	}
	for _, path := range r.managedPlaywrightMCPBrowsersJSONPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var catalog browserCatalog
		if err := json.Unmarshal(data, &catalog); err != nil {
			continue
		}
		for _, browser := range catalog.Browsers {
			if strings.EqualFold(strings.TrimSpace(browser.Name), playwrightMCPChromiumBrowserName) {
				return strings.TrimSpace(browser.Revision)
			}
		}
	}
	return ""
}

func (r *Runtime) PlaywrightMCPServerConfig(provider setup.BrowserProviderOption) (setup.MCPServerConfig, bool) {
	provider = r.DecorateBrowserProvider(provider)
	if provider.ID != setup.BrowserProviderPlaywright || !provider.Local || !provider.RuntimeInstalled || !provider.BrowserInstalled {
		return setup.MCPServerConfig{}, false
	}
	args := r.playwrightMCPServerArgs(provider)
	return setup.MCPServerConfig{
		ID:              "browser",
		Name:            "Local Browser",
		Enabled:         true,
		Transport:       "stdio",
		Command:         provider.RuntimePath,
		Args:            args,
		Env:             map[string]string{"PLAYWRIGHT_BROWSERS_PATH": r.playwrightBrowsersDir()},
		ToolPrefix:      "browser",
		ReadOnly:        false,
		RequireApproval: true,
		TimeoutSeconds:  120,
	}, true
}

func (r *Runtime) playwrightMCPServerArgs(provider setup.BrowserProviderOption) []string {
	return r.playwrightMCPServerArgsForPlatform(provider, runtime.GOOS, os.Geteuid())
}

func (r *Runtime) playwrightMCPServerArgsForPlatform(provider setup.BrowserProviderOption, goos string, euid int) []string {
	args := []string{"--headless", "--browser=" + playwrightMCPBrowserArg, "--viewport-size=1280x720"}
	if goos == "linux" && euid == 0 {
		args = append(args, "--no-sandbox")
	}
	if browserProviderRunsPerTask(provider) {
		args = append(args, "--isolated")
	} else {
		args = append(args, "--user-data-dir", filepath.Join(r.runtimeDir(), "browser", "profile"))
	}
	return args
}

func (r *Runtime) ManagedPlaywrightMCPBinaryPathForTest() string {
	return r.managedPlaywrightMCPBinaryPath()
}

func (r *Runtime) PlaywrightBrowsersDirForTest() string {
	return r.playwrightBrowsersDir()
}

func (r *Runtime) ManagedPlaywrightMCPBrowsersJSONPathForTest() string {
	return r.managedPlaywrightMCPBrowsersJSONPath()
}
