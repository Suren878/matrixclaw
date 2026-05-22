package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var webSearchClient = &http.Client{Timeout: webSearchTimeout * time.Second}

func (e *webSearchExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params WebSearchParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(webSearchToolName, err)
	}

	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		return Result{Content: "query is required", IsError: true}, nil
	}
	if params.Limit <= 0 {
		params.Limit = defaultWebSearchLimit
	}
	if params.Limit > maxWebSearchLimit {
		params.Limit = maxWebSearchLimit
	}

	var cfg WebSearchProviderConfig
	if e.config != nil {
		cfg, _ = e.config()
	}
	results, provider, err := runWebSearch(ctx, params.Query, params.Limit, cfg)
	if err != nil {
		return Result{Content: fmt.Sprintf("web search failed: %v", err), IsError: true}, nil
	}

	content := formatSearchResults(params.Query, provider, results)
	return Result{
		Content: content,
		Metadata: WebSearchResponseMetadata{
			Query:    params.Query,
			Provider: provider,
			Results:  results,
		},
	}, nil
}

func runWebSearch(ctx context.Context, query string, limit int, cfg WebSearchProviderConfig) ([]WebSearchResult, string, error) {
	switch cfg.Provider {
	case "tavily":
		if cfg.TavilyKey != "" {
			if results, err := searchTavily(ctx, query, limit, cfg.TavilyKey); err == nil {
				return results, "tavily", nil
			}
		}
	case "serper":
		if cfg.SerperKey != "" {
			if results, err := searchSerper(ctx, query, limit, cfg.SerperKey); err == nil {
				return results, "serper", nil
			}
		}
	case "searxng":
		if cfg.BaseURL != "" {
			if results, err := searchSearXNG(ctx, query, limit, cfg.BaseURL); err == nil {
				return results, "searxng", nil
			}
		}
	}
	results, err := searchDDG(ctx, query, limit)
	if err != nil {
		return nil, "", err
	}
	return results, "duckduckgo", nil
}

// searchTavily uses the Tavily Search API — designed for AI agents.
// Free tier: 1000 requests/month at app.tavily.com
// Docs: https://docs.tavily.com/docs/tavily-api/rest_api
func searchTavily(ctx context.Context, query string, limit int, apiKey string) ([]WebSearchResult, error) {
	if limit > 20 {
		limit = 20
	}
	body, _ := json.Marshal(map[string]any{
		"query":       query,
		"max_results": limit,
		"api_key":     apiKey,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := webSearchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tavily returned %d", resp.StatusCode)
	}

	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	results := make([]WebSearchResult, 0, len(out.Results))
	for i, r := range out.Results {
		results = append(results, WebSearchResult{
			Position:    i + 1,
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Content,
		})
	}
	return results, nil
}

// searchSerper uses the Serper.dev Google Search API.
// Free tier: 2500 queries/month at serper.dev
// Docs: https://serper.dev/api
func searchSerper(ctx context.Context, query string, limit int, apiKey string) ([]WebSearchResult, error) {
	if limit > 10 {
		limit = 10
	}
	body, _ := json.Marshal(map[string]any{
		"q":   query,
		"num": limit,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://google.serper.dev/search", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := webSearchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serper returned %d", resp.StatusCode)
	}

	var out struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	results := make([]WebSearchResult, 0, len(out.Organic))
	for i, r := range out.Organic {
		results = append(results, WebSearchResult{
			Position:    i + 1,
			Title:       r.Title,
			URL:         r.Link,
			Description: r.Snippet,
		})
	}
	return results, nil
}

// searchSearXNG queries a self-hosted SearXNG instance.
func searchSearXNG(ctx context.Context, query string, limit int, baseURL string) ([]WebSearchResult, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/search", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("pageno", "1")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")

	resp, err := webSearchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned %d", resp.StatusCode)
	}

	var out struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	results := make([]WebSearchResult, 0, min(len(out.Results), limit))
	for i, r := range out.Results {
		if i >= limit {
			break
		}
		results = append(results, WebSearchResult{
			Position:    i + 1,
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Content,
		})
	}
	return results, nil
}

// searchDDG scrapes DuckDuckGo HTML search (no API key required).
// Uses https://html.duckduckgo.com/html/ — a lightweight page for low-bandwidth clients.
func searchDDG(ctx context.Context, query string, limit int) ([]WebSearchResult, error) {
	formData := url.Values{}
	formData.Set("q", query)
	formData.Set("kl", "wt-wt")
	formData.Set("kp", "-1")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://html.duckduckgo.com/html/", strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := webSearchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}

	return parseDDGResults(body, limit), nil
}

// parseDDGResults extracts search results from the DuckDuckGo HTML page.
// DDG HTML structure: <div class="result"> containing <h2 class="result__title"> and <a class="result__snippet">.
func parseDDGResults(body []byte, limit int) []WebSearchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}

	var results []WebSearchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= limit {
			return
		}
		if n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "result") && !hasClass(n, "result--ad") {
			r := extractDDGResult(n)
			if r.URL != "" && r.Title != "" {
				r.Position = len(results) + 1
				results = append(results, r)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func extractDDGResult(resultNode *html.Node) WebSearchResult {
	var result WebSearchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "h2" && hasClass(n, "result__title"):
				a := findFirstElement(n, "a")
				if a != nil {
					result.Title = strings.TrimSpace(textContent(a))
					href := attrVal(a, "href")
					result.URL = decodeDDGRedirect(href)
				}
			case n.Data == "a" && hasClass(n, "result__snippet"):
				result.Description = strings.TrimSpace(textContent(n))
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(resultNode)
	return result
}

// decodeDDGRedirect extracts the actual destination URL from a DDG redirect URL.
// DDG links look like /l/?kh=-1&uddg=<url-encoded-target>
func decodeDDGRedirect(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		decoded, err := url.QueryUnescape(uddg)
		if err == nil && (strings.HasPrefix(decoded, "http://") || strings.HasPrefix(decoded, "https://")) {
			return decoded
		}
	}
	return href
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func findFirstElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func formatSearchResults(query, provider string, results []WebSearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found for %q (provider: %s)", query, provider)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<web_search query=%q provider=%q count=%d>\n", query, provider, len(results))
	for _, r := range results {
		fmt.Fprintf(&b, "\n[%d] %s\n    URL: %s\n", r.Position, r.Title, r.URL)
		if r.Description != "" {
			fmt.Fprintf(&b, "    %s\n", r.Description)
		}
	}
	b.WriteString("</web_search>")
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
