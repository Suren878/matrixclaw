package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Suren878/matrixclaw/internal/webresearch"
)

type browserClient struct {
	server     ServerConfig
	session    *sdk.ClientSession
	navigate   string
	snapshot   string
	screenshot string
	mu         sync.Mutex
}

func (m *ClientModule) Browser() webresearch.Browser {
	if m == nil {
		return nil
	}
	for _, session := range m.sessions {
		if session == nil || session.session == nil {
			continue
		}
		browser := browserClient{server: session.server, session: session.session}
		for _, tool := range session.tools {
			if tool == nil {
				continue
			}
			name := strings.TrimSpace(tool.Name)
			normalized := strings.ToLower(name)
			switch {
			case browser.navigate == "" && strings.Contains(normalized, "navigate"):
				browser.navigate = name
			case browser.snapshot == "" && (strings.Contains(normalized, "snapshot") || strings.Contains(normalized, "accessibility")):
				browser.snapshot = name
			case browser.screenshot == "" && strings.Contains(normalized, "screenshot"):
				browser.screenshot = name
			}
		}
		if browser.navigate != "" && browser.snapshot != "" {
			return &browser
		}
	}
	return nil
}

func (b *browserClient) Available() bool {
	return b != nil && b.session != nil && b.navigate != "" && b.snapshot != ""
}

func (b *browserClient) SetupHint() string {
	if b == nil {
		return "browser fallback is unavailable; configure an MCP browser server."
	}
	return "MCP browser server is connected: " + firstNonEmpty(b.server.Name, b.server.ID)
}

func (b *browserClient) Fetch(ctx context.Context, url string) (webresearch.BrowserPage, error) {
	if !b.Available() {
		return webresearch.BrowserPage{}, fmt.Errorf("%s", b.SetupHint())
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return webresearch.BrowserPage{}, fmt.Errorf("browser url is required")
	}
	if _, err := b.call(ctx, b.navigate, map[string]any{"url": url}); err != nil {
		return webresearch.BrowserPage{}, err
	}
	snapshot, err := b.call(ctx, b.snapshot, map[string]any{})
	if err != nil {
		return webresearch.BrowserPage{}, err
	}
	page := webresearch.BrowserPage{
		URL:         url,
		Text:        snapshot,
		DOMSnapshot: snapshot,
	}
	if b.screenshot != "" {
		if screenshot, err := b.call(ctx, b.screenshot, map[string]any{}); err == nil && strings.TrimSpace(screenshot) != "" {
			page.DOMSnapshot = strings.TrimSpace(page.DOMSnapshot + "\n\n" + screenshot)
		}
	}
	return page, nil
}

func (b *browserClient) call(ctx context.Context, name string, args map[string]any) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result, err := b.session.CallTool(ctx, &sdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("mcp browser %s: %w", name, err)
	}
	if result != nil && result.IsError {
		return ResultContent(result), fmt.Errorf("mcp browser %s returned an error", name)
	}
	return ResultContent(result), nil
}
