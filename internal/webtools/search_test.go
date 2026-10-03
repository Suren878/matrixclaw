package webtools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/tools"
)

type searchTransport map[string]*http.Response

func (s searchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, ok := s[req.URL.Host]
	if !ok {
		resp = &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}
	}
	resp.Request = req
	return resp, nil
}

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func runSearch(t *testing.T, cfg SearchConfig, responses searchTransport) tools.Result {
	t.Helper()
	previous := webSearchClient
	webSearchClient = &http.Client{Transport: responses}
	t.Cleanup(func() { webSearchClient = previous })
	args, _ := json.Marshal(WebSearchParams{Query: "matrixclaw"})
	result, err := NewSearchTool(func() (SearchConfig, error) { return cfg, nil }).Execute(context.Background(), tools.Call{Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

const ddgPage = `<html><body><div class="result"><h2 class="result__title"><a href="https://example.com/">Example</a></h2></div></body></html>`

func TestWebSearchSaysWhenTheConfiguredProviderFailed(t *testing.T) {
	result := runSearch(t, SearchConfig{Provider: "tavily", TavilyKey: "bad"}, searchTransport{
		"api.tavily.com":      reply(http.StatusUnauthorized, `{"detail":"invalid key"}`),
		"html.duckduckgo.com": reply(http.StatusOK, ddgPage),
	})
	if result.IsError() || !strings.Contains(result.Content, "tavily returned 401") || !strings.Contains(result.Content, "https://example.com/") {
		t.Fatalf("result = %+v, want DuckDuckGo results and the tavily failure", result)
	}
}

func TestWebSearchReportsDuckDuckGoRefusals(t *testing.T) {
	for _, resp := range []*http.Response{
		reply(http.StatusAccepted, `<html><body><div class="anomaly-modal">Select all squares</div></body></html>`),
		reply(http.StatusOK, `<html><body><form id="challenge-form"></form></body></html>`),
	} {
		result := runSearch(t, SearchConfig{}, searchTransport{"html.duckduckgo.com": resp})
		if !result.IsError() || strings.Contains(result.Content, "no results") {
			t.Fatalf("result = %+v, want a failure, not an empty search", result)
		}
	}
}
