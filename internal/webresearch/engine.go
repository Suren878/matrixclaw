package webresearch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

type Config struct {
	Store          *WorkStore
	ArtifactRoot   string
	Searcher       Searcher
	Fetcher        Fetcher
	Browser        Browser
	Summarizer     Summarizer
	SyncTimeout    time.Duration
	HeartbeatEvery time.Duration
	StaleAfter     time.Duration
	RetryLimit     int
	RetentionDays  int
	Now            func() time.Time
}

type Engine struct {
	store          *WorkStore
	artifacts      *ArtifactStore
	searcher       Searcher
	fetcher        Fetcher
	browser        Browser
	summarizer     Summarizer
	syncTimeout    time.Duration
	heartbeatEvery time.Duration
	staleAfter     time.Duration
	retryLimit     int
	retentionDays  int
	now            func() time.Time
	mu             sync.Mutex
	active         map[string]struct{}
}

func NewEngine(cfg Config) *Engine {
	syncTimeout := cfg.SyncTimeout
	if syncTimeout <= 0 {
		syncTimeout = DefaultSyncTimeout
	}
	heartbeatEvery := cfg.HeartbeatEvery
	if heartbeatEvery <= 0 {
		heartbeatEvery = DefaultHeartbeatEvery
	}
	staleAfter := cfg.StaleAfter
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}
	retryLimit := cfg.RetryLimit
	if retryLimit <= 0 {
		retryLimit = DefaultRetryLimit
	}
	retentionDays := cfg.RetentionDays
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	summarizer := cfg.Summarizer
	if summarizer == nil {
		summarizer = NewDeterministicSummarizer()
	}
	return &Engine{
		store:          cfg.Store,
		artifacts:      NewArtifactStore(cfg.ArtifactRoot),
		searcher:       cfg.Searcher,
		fetcher:        cfg.Fetcher,
		browser:        cfg.Browser,
		summarizer:     summarizer,
		syncTimeout:    syncTimeout,
		heartbeatEvery: heartbeatEvery,
		staleAfter:     staleAfter,
		retryLimit:     retryLimit,
		retentionDays:  retentionDays,
		now:            now,
		active:         map[string]struct{}{},
	}
}

func (e *Engine) Research(ctx context.Context, request ResearchRequest) (ResearchResult, error) {
	if err := e.ready(); err != nil {
		return ResearchResult{}, err
	}
	request = normalizeResearchRequest(request)
	if request.Task == "" && request.Query == "" && len(request.URLs) == 0 {
		return ResearchResult{}, fmt.Errorf("task, query, or urls is required")
	}
	now := e.now().UTC()
	session := ResearchSession{
		ID:        newID("wr"),
		Task:      request.Task,
		Query:     firstNonEmpty(request.Query, request.Task),
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
		NextActions: []string{
			"Use web_research_ask with this research_id for follow-up questions.",
		},
	}
	if err := e.store.CreateSession(ctx, session); err != nil {
		return ResearchResult{}, err
	}
	job, err := e.createJob(ctx, session.ID, "research", request)
	if err != nil {
		return ResearchResult{}, err
	}

	switch normalizeAsyncMode(request.Async) {
	case "true":
		e.startJob(context.Background(), job.ID)
		return e.Status(ctx, StatusRequest{ResearchID: session.ID})
	case "false":
		if err := e.runJob(ctx, job.ID); err != nil {
			return e.Status(ctx, StatusRequest{ResearchID: session.ID})
		}
		return e.Status(ctx, StatusRequest{ResearchID: session.ID})
	default:
		done := make(chan struct{})
		e.startJobWithDone(context.Background(), job.ID, done)
		select {
		case <-done:
			return e.Status(ctx, StatusRequest{ResearchID: session.ID})
		case <-ctx.Done():
			return e.Status(context.Background(), StatusRequest{ResearchID: session.ID})
		case <-time.After(e.syncTimeout):
			return e.Status(ctx, StatusRequest{ResearchID: session.ID})
		}
	}
}

