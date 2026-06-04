package webresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

const (
	DefaultMaxSources     = 5
	MaxSourcesLimit       = 12
	DefaultRetentionDays  = 30
	DefaultSyncTimeout    = 25 * time.Second
	DefaultHeartbeatEvery = 5 * time.Second
	DefaultStaleAfter     = 60 * time.Second
	DefaultRetryLimit     = 2
)

type ResearchRequest struct {
	Task       string   `json:"task"`
	Query      string   `json:"query,omitempty"`
	URLs       []string `json:"urls,omitempty"`
	MaxSources int      `json:"max_sources,omitempty"`
	Depth      string   `json:"depth,omitempty"`
	Browser    string   `json:"browser,omitempty"`
	Freshness  string   `json:"freshness,omitempty"`
	Async      string   `json:"async,omitempty"`
}

type AskRequest struct {
	ResearchID string `json:"research_id"`
	Question   string `json:"question"`
	Freshness  string `json:"freshness,omitempty"`
	Browser    string `json:"browser,omitempty"`
}

type StatusRequest struct {
	ResearchID string `json:"research_id"`
}

func (r *ResearchRequest) UnmarshalJSON(data []byte) error {
	type wire struct {
		Task       string   `json:"task"`
		Query      string   `json:"query,omitempty"`
		URLs       []string `json:"urls,omitempty"`
		MaxSources int      `json:"max_sources,omitempty"`
		Depth      string   `json:"depth,omitempty"`
		Browser    any      `json:"browser,omitempty"`
		Freshness  string   `json:"freshness,omitempty"`
		Async      any      `json:"async,omitempty"`
	}
	var input wire
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	r.Task = input.Task
	r.Query = input.Query
	r.URLs = input.URLs
	r.MaxSources = input.MaxSources
	r.Depth = input.Depth
	r.Browser = optionString(input.Browser)
	r.Freshness = input.Freshness
	r.Async = optionString(input.Async)
	return nil
}

func (r *AskRequest) UnmarshalJSON(data []byte) error {
	type wire struct {
		ResearchID string `json:"research_id"`
		Question   string `json:"question"`
		Freshness  string `json:"freshness,omitempty"`
		Browser    any    `json:"browser,omitempty"`
	}
	var input wire
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}
	r.ResearchID = input.ResearchID
	r.Question = input.Question
	r.Freshness = input.Freshness
	r.Browser = optionString(input.Browser)
	return nil
}

func optionString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

type ResearchResult struct {
	ResearchID  string   `json:"research_id"`
	Status      string   `json:"status"`
	Answer      string   `json:"answer,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Facts       []Fact   `json:"facts,omitempty"`
	Sources     []Source `json:"sources,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	NextActions []string `json:"next_actions,omitempty"`
}

type ResearchSession struct {
	ID          string     `json:"id"`
	Task        string     `json:"task"`
	Query       string     `json:"query,omitempty"`
	Status      string     `json:"status"`
	Answer      string     `json:"answer,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Warnings    []string   `json:"warnings,omitempty"`
	NextActions []string   `json:"next_actions,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ResearchJob struct {
	ID          string     `json:"id"`
	ResearchID  string     `json:"research_id"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	InputJSON   string     `json:"input_json,omitempty"`
	Error       string     `json:"error,omitempty"`
	Attempts    int        `json:"attempts"`
	HeartbeatAt *time.Time `json:"heartbeat_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

type Source struct {
	ID          string    `json:"id"`
	ResearchID  string    `json:"research_id,omitempty"`
	URL         string    `json:"url"`
	Title       string    `json:"title,omitempty"`
	Snippet     string    `json:"snippet,omitempty"`
	StatusCode  int       `json:"status_code,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	FetchedAt   time.Time `json:"fetched_at,omitempty"`
	ArtifactIDs []string  `json:"artifact_ids,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type Fact struct {
	ID         string    `json:"id"`
	ResearchID string    `json:"research_id,omitempty"`
	Claim      string    `json:"claim"`
	SourceIDs  []string  `json:"source_ids,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

type Artifact struct {
	ID         string    `json:"id"`
	ResearchID string    `json:"research_id"`
	SourceID   string    `json:"source_id,omitempty"`
	Kind       string    `json:"kind"`
	Path       string    `json:"path"`
	MIMEType   string    `json:"mime_type,omitempty"`
	ByteCount  int64     `json:"byte_count,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type SearchResult struct {
	Position    int    `json:"position"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type SearchOutput struct {
	Provider string         `json:"provider,omitempty"`
	Results  []SearchResult `json:"results,omitempty"`
}

type FetchedPage struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url,omitempty"`
	Title       string `json:"title,omitempty"`
	Text        string `json:"text,omitempty"`
	HTML        string `json:"html,omitempty"`
	StatusCode  int    `json:"status_code,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

type BrowserPage struct {
	URL          string `json:"url"`
	Title        string `json:"title,omitempty"`
	Text         string `json:"text,omitempty"`
	DOMSnapshot  string `json:"dom_snapshot,omitempty"`
	Screenshot   []byte `json:"-"`
	ScreenshotMT string `json:"screenshot_mime_type,omitempty"`
}

type Searcher interface {
	Search(ctx context.Context, query string, limit int) (SearchOutput, error)
}

type SearchFunc func(ctx context.Context, query string, limit int) (SearchOutput, error)

func (f SearchFunc) Search(ctx context.Context, query string, limit int) (SearchOutput, error) {
	return f(ctx, query, limit)
}

type Fetcher interface {
	Fetch(ctx context.Context, url string, maxChars int) (FetchedPage, error)
}

type FetchFunc func(ctx context.Context, url string, maxChars int) (FetchedPage, error)

func (f FetchFunc) Fetch(ctx context.Context, url string, maxChars int) (FetchedPage, error) {
	return f(ctx, url, maxChars)
}

type Browser interface {
	Available() bool
	SetupHint() string
	Fetch(ctx context.Context, url string) (BrowserPage, error)
}

type Summarizer interface {
	Summarize(ctx context.Context, input SummaryInput) (SummaryOutput, error)
}

type SummaryInput struct {
	Task    string
	Query   string
	Sources []SourceDocument
}

type SourceDocument struct {
	Source Source
	Text   string
}

type SummaryOutput struct {
	Answer  string
	Summary string
	Facts   []Fact
}
