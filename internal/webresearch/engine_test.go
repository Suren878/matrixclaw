package webresearch

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResearchStoresSourcesArtifactsFactsAndAskReusesFacts(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()
	result, err := engine.Research(ctx, ResearchRequest{
		Task:       "Find MatrixClaw web research details",
		Query:      "matrixclaw web research",
		MaxSources: 1,
		Async:      "false",
	})
	if err != nil {
		t.Fatalf("Research() error = %v", err)
	}
	if result.ResearchID == "" {
		t.Fatal("ResearchID is empty")
	}
	if result.Status != StatusCompleted {
		t.Fatalf("status = %q, want %q", result.Status, StatusCompleted)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(result.Sources))
	}
	if len(result.Sources[0].ArtifactIDs) == 0 {
		t.Fatal("expected source artifact ids")
	}
	if len(result.Facts) == 0 {
		t.Fatal("expected extracted facts")
	}
	artifacts, err := engine.store.ListArtifacts(ctx, result.ResearchID)
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(artifacts) == 0 {
		t.Fatal("expected stored artifacts")
	}

	fetchesBefore := engine.fetcher.(*countingFetcher).calls
	asked, err := engine.Ask(ctx, AskRequest{
		ResearchID: result.ResearchID,
		Question:   "What did MatrixClaw web research details say?",
		Freshness:  "cache",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if asked.ResearchID != result.ResearchID {
		t.Fatalf("ask research id = %q, want %q", asked.ResearchID, result.ResearchID)
	}
	if !strings.Contains(asked.Summary, "without fetching") {
		t.Fatalf("ask summary did not report cache reuse: %q", asked.Summary)
	}
	if fetchesAfter := engine.fetcher.(*countingFetcher).calls; fetchesAfter != fetchesBefore {
		t.Fatalf("fetch calls changed from %d to %d; ask should reuse stored facts", fetchesBefore, fetchesAfter)
	}
}

func TestAskRefreshFetchesWhenStoredFactMissing(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()
	result, err := engine.Research(ctx, ResearchRequest{Task: "MatrixClaw alpha", Query: "matrixclaw alpha", MaxSources: 1, Async: "false"})
	if err != nil {
		t.Fatalf("Research() error = %v", err)
	}
	fetchesBefore := engine.fetcher.(*countingFetcher).calls
	asked, err := engine.Ask(ctx, AskRequest{ResearchID: result.ResearchID, Question: "unrelated beta question", Freshness: "refresh"})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if asked.Status != StatusCompleted {
		t.Fatalf("status = %q, want completed", asked.Status)
	}
	if fetchesAfter := engine.fetcher.(*countingFetcher).calls; fetchesAfter <= fetchesBefore {
		t.Fatalf("fetch calls = %d, want > %d", fetchesAfter, fetchesBefore)
	}
}

func TestBrowserUnavailableReturnsSetupHint(t *testing.T) {
	engine := newTestEngine(t)
	engine.fetcher = FetchFunc(func(context.Context, string, int) (FetchedPage, error) {
		return FetchedPage{URL: "https://example.com", Text: ""}, nil
	})
	engine.browser = unavailableBrowserForTest{hint: "enable browser test hint"}
	result, err := engine.Research(context.Background(), ResearchRequest{
		Task:    "visual task",
		URLs:    []string{"https://example.com"},
		Browser: "always",
		Async:   "false",
	})
	if err != nil {
		t.Fatalf("Research() error = %v", err)
	}
	if !contains(result.Warnings, "enable browser test hint") {
		t.Fatalf("warnings = %#v, want setup hint", result.Warnings)
	}
}

func TestResearchAutoAsyncReturnsRunningThenCompletes(t *testing.T) {
	engine := newTestEngine(t)
	engine.syncTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	engine.fetcher = FetchFunc(func(_ context.Context, url string, _ int) (FetchedPage, error) {
		<-release
		return FetchedPage{
			URL:         url,
			Title:       "Slow source",
			Text:        "Slow source eventually produced enough research text for a compact fact.",
			StatusCode:  200,
			ContentType: "text/plain",
		}, nil
	})
	result, err := engine.Research(context.Background(), ResearchRequest{
		Task:       "slow research",
		Query:      "slow research",
		MaxSources: 1,
		Async:      "auto",
	})
	if err != nil {
		t.Fatalf("Research() error = %v", err)
	}
	if result.ResearchID == "" {
		t.Fatal("ResearchID is empty")
	}
	if result.Status != StatusPending && result.Status != StatusRunning {
		t.Fatalf("initial status = %q, want pending/running", result.Status)
	}
	close(release)
	var final ResearchResult
	for i := 0; i < 50; i++ {
		final, err = engine.Status(context.Background(), StatusRequest{ResearchID: result.ResearchID})
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		if final.Status == StatusCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("final status = %q, want completed", final.Status)
}

func TestRequeueStaleJobsFailsAfterRetryLimit(t *testing.T) {
	engine := newTestEngine(t)
	engine.retryLimit = 2
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }
	ctx := context.Background()
	session := ResearchSession{ID: "wr_stale", Task: "stale", Status: StatusRunning, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute)}
	if err := engine.store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	heartbeat := now.Add(-2 * time.Minute)
	job := ResearchJob{
		ID:          "wrjob_stale",
		ResearchID:  session.ID,
		Kind:        "research",
		Status:      StatusRunning,
		InputJSON:   `{"task":"stale"}`,
		Attempts:    2,
		HeartbeatAt: &heartbeat,
		CreatedAt:   heartbeat,
		UpdatedAt:   heartbeat,
	}
	if err := engine.store.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	if err := engine.RequeueStaleJobs(ctx); err != nil {
		t.Fatalf("RequeueStaleJobs() error = %v", err)
	}
	updated, err := engine.store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if updated.Status != StatusFailed {
		t.Fatalf("job status = %q, want failed", updated.Status)
	}
	status, err := engine.Status(ctx, StatusRequest{ResearchID: session.ID})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Status != StatusFailed {
		t.Fatalf("session status = %q, want failed", status.Status)
	}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fetcher := &countingFetcher{}
	return NewEngine(Config{
		Store:        store,
		ArtifactRoot: filepath.Join(t.TempDir(), "artifacts"),
		Searcher: SearchFunc(func(_ context.Context, query string, limit int) (SearchOutput, error) {
			if limit <= 0 {
				return SearchOutput{}, errors.New("limit must be positive")
			}
			return SearchOutput{
				Provider: "test",
				Results: []SearchResult{{
					Position:    1,
					Title:       "MatrixClaw research result",
					URL:         "https://example.com/matrixclaw",
					Description: "MatrixClaw web research stores compact facts and sources.",
				}},
			}, nil
		}),
		Fetcher: fetcher,
		Now:     func() time.Time { return time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC) },
	})
}

type countingFetcher struct {
	calls int
}

func (f *countingFetcher) Fetch(_ context.Context, url string, _ int) (FetchedPage, error) {
	f.calls++
	return FetchedPage{
		URL:         url,
		Title:       "Fetched MatrixClaw page",
		Text:        "MatrixClaw web research stores compact facts, sources, and browser artifacts separately.",
		HTML:        "<html><title>Fetched MatrixClaw page</title><body>MatrixClaw web research stores compact facts.</body></html>",
		StatusCode:  200,
		ContentType: "text/html",
	}, nil
}

type unavailableBrowserForTest struct {
	hint string
}

func (b unavailableBrowserForTest) Available() bool   { return false }
func (b unavailableBrowserForTest) SetupHint() string { return b.hint }
func (b unavailableBrowserForTest) Fetch(context.Context, string) (BrowserPage, error) {
	return BrowserPage{}, errors.New(b.hint)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
