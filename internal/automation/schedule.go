package automation

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Service) nextDue(job Job, after time.Time) (*time.Time, error) {
	switch job.ScheduleMode {
	case ScheduleModeOnce:
		if job.RunAt == nil {
			return nil, fmt.Errorf("%w: run_at is required", core.ErrInvalidInput)
		}
		if !job.RunAt.UTC().After(after.UTC()) {
			return nil, fmt.Errorf("%w: run_at must be in the future; current daemon time is %s", core.ErrInvalidInput, formatTimeInLocation(after, job.Timezone))
		}
		if job.LastScheduledFor != nil {
			return nil, nil
		}
		next := job.RunAt.UTC()
		return &next, nil
	case ScheduleModeInterval:
		if job.IntervalSeconds <= 0 {
			return nil, fmt.Errorf("%w: interval_seconds must be positive", core.ErrInvalidInput)
		}
		next := after.UTC().Add(time.Duration(job.IntervalSeconds) * time.Second)
		return &next, nil
	case ScheduleModeCron:
		expr := strings.TrimSpace(job.CronExpr)
		if expr == "" {
			return nil, fmt.Errorf("%w: cron_expr is required", core.ErrInvalidInput)
		}
		loc, err := time.LoadLocation(job.Timezone)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid timezone %q", core.ErrInvalidInput, job.Timezone)
		}
		schedule, err := cron.ParseStandard(expr)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid cron expression: %v", core.ErrInvalidInput, err)
		}
		next := schedule.Next(after.In(loc)).UTC()
		return &next, nil
	default:
		return nil, fmt.Errorf("%w: unsupported schedule mode %q", core.ErrInvalidInput, job.ScheduleMode)
	}
}
