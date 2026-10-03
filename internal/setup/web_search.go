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

func (s *Service) UpdateWebSearchConfig(update WebSearchConfigUpdate) (WebSearchConfig, error) {
	cfg, err := s.Update(func(cfg *Config) error {
		merged := mergeWebSearchConfig(cfg.Modules.WebSearch, update)
		if err := validateWebSearchConfig(merged); err != nil {
			return err
		}
		cfg.Modules.WebSearch = normalizeWebSearchConfig(merged)
		return nil
	})
	if err != nil {
		return WebSearchConfig{}, err
	}
	return normalizeWebSearchConfig(cfg.Modules.WebSearch), nil
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

func mergeWebSearchConfig(existing WebSearchConfig, update WebSearchConfigUpdate) WebSearchConfig {
	merged := existing
	setIfPresent(&merged.Provider, update.Provider)
	setIfPresent(&merged.TavilyKey, update.TavilyKey)
	setIfPresent(&merged.SerperKey, update.SerperKey)
	setIfPresent(&merged.BaseURL, update.BaseURL)
	return merged
}

// WebSearchResponse describes cfg for clients without its keys.
func WebSearchResponse(cfg WebSearchConfig) WebSearchConfigResponse {
	cfg = normalizeWebSearchConfig(cfg)
	return WebSearchConfigResponse{
		Provider:         cfg.Provider,
		Status:           WebSearchConfigStatus(cfg),
		BaseURL:          cfg.BaseURL,
		TavilyKeyPreview: MaskSecret(cfg.TavilyKey),
		SerperKeyPreview: MaskSecret(cfg.SerperKey),
	}
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
