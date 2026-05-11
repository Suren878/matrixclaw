package automation

import (
	"context"
	"errors"
	"time"
)

var ErrDuplicateFire = errors.New("duplicate automation fire")

type Store interface {
	CreateAutomationJob(ctx context.Context, job Job) error
	GetAutomationJob(ctx context.Context, jobID string) (Job, error)
	ListAutomationJobs(ctx context.Context, filter JobFilter) ([]Job, error)
	UpdateAutomationJob(ctx context.Context, job Job) error
	CreateAutomationFire(ctx context.Context, fire Fire) error
	GetAutomationFireBySchedule(ctx context.Context, jobID string, scheduledFor time.Time) (Fire, error)
	UpdateAutomationFire(ctx context.Context, fire Fire) error
}
