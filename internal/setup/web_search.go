package setup

import (
	"fmt"
	"strings"
)

func (s *Service) GetWebSearchConfig() (WebSearchConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return WebSearchConfig{}, err
	}
	return normalizeWebSearchConfig(cfg.Modules.WebSearch), nil
}

func (s *Service) UpdateWebSearchConfig(update WebSearchConfig) (WebSearchConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return WebSearchConfig{}, err
	}
	merged := mergeWebSearchConfig(cfg.Modules.WebSearch, update)
	if err := validateWebSearchConfig(merged); err != nil {
		return WebSearchConfig{}, err
	}
	cfg.Modules.WebSearch = normalizeWebSearchConfig(merged)
	if err := s.store.Save(cfg); err != nil {
		return WebSearchConfig{}, err
	}
	return normalizeWebSearchConfig(merged), nil
}

func WebSearchConfigStatus(cfg WebSearchConfig) string {
	cfg = normalizeWebSearchConfig(cfg)
	switch cfg.Provider {
	case WebSearchProviderTavily:
		return "Tavily"
	case WebSearchProviderSerper:
		return "Serper"
	case WebSearchProviderSearXNG:
		return "SearXNG"
	default:
		return "DuckDuckGo"
	}
}

// WebSearchEnvVars returns environment variable pairs for the active provider credentials.
func WebSearchEnvVars(cfg WebSearchConfig) map[string]string {
	cfg = normalizeWebSearchConfig(cfg)
	env := map[string]string{}
	switch cfg.Provider {
	case WebSearchProviderTavily:
		if cfg.TavilyKey != "" {
			env["TAVILY_API_KEY"] = cfg.TavilyKey
		}
	case WebSearchProviderSerper:
		if cfg.SerperKey != "" {
			env["SERPER_API_KEY"] = cfg.SerperKey
		}
	case WebSearchProviderSearXNG:
		if cfg.BaseURL != "" {
			env["SEARXNG_URL"] = cfg.BaseURL
		}
	}
	return env
}

func normalizeWebSearchConfig(cfg WebSearchConfig) WebSearchConfig {
	cfg.Provider = normalizeWebSearchProvider(cfg.Provider)
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

func mergeWebSearchConfig(existing, update WebSearchConfig) WebSearchConfig {
	merged := existing
	if strings.TrimSpace(update.Provider) != "" {
		merged.Provider = update.Provider
	}
	if strings.TrimSpace(update.TavilyKey) != "" {
		merged.TavilyKey = update.TavilyKey
	}
	if strings.TrimSpace(update.SerperKey) != "" {
		merged.SerperKey = update.SerperKey
	}
	if strings.TrimSpace(update.BaseURL) != "" {
		merged.BaseURL = update.BaseURL
	}
	if update.TavilyKey == "-" {
		merged.TavilyKey = ""
	}
	if update.SerperKey == "-" {
		merged.SerperKey = ""
	}
	if update.BaseURL == "-" {
		merged.BaseURL = ""
	}
	return merged
}

func validateWebSearchConfig(cfg WebSearchConfig) error {
	cfg = normalizeWebSearchConfig(cfg)
	switch cfg.Provider {
	case WebSearchProviderTavily:
		if cfg.TavilyKey == "" {
			return fmt.Errorf("tavily requires an API key (free at app.tavily.com)")
		}
	case WebSearchProviderSerper:
		if cfg.SerperKey == "" {
			return fmt.Errorf("serper requires an API key (free at serper.dev)")
		}
	case WebSearchProviderSearXNG:
		if cfg.BaseURL == "" {
			return fmt.Errorf("searxng requires a base URL")
		}
		if !strings.HasPrefix(cfg.BaseURL, "http://") && !strings.HasPrefix(cfg.BaseURL, "https://") {
			return fmt.Errorf("searxng base URL must start with http:// or https://")
		}
	}
	return nil
}