func (e *Engine) Ask(ctx context.Context, request AskRequest) (ResearchResult, error) {
	if err := e.ready(); err != nil {
		return ResearchResult{}, err
	}
	request.ResearchID = strings.TrimSpace(request.ResearchID)
	request.Question = strings.TrimSpace(request.Question)
	if request.ResearchID == "" {
		return ResearchResult{}, fmt.Errorf("research_id is required")
	}
	if request.Question == "" {
		return ResearchResult{}, fmt.Errorf("question is required")
	}
	session, err := e.store.GetSession(ctx, request.ResearchID)
	if err != nil {
		return ResearchResult{}, err
	}
	facts, err := e.store.ListFacts(ctx, request.ResearchID)
	if err != nil {
		return ResearchResult{}, err
	}
	sources, err := e.store.ListSources(ctx, request.ResearchID)
	if err != nil {
		return ResearchResult{}, err
	}
	if !freshnessRequiresRefresh(request.Question, request.Freshness, sources, e.now()) {
		if matched := matchFacts(request.Question, facts); len(matched) > 0 {
			out := ResearchResult{
				ResearchID: request.ResearchID,
				Status:     session.Status,
				Answer:     answerFromFacts(request.Question, matched),
				Summary:    "Answered from stored research facts without fetching new pages.",
				Facts:      boundedFacts(matched),
				Sources:    boundedSources(sourcesForFacts(sources, matched)),
				NextActions: []string{
					"Ask another follow-up with web_research_ask, or rerun web_research with freshness=refresh for updated data.",
				},
			}
			return out, nil
		}
	}

	researchRequest := ResearchRequest{
		Task:       "Follow-up: " + request.Question,
		Query:      request.Question,
		MaxSources: max(3, min(DefaultMaxSources, len(sources))),
		Browser:    request.Browser,
		Freshness:  firstNonEmpty(request.Freshness, "auto"),
		Async:      "false",
	}
	job, err := e.createJob(ctx, request.ResearchID, "followup", researchRequest)
	if err != nil {
		return ResearchResult{}, err
	}
	if err := e.runJob(ctx, job.ID); err != nil {
		return e.Status(ctx, StatusRequest{ResearchID: request.ResearchID})
	}
	return e.Status(ctx, StatusRequest{ResearchID: request.ResearchID})
}

func (e *Engine) Status(ctx context.Context, request StatusRequest) (ResearchResult, error) {
	if err := e.ready(); err != nil {
		return ResearchResult{}, err
	}
	researchID := strings.TrimSpace(request.ResearchID)
	if researchID == "" {
		return ResearchResult{}, fmt.Errorf("research_id is required")
	}
	session, err := e.store.GetSession(ctx, researchID)
	if err != nil {
		return ResearchResult{}, err
	}
	facts, err := e.store.ListFacts(ctx, researchID)
	if err != nil {
		return ResearchResult{}, err
	}
	sources, err := e.store.ListSources(ctx, researchID)
	if err != nil {
		return ResearchResult{}, err
	}
	warnings := append([]string(nil), session.Warnings...)
	if job, err := e.store.LatestJob(ctx, researchID); err == nil && job.Error != "" && session.Status == StatusFailed {
		warnings = appendUnique(warnings, job.Error)
	}
	nextActions := append([]string(nil), session.NextActions...)
	if session.Status == StatusRunning || session.Status == StatusPending {
		nextActions = appendUnique(nextActions, "Poll web_research_status with this research_id.")
	}
	return ResearchResult{
		ResearchID:  session.ID,
		Status:      session.Status,
		Answer:      session.Answer,
		Summary:     session.Summary,
		Facts:       boundedFacts(facts),
		Sources:     boundedSources(sources),
		Warnings:    boundedStrings(warnings, 8),
		NextActions: boundedStrings(nextActions, 6),
	}, nil
}

