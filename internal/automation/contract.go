package automation

import (
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

type CreateJobRequest struct {
	Kind            JobKind      `json:"kind"`
	SessionID       string       `json:"session_id"`
	Client          string       `json:"client,omitempty"`
	ExternalKey     string       `json:"external_key,omitempty"`
	Title           string       `json:"title,omitempty"`
	Timezone        string       `json:"timezone,omitempty"`
	ScheduleMode    ScheduleMode `json:"schedule_mode"`
	RunAt           string       `json:"run_at,omitempty"`
	IntervalSeconds int          `json:"interval_seconds,omitempty"`
	CronExpr        string       `json:"cron_expr,omitempty"`
	Prompt          string       `json:"prompt"`
}

type JobResponse struct {
	Job Job `json:"job"`
}

type JobsResponse struct {
	Jobs []Job `json:"jobs"`
}

type FireResponse struct {
	Fire Fire `json:"fire"`
}

func NewCreateJobRequest(input CreateJobInput) CreateJobRequest {
	request := CreateJobRequest{
		Kind:            input.Kind,
		SessionID:       input.SessionID,
		Client:          input.Client,
		ExternalKey:     input.ExternalKey,
		Title:           input.Title,
		Timezone:        input.Timezone,
		ScheduleMode:    input.ScheduleMode,
		IntervalSeconds: input.IntervalSeconds,
		CronExpr:        input.CronExpr,
		Prompt:          input.Prompt,
	}
	if input.RunAt != nil {
		request.RunAt = input.RunAt.UTC().Format(time.RFC3339)
	}
	return request
}

func (request CreateJobRequest) CreateInput() (CreateJobInput, error) {
	var runAt *time.Time
	if strings.TrimSpace(request.RunAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(request.RunAt))
		if err != nil {
			return CreateJobInput{}, core.ErrInvalidInput
		}
		parsed = parsed.UTC()
		runAt = &parsed
	}
	return CreateJobInput{
		Kind:            request.Kind,
		SessionID:       strings.TrimSpace(request.SessionID),
		Client:          strings.TrimSpace(request.Client),
		ExternalKey:     strings.TrimSpace(request.ExternalKey),
		Title:           strings.TrimSpace(request.Title),
		Timezone:        strings.TrimSpace(request.Timezone),
		ScheduleMode:    request.ScheduleMode,
		RunAt:           runAt,
		IntervalSeconds: request.IntervalSeconds,
		CronExpr:        strings.TrimSpace(request.CronExpr),
		Prompt:          strings.TrimSpace(request.Prompt),
	}, nil
}
