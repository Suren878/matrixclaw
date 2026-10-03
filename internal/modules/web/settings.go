package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type searchProvider struct {
	id, name, info string
	setting        string // the key or URL it needs, "" for none
}

var searchProviders = []searchProvider{
	{setup.WebSearchProviderDDG, "DuckDuckGo", "Free", ""},
	{setup.WebSearchProviderTavily, "Tavily", "1000 req/mo free", "tavily_key"},
	{setup.WebSearchProviderSerper, "Serper", "2500 req/mo free", "serper_key"},
	{setup.WebSearchProviderSearXNG, "SearXNG", "Self-hosted", "searxng_url"},
}

func (m *Module) Settings(context.Context) []modules.Item {
	cfg := *m.config.Load()
	provider := modules.Item{Key: "provider", Kind: modules.ItemChoice, Label: "Provider", Value: providerID(cfg)}
	for _, p := range searchProviders {
		provider.Options = append(provider.Options, modules.Option{Value: p.id, Label: p.name, Info: p.info})
	}
	return []modules.Item{
		provider,
		{Key: "tavily_key", Kind: modules.ItemSecret, Label: "Tavily API Key", Display: secretDisplay(cfg.TavilyKey), Hint: "tvly-..."},
		{Key: "serper_key", Kind: modules.ItemSecret, Label: "Serper API Key", Display: secretDisplay(cfg.SerperKey), Hint: "..."},
		{Key: "searxng_url", Kind: modules.ItemText, Label: "SearXNG Base URL", Value: cfg.BaseURL, Display: firstNonEmpty(cfg.BaseURL, "Not set"), Hint: "http://localhost:8888"},
	}
}

// Change picks the provider or sets its key or URL. A provider that still
// needs one opens it; setting one switches to its provider, clearing the
// active provider's falls back to DuckDuckGo.
func (m *Module) Change(_ context.Context, path []string, value string) (modules.Change, error) {
	if len(path) != 1 {
		return modules.Change{}, modules.Unknown(path)
	}
	cfg := *m.config.Load()
	if path[0] == "provider" {
		for _, p := range searchProviders {
			if p.id != value {
				continue
			}
			if p.setting != "" && setting(cfg, p.setting) == "" {
				return modules.Change{Open: []string{p.setting}, Message: p.name + " needs its " + settingName(p.setting) + " first."}, nil
			}
			return modules.Change{Config: func(c *setup.Config) error {
				c.Modules.WebSearch.Provider = value
				return validate(c.Modules.WebSearch)
			}}, nil
		}
		return modules.Change{}, fmt.Errorf("%w: unknown web search provider %q", modules.ErrInvalidSetting, value)
	}
	for _, p := range searchProviders {
		if p.setting != path[0] || p.setting == "" {
			continue
		}
		return modules.Change{Config: func(c *setup.Config) error {
			search := &c.Modules.WebSearch
			switch p.setting {
			case "tavily_key":
				search.TavilyKey = value
			case "serper_key":
				search.SerperKey = value
			case "searxng_url":
				search.BaseURL = value
			}
			switch {
			case value != "":
				search.Provider = p.id
			case providerID(*search) == p.id:
				search.Provider = setup.WebSearchProviderDDG
			}
			return validate(*search)
		}}, nil
	}
	return modules.Change{}, modules.Unknown(path)
}

func providerID(cfg setup.WebSearchConfig) string {
	for _, p := range searchProviders {
		if p.id == strings.ToLower(strings.TrimSpace(cfg.Provider)) {
			return p.id
		}
	}
	return setup.WebSearchProviderDDG
}

func setting(cfg setup.WebSearchConfig, key string) string {
	switch key {
	case "tavily_key":
		return strings.TrimSpace(cfg.TavilyKey)
	case "serper_key":
		return strings.TrimSpace(cfg.SerperKey)
	default:
		return strings.TrimSpace(cfg.BaseURL)
	}
}

func settingName(key string) string {
	if key == "searxng_url" {
		return "base URL"
	}
	return "API key"
}

func validate(cfg setup.WebSearchConfig) error {
	if url := strings.TrimSpace(cfg.BaseURL); providerID(cfg) == setup.WebSearchProviderSearXNG && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("%w: the SearXNG base URL must start with http:// or https://", modules.ErrInvalidSetting)
	}
	return nil
}

func secretDisplay(secret string) string {
	if strings.TrimSpace(secret) == "" {
		return "Not set"
	}
	return setup.MaskSecret(secret)
}

func firstNonEmpty(value string, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