func (e *Engine) Start(ctx context.Context) {
	if e == nil || e.store == nil {
		return
	}
	ticker := time.NewTicker(e.heartbeatEvery)
	defer ticker.Stop()
	for {
		_ = e.CleanupExpired(ctx)
		e.dispatchPending(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.requeueStale(ctx); err != nil {
				log.Printf("webresearch requeue stale jobs failed: %v", err)
			}
		}
	}
}

func (e *Engine) CleanupExpired(ctx context.Context) error {
	if err := e.ready(); err != nil {
		return err
	}
	if e.retentionDays <= 0 {
		return nil
	}
	before := e.now().Add(-time.Duration(e.retentionDays) * 24 * time.Hour)
	artifacts, err := e.store.ListArtifactsForExpiredSessions(ctx, before)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Path) != "" {
			_ = os.Remove(artifact.Path)
		}
	}
	return e.store.DeleteExpiredSessions(ctx, before)
}

func (e *Engine) RequeueStaleJobs(ctx context.Context) error {
	if err := e.ready(); err != nil {
		return err
	}
	return e.requeueStale(ctx)
}

func (e *Engine) ready() error {
	if e == nil {
		return fmt.Errorf("webresearch: engine is not configured")
	}
	if e.store == nil {
		return fmt.Errorf("webresearch: store is not configured")
	}
	if e.searcher == nil && e.fetcher == nil {
		return fmt.Errorf("webresearch: searcher or fetcher is required")
	}
	return nil
}

func (e *Engine) createJob(ctx context.Context, researchID string, kind string, input any) (ResearchJob, error) {
	raw, _ := json.Marshal(input)
	now := e.now().UTC()
	job := ResearchJob{
		ID:         newID("wrjob"),
		ResearchID: researchID,
		Kind:       firstNonEmpty(kind, "research"),
		Status:     StatusPending,
		InputJSON:  string(raw),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := e.store.CreateJob(ctx, job); err != nil {
		return ResearchJob{}, err
	}
	return job, nil
}

func (e *Engine) startJob(ctx context.Context, jobID string) {
	e.startJobWithDone(ctx, jobID, nil)
}

func (e *Engine) startJobWithDone(ctx context.Context, jobID string, done chan<- struct{}) {
	if !e.markActive(jobID) {
		if done != nil {
			close(done)
		}
		return
	}
	safego.Go("webresearch.job", func() {
		defer e.clearActive(jobID)
		if done != nil {
			defer close(done)
		}
		_ = e.runJob(ctx, jobID)
	})
}

func (e *Engine) markActive(jobID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.active[jobID]; ok {
		return false
	}
	e.active[jobID] = struct{}{}
	return true
}

func (e *Engine) clearActive(jobID string) {
	e.mu.Lock()
	delete(e.active, jobID)
	e.mu.Unlock()
}

func (e *Engine) dispatchPending(ctx context.Context) {
	jobs, err := e.store.ListRunnableJobs(ctx, 4)
	if err != nil {
		return
	}
	for _, job := range jobs {
		e.startJob(context.Background(), job.ID)
	}
}

func (e *Engine) requeueStale(ctx context.Context) error {
	jobs, err := e.store.ListStaleJobs(ctx, e.now().Add(-e.staleAfter), e.retryLimit)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		now := e.now().UTC()
		if job.Attempts >= e.retryLimit {
			job.Status = StatusFailed
			job.Error = "research job exceeded retry limit after stale heartbeat"
			job.UpdatedAt = now
			job.FinishedAt = &now
			_ = e.store.UpdateJob(ctx, job)
			if session, err := e.store.GetSession(ctx, job.ResearchID); err == nil {
				session.Status = StatusFailed
				session.Warnings = appendUnique(session.Warnings, job.Error)
				session.UpdatedAt = now
				session.CompletedAt = &now
				_ = e.store.UpdateSession(ctx, session)
			}
			continue
		}
		job.Status = StatusPending
		job.Attempts++
		job.UpdatedAt = now
		job.HeartbeatAt = nil
		_ = e.store.UpdateJob(ctx, job)
	}
	return nil
}

