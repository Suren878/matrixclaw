package daemoncmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	mcpmodule "github.com/Suren878/matrixclaw/internal/modules/mcp"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webresearch"
)

func webSearchProviderConfig(service *setup.Service) func() (tools.WebSearchProviderConfig, error) {
	return func() (tools.WebSearchProviderConfig, error) {
		if service == nil {
			return tools.WebSearchProviderConfig{}, nil
		}
		cfg, err := service.GetWebSearchConfig()
		if err != nil {
			return tools.WebSearchProviderConfig{}, err
		}
		return tools.WebSearchProviderConfig{
			Provider:  cfg.Provider,
			TavilyKey: cfg.TavilyKey,
			SerperKey: cfg.SerperKey,
			BaseURL:   cfg.BaseURL,
		}, nil
	}
}

func newWebResearchEngine(dbPath string, mcpCfg setup.MCPConfig, mcpModule *mcpmodule.Module, store *webresearch.WorkStore, webSearchConfig func() (tools.WebSearchProviderConfig, error)) *webresearch.Engine {
	browser := webResearchBrowserCapability(mcpCfg)
	if mcpModule != nil {
		if connected := mcpModule.Browser(); connected != nil {
			browser = connected
		}
	}
	return webresearch.NewEngine(webresearch.Config{
		Store:        store,
		ArtifactRoot: defaultWebResearchRoot(dbPath),
		Searcher:     webresearch.SearchFunc(webResearchSearch(webSearchConfig)),
		Fetcher:      webresearch.FetchFunc(webResearchFetch),
		Browser:      browser,
	})
}

func webResearchSearch(config func() (tools.WebSearchProviderConfig, error)) func(context.Context, string, int) (webresearch.SearchOutput, error) {
	return func(ctx context.Context, query string, limit int) (webresearch.SearchOutput, error) {
		cfg, err := config()
		if err != nil {
			return webresearch.SearchOutput{}, err
		}
		results, provider, err := tools.RunWebSearch(ctx, query, limit, cfg)
		if err != nil {
			return webresearch.SearchOutput{}, err
		}
		out := webresearch.SearchOutput{Provider: provider}
		for _, result := range results {
			out.Results = append(out.Results, webresearch.SearchResult{
				Position:    result.Position,
				Title:       result.Title,
				URL:         result.URL,
				Description: result.Description,
			})
		}
		return out, nil
	}
}

func webResearchFetch(ctx context.Context, rawURL string, maxChars int) (webresearch.FetchedPage, error) {
	page, err := tools.FetchWebPage(ctx, rawURL, maxChars)
	return webresearch.FetchedPage{
		URL:         page.URL,
		Title:       page.Title,
		Text:        page.Text,
		HTML:        page.HTML,
		StatusCode:  page.StatusCode,
		ContentType: page.ContentType,
		Truncated:   page.Truncated,
	}, err
}

func defaultWebResearchRoot(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = setup.DefaultDBPath()
	}
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}
	return filepath.Join(filepath.Dir(dbPath), "web-research")
}

type unavailableResearchBrowser struct {
	hint string
}

func (b unavailableResearchBrowser) Available() bool { return false }

func (b unavailableResearchBrowser) SetupHint() string {
	if strings.TrimSpace(b.hint) != "" {
		return b.hint
	}
	return "browser fallback is unavailable; enable an MCP browser server in /modules mcp with id browser and restart matrixclawd."
}

func (b unavailableResearchBrowser) Fetch(context.Context, string) (webresearch.BrowserPage, error) {
	return webresearch.BrowserPage{}, fmt.Errorf("%s", b.SetupHint())
}

func webResearchBrowserCapability(cfg setup.MCPConfig) webresearch.Browser {
	if !cfg.Enabled {
		return unavailableResearchBrowser{hint: "browser fallback is unavailable; enable the MCP module in /modules mcp, add a browser server with id browser, and restart matrixclawd."}
	}
	for _, server := range cfg.Servers {
		id := strings.ToLower(strings.TrimSpace(firstNonEmpty(server.ID, server.Name, server.ToolPrefix)))
		if server.Enabled && strings.Contains(id, "browser") {
			return unavailableResearchBrowser{hint: "browser fallback is unavailable to web_research; browser MCP tools are registered separately as mcp_<server>_* tools, but no native web_research browser adapter is configured."}
		}
	}
	return unavailableResearchBrowser{hint: "browser fallback is unavailable; add and enable an MCP browser server in /modules mcp with id browser, then restart matrixclawd."}
}
