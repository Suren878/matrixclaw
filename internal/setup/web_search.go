package setup

import (
	"strings"
)

// normalizeWebSearchConfig trims the web search settings.
func normalizeWebSearchConfig(cfg WebSearchConfig) WebSearchConfig {
	if strings.TrimSpace(cfg.Provider) != "" {
		cfg.Provider = normalizeWebSearchProvider(cfg.Provider)
	}
	cfg.TavilyKey = strings.TrimSpace(cfg.TavilyKey)
	cfg.SerperKey = strings.TrimSpace(cfg.SerperKey)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
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