func (e *Engine) runJob(ctx context.Context, jobID string) error {
	job, err := e.store.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	var request ResearchRequest
	if err := json.Unmarshal([]byte(job.InputJSON), &request); err != nil {
		return e.failJob(ctx, job, fmt.Errorf("decode job input: %w", err))
	}
	request = normalizeResearchRequest(request)
	now := e.now().UTC()
	job.Status = StatusRunning
	job.Attempts++
	job.HeartbeatAt = &now
	job.UpdatedAt = now
	if err := e.store.UpdateJob(ctx, job); err != nil {
		return err
	}
	session, err := e.store.GetSession(ctx, job.ResearchID)
	if err != nil {
		return e.failJob(ctx, job, err)
	}
	session.Status = StatusRunning
	session.UpdatedAt = now
	if err := e.store.UpdateSession(ctx, session); err != nil {
		return e.failJob(ctx, job, err)
	}
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	safego.Go("webresearch.heartbeat", func() { e.heartbeat(heartbeatCtx, job.ID) })

	result, err := e.executeResearch(ctx, session, request)
	if err != nil {
		return e.failJob(ctx, job, err)
	}
	now = e.now().UTC()
	session.Status = StatusCompleted
	session.Answer = result.Answer
	session.Summary = result.Summary
	session.Warnings = result.Warnings
	session.NextActions = result.NextActions
	session.UpdatedAt = now
	session.CompletedAt = &now
	if err := e.store.UpdateSession(ctx, session); err != nil {
		return e.failJob(ctx, job, err)
	}
	job.Status = StatusCompleted
	job.Error = ""
	job.UpdatedAt = now
	job.FinishedAt = &now
	job.HeartbeatAt = &now
	return e.store.UpdateJob(ctx, job)
}

func (e *Engine) heartbeat(ctx context.Context, jobID string) {
	ticker := time.NewTicker(e.heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := e.store.GetJob(ctx, jobID)
			if err != nil || job.Status != StatusRunning {
				return
			}
			now := e.now().UTC()
			job.HeartbeatAt = &now
			job.UpdatedAt = now
			_ = e.store.UpdateJob(ctx, job)
		}
	}
}

func (e *Engine) failJob(ctx context.Context, job ResearchJob, err error) error {
	if err == nil {
		err = errors.New("unknown research failure")
	}
	now := e.now().UTC()
	job.Status = StatusFailed
	job.Error = err.Error()
	job.UpdatedAt = now
	job.FinishedAt = &now
	_ = e.store.UpdateJob(ctx, job)
	if session, getErr := e.store.GetSession(ctx, job.ResearchID); getErr == nil {
		session.Status = StatusFailed
		session.Warnings = appendUnique(session.Warnings, err.Error())
		session.NextActions = appendUnique(session.NextActions, "Try web_research again with narrower sources or a different query.")
		session.UpdatedAt = now
		session.CompletedAt = &now
		_ = e.store.UpdateSession(ctx, session)
	}
	return err
}

