package work

import (
	"context"
	"time"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
)

const (
	KindWebResearch = "web_research"
	KindSubagent    = "subagent"
)

type Job struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	Task           string     `json:"task,omitempty"`
	Summary        string     `json:"summary,omitempty"`
	ResultJSON     string     `json:"result_json,omitempty"`
	WorkerRef      string     `json:"worker_ref,omitempty"`
	TokenUsageJSON string     `json:"token_usage_json,omitempty"`
	InputJSON      string     `json:"input_json,omitempty"`
	Error          string     `json:"error,omitempty"`
	Attempts       int        `json:"attempts,omitempty"`
	HeartbeatAt    *time.Time `json:"heartbeat_at,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type Artifact struct {
	ID           string    `json:"id"`
	JobID        string    `json:"job_id"`
	SourceID     string    `json:"source_id,omitempty"`
	SourceURL    string    `json:"source_url,omitempty"`
	Kind         string    `json:"kind"`
	Path         string    `json:"path"`
	MIMEType     string    `json:"mime_type,omitempty"`
	ByteCount    int64     `json:"byte_count,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Fact struct {
	ID           string    `json:"id"`
	JobID        string    `json:"job_id"`
	Claim        string    `json:"claim"`
	Confidence   float64   `json:"confidence,omitempty"`
	SourceIDs    []string  `json:"source_ids,omitempty"`
	ArtifactIDs  []string  `json:"artifact_ids,omitempty"`
	MetadataJSON string    `json:"metadata_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type JobFilter struct {
	Kinds      []string
	KindPrefix string
	Statuses   []string
	WorkerRef  string
	Before     *time.Time
	Limit      int
}

type Store interface {
	CreateJob(ctx context.Context, job Job) error
	UpdateJob(ctx context.Context, job Job) error
	GetJob(ctx context.Context, id string) (Job, error)
	ListJobs(ctx context.Context, filter JobFilter) ([]Job, error)
	DeleteJob(ctx context.Context, id string) error
	DeleteJobsByWorkerRef(ctx context.Context, workerRef string) error

	CreateArtifact(ctx context.Context, artifact Artifact) error
	ListArtifacts(ctx context.Context, jobID string) ([]Artifact, error)
	ListArtifactsForExpiredJobs(ctx context.Context, kind string, before time.Time) ([]Artifact, error)

	ReplaceFacts(ctx context.Context, jobID string, facts []Fact) error
	ListFacts(ctx context.Context, jobID string) ([]Fact, error)
}
