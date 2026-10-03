// Package browser is the module of the local Playwright browser, whose tools
// the MCP module serves.
package browser

import (
	"cmp"
	"context"
	"fmt"
	"sync"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type Module struct {
	runtime *localruntime.Runtime
	mu      sync.RWMutex
	modules setup.ModulesConfig
}

func New(runtime *localruntime.Runtime) *Module {
	return &Module{runtime: runtime}
}

func (m *Module) ID() string { return setup.BrowserModuleBrowser }

func (m *Module) Apply(_ context.Context, cfg setup.Config) error {
	m.mu.Lock()
	m.modules = cfg.Modules
	m.mu.Unlock()
	return nil
}

func (m *Module) Tools() []tools.Executor { return nil }

func (m *Module) Context() string { return "" }

func (m *Module) Close() error { return nil }

// Descriptor is the module with the runtime's install state.
func (m *Module) Descriptor() setup.BrowserModuleDescriptor {
	m.mu.RLock()
	modulesCfg := m.modules
	m.mu.RUnlock()
	return m.runtime.DecorateBrowserModule(setup.BrowserModuleFromConfig(modulesCfg))
}

func (m *Module) Status(context.Context) modules.Status {
	module := m.Descriptor()
	provider := selected(module)
	return modules.Status{
		ID:      m.ID(),
		Title:   module.Title,
		Enabled: module.Enabled,
		Ready:   module.Enabled && provider.RuntimeInstalled && provider.BrowserInstalled,
		State:   module.Status,
		Detail:  provider.RuntimeDetail,
		Facts: []modules.Fact{
			{Key: "provider", Label: "Provider", Value: cmp.Or(module.ProviderName, module.ProviderID)},
			{Key: "mode", Label: "Run mode", Value: cmp.Or(provider.Config.RuntimeMode, "per_task")},
			{Key: "runtime_installed", Label: "Runtime installed", Value: fmt.Sprint(provider.RuntimeInstalled)},
			{Key: "browser_installed", Label: "Browser installed", Value: fmt.Sprint(provider.BrowserInstalled)},
		},
	}
}

// Action installs, removes or checks the provider's runtime.
func (m *Module) Action(ctx context.Context, providerID string, request setup.BrowserProviderActionRequest) (setup.BrowserProviderOption, error) {
	for _, provider := range m.Descriptor().Providers {
		if provider.ID == providerID {
			return m.runtime.ApplyBrowserAction(ctx, provider, request)
		}
	}
	return setup.BrowserProviderOption{}, fmt.Errorf("browser provider %q not found", providerID)
}

// MCPServer is the MCP server the enabled, installed browser runs as.
func (m *Module) MCPServer(cfg setup.Config) (setup.MCPServerConfig, bool) {
	module := setup.BrowserModuleFromConfig(cfg.Modules)
	if !module.Enabled {
		return setup.MCPServerConfig{}, false
	}
	return m.runtime.PlaywrightMCPServerConfig(selected(module))
}

func selected(module setup.BrowserModuleDescriptor) setup.BrowserProviderOption {
	for _, provider := range module.Providers {
		if provider.ID == module.ProviderID {
			return provider
		}
	}
	return setup.BrowserProviderOption{ID: module.ProviderID, Name: module.ProviderName, Config: module.Config}
}
