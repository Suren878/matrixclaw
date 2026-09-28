package webtools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webresearch"
)

// recordingEngine is a research engine whose fetches and browser loads are recorded.
func recordingEngine(t *testing.T) (*webresearch.Engine, *[]string, *countingBrowser) {
	t.Helper()
	dir := t.TempDir()
	store, err := webresearch.NewSQLiteStore(filepath.Join(dir, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var fetched []string
	browser := &countingBrowser{}
	engine := webresearch.NewEngine(webresearch.Config{
		Store:        store,
		ArtifactRoot: filepath.Join(dir, "artifacts"),
		Searcher: webresearch.SearchFunc(func(context.Context, string, int) (webresearch.SearchOutput, error) {
			return webresearch.SearchOutput{Results: []webresearch.SearchResult{{URL: "https://docs.example/a"}, {URL: "https://Evil.Example./b"}}}, nil
		}),
		Fetcher: webresearch.FetchFunc(func(_ context.Context, url string, _ int) (webresearch.FetchedPage, error) {
			fetched = append(fetched, url)
			return webresearch.FetchedPage{URL: url, Text: "short"}, nil
		}),
		Browser: browser,
	})
	return engine, &fetched, browser
}

func denyEvil(_ context.Context, subject permission.Subject) error {
	if subject.Value == "evil.example" {
		return errors.New("blocked by rule web_fetch: evil.example")
	}
	return nil
}

func TestWebResearchChecksEveryURLOfAGuardedCall(t *testing.T) {
	for _, args := range []string{
		`{"task":"compare","urls":["https://docs.example/a","https://evil.example/b"],"async":"true"}`,
		`{"task":"compare","query":"compare"}`,
	} {
		engine, fetched, browser := recordingEngine(t)
		executor := NewWebResearchExecutorsWithService(NewWebService(nil, engine))[0]

		result, err := executor.Execute(context.Background(), tools.Call{Args: json.RawMessage(args), Recheck: denyEvil, Guarded: true})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(*fetched, []string{"https://docs.example/a"}) || browser.fetches != 0 {
			t.Errorf("%s: fetched = %v, browser = %d", args, *fetched, browser.fetches)
		}
		if !strings.Contains(result.Content, "blocked by rule web_fetch: evil.example") {
			t.Errorf("%s: result does not say why a source was skipped: %s", args, result.Content)
		}
	}
}

func TestResearchNeedingURLChecksFetchesNothingWithoutThem(t *testing.T) {
	engine, fetched, _ := recordingEngine(t)
	if _, err := engine.Research(context.Background(), webresearch.ResearchRequest{Task: "x", URLs: []string{"https://docs.example/a"}, Async: "false", RequireURLCheck: true}); err != nil {
		t.Fatal(err)
	}
	if len(*fetched) != 0 {
		t.Fatalf("fetched = %v", *fetched)
	}
}
