package webtools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webresearch"
	"github.com/Suren878/matrixclaw/internal/work"
)

// countingBrowser is a browser fallback that cannot say where its redirects led.
type countingBrowser struct{ fetches int }

func (b *countingBrowser) Available() bool   { return true }
func (b *countingBrowser) SetupHint() string { return "" }
func (b *countingBrowser) Fetch(_ context.Context, url string) (webresearch.BrowserPage, error) {
	b.fetches++
	return webresearch.BrowserPage{URL: url, Text: "rendered"}, nil
}

func browserFetchExecutor(t *testing.T, browser webresearch.Browser, fetch webresearch.FetchFunc) tools.Executor {
	t.Helper()
	dir := t.TempDir()
	workStore, err := work.NewSQLiteStore(filepath.Join(dir, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workStore.Close() })
	store := webresearch.NewStore(workStore)
	engine := webresearch.NewEngine(webresearch.Config{
		Store:        store,
		ArtifactRoot: filepath.Join(dir, "artifacts"),
		Fetcher:      fetch,
		Browser:      browser,
	})
	return NewWebFetchExecutorWithService(NewWebService(nil, engine))
}

func TestWebFetchSkipsTheBrowserFallbackWhenRulesGuardDomains(t *testing.T) {
	short := webresearch.FetchFunc(func(_ context.Context, url string, _ int) (webresearch.FetchedPage, error) {
		return webresearch.FetchedPage{URL: url, Text: "short"}, nil
	})
	for _, guarded := range []bool{false, true} {
		browser := &countingBrowser{}
		executor := browserFetchExecutor(t, browser, short)
		if _, err := executor.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"url":"https://93.184.215.14/"}`), Guarded: guarded}); err != nil {
			t.Fatal(err)
		}
		if (browser.fetches == 0) != guarded {
			t.Errorf("guarded=%v: browser fetches = %d", guarded, browser.fetches)
		}
	}
}

func TestWebFetchBrowserFallbackRefusesPrivateTargets(t *testing.T) {
	refused := webresearch.FetchFunc(func(_ context.Context, url string, _ int) (webresearch.FetchedPage, error) {
		return webresearch.FetchedPage{URL: url}, errors.New("private address")
	})
	for _, target := range []string{"http://127.0.0.1:8080/admin", "http://169.254.169.254/latest/meta-data/"} {
		browser := &countingBrowser{}
		executor := browserFetchExecutor(t, browser, refused)
		if _, err := executor.Execute(context.Background(), tools.Call{Args: json.RawMessage(`{"url":"` + target + `"}`)}); err != nil {
			t.Fatal(err)
		}
		if browser.fetches != 0 {
			t.Errorf("%s: browser fetched a private target", target)
		}
	}
}
