package setup

import (
	"strings"
)

// normalizeWebSearchConfig trims the web search settings and moves a key
// saved by older versions to its provider's field.
func normalizeWebSearchConfig(cfg WebSearchConfig) WebSearchConfig {
	if strings.TrimSpace(cfg.Provider) != "" {
		cfg.Provider = normalizeWebSearchProvider(cfg.Provider)
	}
	cfg.TavilyKey = strings.TrimSpace(cfg.TavilyKey)
	cfg.SerperKey = strings.TrimSpace(cfg.SerperKey)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	// Migrate legacy api_key to the provider-specific field.
	if legacy := strings.TrimSpace(cfg.APIKey); legacy != "" {
		switch cfg.Provider {
		case WebSearchProviderTavily:
			if cfg.TavilyKey == "" {
				cfg.TavilyKey = legacy
			}
		case WebSearchProviderSerper:
			if cfg.SerperKey == "" {
				cfg.SerperKey = legacy
			}
		}
		cfg.APIKey = ""
	}
	return cfg
}

func normalizeWebSearchProvider(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case WebSearchProviderTavily:
		return WebSearchProviderTavily
	case WebSearchProviderSerper:
		return WebSearchProviderSerper
	case WebSearchProviderSearXNG:
		return WebSearchProviderSearXNG
	default:
		return WebSearchProviderDDG
	}
}