func (e *Engine) executeResearch(ctx context.Context, session ResearchSession, request ResearchRequest) (ResearchResult, error) {
	maxSources := request.MaxSources
	if maxSources <= 0 {
		maxSources = DefaultMaxSources
	}
	maxSources = min(maxSources, MaxSourcesLimit)

	var warnings []string
	var documents []SourceDocument
	sourceByURL := map[string]Source{}
	orderedURLs := dedupeStrings(request.URLs)
	if len(orderedURLs) == 0 && e.searcher != nil {
		search, err := e.searcher.Search(ctx, firstNonEmpty(request.Query, request.Task), maxSources)
		if err != nil {
			warnings = appendUnique(warnings, "search failed: "+err.Error())
		} else {
			for _, result := range search.Results {
				if strings.TrimSpace(result.URL) == "" {
					continue
				}
				source := Source{
					ID:         newID("wrsrc"),
					ResearchID: session.ID,
					URL:        strings.TrimSpace(result.URL),
					Title:      strings.TrimSpace(result.Title),
					Snippet:    strings.TrimSpace(result.Description),
					CreatedAt:  e.now().UTC(),
					UpdatedAt:  e.now().UTC(),
				}
				sourceByURL[source.URL] = source
				orderedURLs = append(orderedURLs, source.URL)
				if len(orderedURLs) >= maxSources {
					break
				}
			}
		}
	}
	if len(orderedURLs) == 0 {
		return ResearchResult{
			ResearchID:  session.ID,
			Status:      StatusCompleted,
			Answer:      "No web sources were available for this research request.",
			Summary:     "No sources.",
			Warnings:    appendUnique(warnings, "no sources found"),
			NextActions: []string{"Try web_research again with a more specific query or explicit urls."},
		}, nil
	}
	for _, rawURL := range orderedURLs {
		if len(documents) >= maxSources {
			break
		}
		source := sourceByURL[rawURL]
		if source.ID == "" {
			source = Source{
				ID:         newID("wrsrc"),
				ResearchID: session.ID,
				URL:        strings.TrimSpace(rawURL),
				CreatedAt:  e.now().UTC(),
				UpdatedAt:  e.now().UTC(),
			}
		}
		text, sourceWarnings := e.readSource(ctx, session.ID, &source, request)
		warnings = appendUnique(warnings, sourceWarnings...)
		if err := e.store.UpsertSource(ctx, source); err != nil {
			warnings = appendUnique(warnings, err.Error())
			continue
		}
		documents = append(documents, SourceDocument{Source: source, Text: text})
	}
	summary, err := e.summarizer.Summarize(ctx, SummaryInput{
		Task:    request.Task,
		Query:   request.Query,
		Sources: documents,
	})
	if err != nil {
		return ResearchResult{}, fmt.Errorf("summarize research: %w", err)
	}
	for i := range summary.Facts {
		if strings.TrimSpace(summary.Facts[i].ID) == "" {
			summary.Facts[i].ID = newID("wrfact")
		}
		summary.Facts[i].ResearchID = session.ID
		if summary.Facts[i].CreatedAt.IsZero() {
			summary.Facts[i].CreatedAt = e.now().UTC()
		}
	}
	if err := e.store.ReplaceFacts(ctx, session.ID, summary.Facts); err != nil {
		return ResearchResult{}, err
	}
	sources, err := e.store.ListSources(ctx, session.ID)
	if err != nil {
		return ResearchResult{}, err
	}
	nextActions := []string{"Use web_research_ask with this research_id for follow-up questions."}
	if len(warnings) > 0 {
		nextActions = append(nextActions, "Review warnings before treating the answer as complete.")
	}
	return ResearchResult{
		ResearchID:  session.ID,
		Status:      StatusCompleted,
		Answer:      boundText(summary.Answer, 2200),
		Summary:     boundText(summary.Summary, 900),
		Facts:       boundedFacts(summary.Facts),
		Sources:     boundedSources(sources),
		Warnings:    boundedStrings(warnings, 8),
		NextActions: nextActions,
	}, nil
}

