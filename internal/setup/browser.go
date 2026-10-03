package setup

import (
	"cmp"
	"strings"
)

const (
	BrowserModuleBrowser      = "browser"
	BrowserProviderPlaywright = "playwright"
)

func (s *Service) UpdateBrowserModule(update BrowserModuleUpdate) (BrowserModuleDescriptor, error) {
	cfg, err := s.Update(func(cfg *Config) error {
		cfg.Modules.Browser = applyBrowserModuleUpdate(cfg.Modules.Browser, update)
		return nil
	})
	if err != nil {
		return BrowserModuleDescriptor{}, err
	}
	return BrowserModuleFromConfig(cfg.Modules), nil
}

func applyBrowserModuleUpdate(current BrowserConfig, update BrowserModuleUpdate) BrowserConfig {
	current = normalizeBrowserConfig(current)
	if update.Enabled != nil {
		current.Enabled = *update.Enabled
	}
	if providerID := normalizeBrowserProviderID(update.ProviderID); providerID != "" {
		current.ProviderID = providerID
	}
	if update.ProviderConfig != nil {
		next := normalizeBrowserProviderConfig(*update.ProviderConfig)
		if next.RuntimeMode != "" {
			current.ProviderConfig.RuntimeMode = next.RuntimeMode
		}
		if strings.TrimSpace(update.ProviderConfig.BinaryPath) != "" {
			current.ProviderConfig.BinaryPath = next.BinaryPath
		}
		if strings.TrimSpace(update.ProviderConfig.BrowserPath) != "" {
			current.ProviderConfig.BrowserPath = next.BrowserPath
		}
	}
	return normalizeBrowserConfig(current)
}

func BrowserModuleFromConfig(modules ModulesConfig) BrowserModuleDescriptor {
	cfg := normalizeBrowserConfig(modules.Browser)
	providers := browserProviders(cfg)
	selected := providers[0]
	for _, provider := range providers {
		if provider.ID == cfg.ProviderID {
			selected = provider
			break
		}
	}
	status := "Disabled"
	if cfg.Enabled {
		status = selected.Status
	}
	return BrowserModuleDescriptor{
		ID:           BrowserModuleBrowser,
		Title:        "Browser",
		Enabled:      cfg.Enabled,
		ProviderID:   selected.ID,
		ProviderName: selected.Name,
		Local:        selected.Local,
		Status:       status,
		Config:       selected.Config,
		Providers:    providers,
	}
}

// normalizeBrowserConfig is the browser module with its defaults filled.
func normalizeBrowserConfig(cfg BrowserConfig) BrowserConfig {
	cfg.ProviderID = cmp.Or(normalizeBrowserProviderID(cfg.ProviderID), BrowserProviderPlaywright)
	cfg.ProviderConfig = normalizeBrowserProviderConfig(cfg.ProviderConfig)
	return cfg
}

// storedBrowserConfig is the browser module without the values that only
// repeat its defaults.
func storedBrowserConfig(cfg BrowserConfig) BrowserConfig {
	cfg = normalizeBrowserConfig(cfg)
	cfg.ProviderID = omitDefault(cfg.ProviderID, BrowserProviderPlaywright)
	cfg.ProviderConfig.RuntimeMode = omitDefault(cfg.ProviderConfig.RuntimeMode, "per_task")
	return cfg
}

func normalizeBrowserProviderConfig(cfg BrowserProviderConfig) BrowserProviderConfig {
	cfg.RuntimeMode = normalizeRuntimeMode(cfg.RuntimeMode)
	cfg.BinaryPath = strings.TrimSpace(cfg.BinaryPath)
	cfg.BrowserPath = strings.TrimSpace(cfg.BrowserPath)
	return cfg
}

func normalizeRuntimeMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "always", "always_running", "persistent", "server":
		return "always_running"
	default:
		return "per_task"
	}
}

func normalizeBrowserProviderID(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", BrowserProviderPlaywright, "local-playwright", "local_playwright":
		return BrowserProviderPlaywright
	default:
		return ""
	}
}

func browserProviders(cfg BrowserConfig) []BrowserProviderOption {
	cfg = normalizeBrowserConfig(cfg)
	return []BrowserProviderOption{{
		ID:     BrowserProviderPlaywright,
		Name:   "Local Playwright",
		Local:  true,
		Status: "Local · not installed",
		ActionIDs: BrowserProviderActionIDs{
			InstallRuntime: "install-runtime",
			DeleteRuntime:  "delete-runtime",
			Start:          "start",
			Stop:           "stop",
			Test:           "test",
		},
		Config: cfg.ProviderConfig,
	}}
}
