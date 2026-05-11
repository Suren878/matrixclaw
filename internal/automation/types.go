package automation

import "time"

type JobKind string

const (
	JobKindReminder JobKind = "reminder"
	JobKindAITask   JobKind = "ai_task"
)

type JobStatus string

const (
	JobStatusActive    JobStatus = "active"
	JobStatusPaused    JobStatus = "paused"
	JobStatusCompleted JobStatus = "completed"
	JobStatusDeleted   JobStatus = "deleted"
)

type ScheduleMode string

const (
	ScheduleModeOnce     ScheduleMode = "once"
	ScheduleModeInterval ScheduleMode = "interval"
	ScheduleModeCron     ScheduleMode = "cron"
)

type FireStatus string

const (
	FireStatusPending   FireStatus = "pending"
	FireStatusRunning   FireStatus = "running"
	FireStatusCompleted FireStatus = "completed"
	FireStatusFailed    FireStatus = "failed"
	FireStatusSkipped   FireStatus = "skipped"
)

type Job struct {
	ID               string       `json:"id"`
	Kind             JobKind      `json:"kind"`
	Status           JobStatus    `json:"status"`
	SessionID        string       `json:"session_id"`
	Client           string       `json:"client,omitempty"`
	ExternalKey      string       `json:"external_key,omitempty"`
	Title            string       `json:"title,omitempty"`
	Timezone         string       `json:"timezone"`
	ScheduleMode     ScheduleMode `json:"schedule_mode"`
	RunAt            *time.Time   `json:"run_at,omitempty"`
	IntervalSeconds  int          `json:"interval_seconds,omitempty"`
	CronExpr         string       `json:"cron_expr,omitempty"`
	NextDueAt        *time.Time   `json:"next_due_at,omitempty"`
	LastScheduledFor *time.Time   `json:"last_scheduled_for,omitempty"`
	Prompt           string       `json:"prompt"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	DeletedAt        *time.Time   `json:"deleted_at,omitempty"`
}

type JobFilter struct {
	IncludeDeleted bool
	Status         JobStatus
	SessionID      string
	Limit          int
}

type Fire struct {
	ID           string     `json:"id"`
	JobID        string     `json:"job_id"`
	ScheduledFor time.Time  `json:"scheduled_for"`
	Status       FireStatus `json:"status"`
	ResultState  string     `json:"result_state,omitempty"`
	RunID        string     `json:"run_id,omitempty"`
	Error        string     `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

type CreateJobInput struct {
	Kind            JobKind
	SessionID       string
	Client          string
	ExternalKey     string
	Title           string
	Timezone        string
	ScheduleMode    ScheduleMode
	RunAt           *time.Time
	IntervalSeconds int
	CronExpr        string
	Prompt          string
}