func (e *Engine) readSource(ctx context.Context, researchID string, source *Source, request ResearchRequest) (string, []string) {
	var warnings []string
	var text string
	if e.fetcher != nil {
		page, err := e.fetcher.Fetch(ctx, source.URL, 80_000)
		if err != nil {
			warnings = appendUnique(warnings, "fetch failed for "+source.URL+": "+err.Error())
		} else {
			if strings.TrimSpace(page.Title) != "" {
				source.Title = strings.TrimSpace(page.Title)
			}
			source.StatusCode = page.StatusCode
			source.ContentType = page.ContentType
			source.FetchedAt = e.now().UTC()
			source.UpdatedAt = e.now().UTC()
			text = page.Text
			if page.Text != "" {
				artifactID := newID("wrart")
				path, n, err := e.artifacts.Write(researchID, artifactID, "text", []byte(page.Text))
				if err == nil {
					source.ArtifactIDs = append(source.ArtifactIDs, artifactID)
					_ = e.store.CreateArtifact(ctx, Artifact{ID: artifactID, ResearchID: researchID, SourceID: source.ID, Kind: "text", Path: path, MIMEType: "text/plain", ByteCount: n, CreatedAt: e.now().UTC()})
				}
			}
			if page.HTML != "" {
				artifactID := newID("wrart")
				path, n, err := e.artifacts.Write(researchID, artifactID, "html", []byte(page.HTML))
				if err == nil {
					source.ArtifactIDs = append(source.ArtifactIDs, artifactID)
					_ = e.store.CreateArtifact(ctx, Artifact{ID: artifactID, ResearchID: researchID, SourceID: source.ID, Kind: "html", Path: path, MIMEType: "text/html", ByteCount: n, CreatedAt: e.now().UTC()})
				}
			}
		}
	}
	if shouldUseBrowser(request.Browser, text) {
		if e.browser == nil || !e.browser.Available() {
			warnings = appendUnique(warnings, browserSetupHint(e.browser))
			return text, warnings
		}
		page, err := e.browser.Fetch(ctx, source.URL)
		if err != nil {
			warnings = appendUnique(warnings, "browser fallback failed for "+source.URL+": "+err.Error())
			return text, warnings
		}
		if strings.TrimSpace(page.Title) != "" {
			source.Title = strings.TrimSpace(page.Title)
		}
		if strings.TrimSpace(page.Text) != "" {
			text = page.Text
			artifactID := newID("wrart")
			path, n, err := e.artifacts.Write(researchID, artifactID, "text", []byte(page.Text))
			if err == nil {
				source.ArtifactIDs = append(source.ArtifactIDs, artifactID)
				_ = e.store.CreateArtifact(ctx, Artifact{ID: artifactID, ResearchID: researchID, SourceID: source.ID, Kind: "browser_text", Path: path, MIMEType: "text/plain", ByteCount: n, CreatedAt: e.now().UTC()})
			}
		}
		if page.DOMSnapshot != "" {
			artifactID := newID("wrart")
			path, n, err := e.artifacts.Write(researchID, artifactID, "dom", []byte(page.DOMSnapshot))
			if err == nil {
				source.ArtifactIDs = append(source.ArtifactIDs, artifactID)
				_ = e.store.CreateArtifact(ctx, Artifact{ID: artifactID, ResearchID: researchID, SourceID: source.ID, Kind: "dom", Path: path, MIMEType: "text/plain", ByteCount: n, CreatedAt: e.now().UTC()})
			}
		}
		if len(page.Screenshot) > 0 {
			artifactID := newID("wrart")
			path, n, err := e.artifacts.Write(researchID, artifactID, "screenshot", page.Screenshot)
			if err == nil {
				source.ArtifactIDs = append(source.ArtifactIDs, artifactID)
				_ = e.store.CreateArtifact(ctx, Artifact{ID: artifactID, ResearchID: researchID, SourceID: source.ID, Kind: "screenshot", Path: path, MIMEType: firstNonEmpty(page.ScreenshotMT, "image/png"), ByteCount: n, CreatedAt: e.now().UTC()})
			}
		}
	}
	if strings.TrimSpace(source.Snippet) == "" {
		source.Snippet = boundText(text, 280)
	}
	return text, warnings
}

func browserSetupHint(browser Browser) string {
	if browser != nil {
		if hint := strings.TrimSpace(browser.SetupHint()); hint != "" {
			return hint
		}
	}
	return "browser fallback is unavailable; enable an MCP browser server in /modules mcp (for example id=browser with browser automation tools) and restart the daemon."
}

func shouldUseBrowser(mode string, fetchedText string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "true", "required", "yes":
		return true
	case "never", "false", "off", "no":
		return false
	default:
		text := strings.TrimSpace(fetchedText)
		return len(text) < 300
	}
}

