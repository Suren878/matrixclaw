// Package web is the module offering web_search and web_fetch.
package web

import (
	"context"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webtools"
)

type Module struct {
	config atomic.Pointer[setup.WebSearchConfig]
	tools  []tools.Executor
}

func New() *Module {
	m := &Module{}
	m.config.Store(&setup.WebSearchConfig{})
	m.tools = []tools.Executor{webtools.NewFetchTool(), webtools.NewSearchTool(m.searchConfig)}
	return m
}

func (m *Module) ID() string { return "web_search" }

func (m *Module) Apply(_ context.Context, cfg setup.Config) error {
	search := cfg.Modules.WebSearch
	m.config.Store(&search)
	return nil
}

func (m *Module) searchConfig() (webtools.SearchConfig, error) {
	cfg := m.config.Load()
	return webtools.SearchConfig{Provider: cfg.Provider, TavilyKey: cfg.TavilyKey, SerperKey: cfg.SerperKey, BaseURL: cfg.BaseURL}, nil
}

func (m *Module) Tools() []tools.Executor { return m.tools }

func (m *Module) Context() string { return "" }

func (m *Module) Close() error { return nil }

func (m *Module) Status(context.Context) modules.Status {
	provider := searchProviders[0].name
	for _, p := range searchProviders {
		if p.id == providerID(*m.config.Load()) {
			provider = p.name
		}
	}
	return modules.Status{
		ID:      m.ID(),
		Title:   "Web Search",
		Enabled: true,
		Ready:   true,
		State:   provider,
		Facts:   []modules.Fact{{Key: "provider", Label: "Provider", Value: provider}},
	}
}
