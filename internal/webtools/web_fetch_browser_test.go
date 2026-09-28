package webtools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webresearch"
)

// countingBrowser is a browser fallback that cannot say where its redirects led.
type countingBrowser struct{ fetches int }

func (b *countingBrowser) Available() bool   { return true }
func (b *countingBrowser) SetupHint() string { return "" }
func (b *countingBrowser) Fetch(_ context.Context, url string) (webresearch.BrowserPage, error) {
	b.fetches++
	return webresearch.BrowserPage{URL: url, Text: "rendered"}, nil
}

func TestWebFetchSkipsTheBrowserFallbackWhenRulesGuardDomains(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		dir := t.TempDir()
		store, err := webresearch.NewSQLiteStore(filepath.Join(dir, "work.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		browser := &countingBrowser{}
		engine := webresearch.NewEngine(webresearch.Config{
			Store:        store,
			ArtifactRoot: filepath.Join(dir, "artifacts"),
			Fetcher: webresearch.FetchFunc(func(_ context.Context, url string, _ int) (webresearch.FetchedPage, error) {
				return webresearch.FetchedPage{URL: url, Text: "short"}, nil
			}),
			Browser: browser,
		})
		executor := NewWebFetchExecutorWithService(NewWebService(nil, engine))

		if _, err := executor.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"url":"https://docs.example/"}`), Guarded: guarded}); err != nil {
			t.Fatal(err)
		}
		if (browser.fetches == 0) != guarded {
			t.Errorf("guarded=%v: browser fetches = %d", guarded, browser.fetches)
		}
	}
}