func normalizeResearchRequest(request ResearchRequest) ResearchRequest {
	request.Task = strings.Join(strings.Fields(request.Task), " ")
	request.Query = strings.Join(strings.Fields(request.Query), " ")
	request.Depth = strings.ToLower(strings.TrimSpace(request.Depth))
	request.Browser = strings.ToLower(strings.TrimSpace(request.Browser))
	request.Freshness = strings.ToLower(strings.TrimSpace(request.Freshness))
	request.Async = strings.ToLower(strings.TrimSpace(request.Async))
	if request.MaxSources <= 0 {
		request.MaxSources = DefaultMaxSources
	}
	request.MaxSources = min(request.MaxSources, MaxSourcesLimit)
	request.URLs = dedupeStrings(request.URLs)
	return request
}

func normalizeAsyncMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "always", "background":
		return "true"
	case "false", "no", "never", "sync":
		return "false"
	default:
		return "auto"
	}
}

func freshnessRequiresRefresh(question string, freshness string, sources []Source, now time.Time) bool {
	switch strings.ToLower(strings.TrimSpace(freshness)) {
	case "refresh", "always", "true":
		return true
	case "cache", "cached", "never", "false":
		return false
	}
	if len(sources) == 0 {
		return true
	}
	maxAge := 24 * time.Hour
	if looksVolatile(question) {
		maxAge = time.Hour
	}
	for _, source := range sources {
		if source.FetchedAt.IsZero() || now.Sub(source.FetchedAt) > maxAge {
			return true
		}
	}
	return false
}

func looksVolatile(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{"latest", "today", "current", "price", "rating", "review", "availability", "stock", "score", "schedule", "now"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func matchFacts(question string, facts []Fact) []Fact {
	terms := significantTerms(question)
	if len(terms) == 0 {
		return nil
	}
	var out []Fact
	for _, fact := range facts {
		claim := strings.ToLower(fact.Claim)
		matches := 0
		for _, term := range terms {
			if strings.Contains(claim, term) {
				matches++
			}
		}
		if matches >= min(2, len(terms)) {
			out = append(out, fact)
		}
	}
	if len(out) == 0 && len(facts) > 0 && len(terms) <= 2 {
		return facts[:1]
	}
	return out
}

func significantTerms(text string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(token) < 4 || stopword(token) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func stopword(value string) bool {
	switch value {
	case "what", "when", "where", "which", "with", "from", "that", "this", "there", "about", "have", "does", "will", "your", "into":
		return true
	default:
		return false
	}
}

func answerFromFacts(question string, facts []Fact) string {
	lines := []string{"Stored answer for: " + strings.TrimSpace(question)}
	for _, fact := range boundedFacts(facts) {
		lines = append(lines, "- "+fact.Claim)
	}
	return strings.Join(lines, "\n")
}

func sourcesForFacts(sources []Source, facts []Fact) []Source {
	wanted := map[string]struct{}{}
	for _, fact := range facts {
		for _, id := range fact.SourceIDs {
			wanted[id] = struct{}{}
		}
	}
	var out []Source
	for _, source := range sources {
		if _, ok := wanted[source.ID]; ok {
			out = append(out, source)
		}
	}
	return out
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendUnique(values []string, additions ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func boundedFacts(facts []Fact) []Fact {
	if len(facts) > 12 {
		facts = facts[:12]
	}
	out := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		fact.Claim = boundText(fact.Claim, 360)
		fact.ResearchID = ""
		out = append(out, fact)
	}
	return out
}

func boundedSources(sources []Source) []Source {
	if len(sources) > MaxSourcesLimit {
		sources = sources[:MaxSourcesLimit]
	}
	out := make([]Source, 0, len(sources))
	for _, source := range sources {
		source.ResearchID = ""
		source.Snippet = boundText(source.Snippet, 300)
		out = append(out, source)
	}
	return out
}

func boundedStrings(values []string, limit int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = boundText(value, 220); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func boundText(value string, maxChars int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	cut := strings.LastIndex(value[:maxChars], " ")
	if cut < maxChars/2 {
		cut = maxChars
	}
	return strings.TrimSpace(value[:cut]) + "..."
}

func newID(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "_" + hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return prefix + "_" + hex.EncodeToString(raw[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
