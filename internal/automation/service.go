package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/ids"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

const (
	tickInterval    = 30 * time.Second
	defaultTimezone = "UTC"
	// busyRetryWindow is how long past its time a job whose session is busy
	// with another run is tried again on every tick before its fire fails.
	busyRetryWindow = time.Hour
	// dueBatch bounds the jobs one tick fires; the rest fire on the next.
	dueBatch = 100
)

// Runner is what automation needs of core: it starts the job runs, reads the
// run a job is created from and queues the deliveries of results.
type Runner interface {
	AcceptTriggeredRun(ctx context.Context, input core.HandleTriggeredRunInput) (core.AcceptRunResult, error)
	GetRun(ctx context.Context, runID string) (core.Run, error)
	CreateClientDelivery(ctx context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error)
	ListClientDeliveries(ctx context.Context, filter core.ClientDeliveryFilter) ([]core.ClientDelivery, error)
}

// Settings are automation's settings from setup.json: the timezone of jobs
// created without one and the targets every job's results also go to.
type Settings struct {
	Timezone string
	Targets  []core.ClientDeliveryTarget
}

type Service struct {
	store    Store
	runner   Runner
	now      func() time.Time
	newID    func(prefix string) string
	settings atomic.Pointer[Settings]
	mu       sync.Mutex
}

func NewService(store Store, runner Runner) *Service {
	s := &Service{store: store, runner: runner, now: time.Now, newID: ids.New}
	s.Configure(Settings{})
	return s
}

func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// Configure applies the settings; it is safe while jobs fire.
func (s *Service) Configure(settings Settings) {
	settings.Timezone = strings.TrimSpace(settings.Timezone)
	if settings.Timezone == "" {
		settings.Timezone = defaultTimezone
	}
	s.settings.Store(&settings)
}

func (s *Service) currentSettings() Settings {
	return *s.settings.Load()
}

func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	s.tickAndLog(ctx)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tickAndLog(ctx)
		}
	}
}

func (s *Service) tickAndLog(ctx context.Context) {
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("matrixclaw automation tick failed: %v", err)
	}
}

func (s *Service) CreateJob(ctx context.Context, input CreateJobInput) (Job, error) {
	if s == nil || s.store == nil {
		return Job{}, fmt.Errorf("%w: automation store not configured", core.ErrExecutionUnavailable)
	}
	job, err := s.buildJob(input)
	if err != nil {
		return Job{}, err
	}
	if err := s.store.CreateAutomationJob(ctx, job); err != nil {
		return Job{}, err
	}
	return job, nil
}

// CreateJobForRun creates a job for the session, client and delivery address
// of the run that asks for it.
func (s *Service) CreateJobForRun(ctx context.Context, runID string, input CreateJobInput) (Job, error) {
	run, err := s.runner.GetRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return Job{}, err
	}
	input.SessionID, input.Client, input.ExternalKey = run.SessionID, run.Client, run.ExternalKey
	input.ClientCapabilities = run.ClientCapabilities
	if input.DeliveryAddress, err = s.runDeliveryAddress(ctx, run); err != nil {
		return Job{}, err
	}
	return s.CreateJob(ctx, input)
}

// runDeliveryAddress is where the run's reply goes, unless that address takes
// one reply only.
func (s *Service) runDeliveryAddress(ctx context.Context, run core.Run) (json.RawMessage, error) {
	if !run.ClientCapabilities.ReceivesDeliveries {
		return nil, nil
	}
	deliveries, err := s.runner.ListClientDeliveries(ctx, core.ClientDeliveryFilter{Client: run.Client, ExternalKey: run.ExternalKey, RunID: run.ID, Type: core.ClientDeliveryTypeRun, Limit: 1})
	if err != nil || len(deliveries) == 0 || deliveries[0].ReplyOnce {
		return nil, err
	}
	return deliveries[0].Address, nil
}

func (s *Service) ListJobs(ctx context.Context, filter JobFilter) ([]Job, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: automation store not configured", core.ErrExecutionUnavailable)
	}
	return s.store.ListAutomationJobs(ctx, filter)
}

func (s *Service) GetJob(ctx context.Context, jobID string) (Job, error) {
	if s == nil || s.store == nil {
		return Job{}, fmt.Errorf("%w: automation store not configured", core.ErrExecutionUnavailable)
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return Job{}, fmt.Errorf("%w: job id is required", core.ErrInvalidInput)
	}
	return s.store.GetAutomationJob(ctx, jobID)
}

func (s *Service) PauseJob(ctx context.Context, jobID string) (Job, error) {
	return s.setJobStatus(ctx, jobID, JobStatusPaused)
}

func (s *Service) ResumeJob(ctx context.Context, jobID string) (Job, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	job.Status = JobStatusActive
	now := s.now().UTC()
	job.UpdatedAt = now
	next, err := s.nextDue(job, now)
	if err != nil {
		return Job{}, err
	}
	job.NextDueAt = next
	if err := s.store.UpdateAutomationJob(ctx, job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Service) CompleteJob(ctx context.Context, jobID string) (Job, error) {
	return s.setJobStatus(ctx, jobID, JobStatusCompleted)
}

func (s *Service) DeleteJob(ctx context.Context, jobID string) (Job, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	now := s.now().UTC()
	job.Status = JobStatusDeleted
	job.UpdatedAt = now
	job.DeletedAt = &now
	job.NextDueAt = nil
	if err := s.store.UpdateAutomationJob(ctx, job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Service) RunNow(ctx context.Context, jobID string) (Fire, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return Fire{}, err
	}
	if job.Status != JobStatusActive {
		return Fire{}, fmt.Errorf("%w: automation job %s is %s", core.ErrInvalidInput, job.ID, job.Status)
	}
	return s.fireJob(ctx, job, s.now().UTC())
}

func (s *Service) Tick(ctx context.Context) error {
	if s == nil || s.store == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs, err := s.store.ListAutomationJobs(ctx, JobFilter{
		Status: JobStatusActive,
		DueBy:  s.now().UTC(),
		Limit:  dueBatch,
	})
	if err != nil {
		return err
	}
	var firstErr error
	for _, job := range jobs {
		scheduledFor := *job.NextDueAt
		if _, err := s.fireJob(ctx, job, scheduledFor); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) buildJob(input CreateJobInput) (Job, error) {
	kind := input.Kind
	if kind == "" {
		kind = JobKindReminder
	}
	switch kind {
	case JobKindReminder, JobKindAITask:
	default:
		return Job{}, fmt.Errorf("%w: unsupported automation kind %q", core.ErrInvalidInput, kind)
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return Job{}, fmt.Errorf("%w: session id is required", core.ErrInvalidInput)
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return Job{}, fmt.Errorf("%w: prompt is required", core.ErrInvalidInput)
	}
	timezone := textutil.FirstNonEmpty(strings.TrimSpace(input.Timezone), s.currentSettings().Timezone)
	if _, err := time.LoadLocation(timezone); err != nil {
		return Job{}, fmt.Errorf("%w: invalid timezone %q", core.ErrInvalidInput, timezone)
	}
	mode := input.ScheduleMode
	if mode == "" {
		mode = ScheduleModeOnce
	}
	now := s.now().UTC()
	job := Job{
		ID:                 s.newID("job"),
		Kind:               kind,
		Status:             JobStatusActive,
		SessionID:          sessionID,
		Client:             strings.TrimSpace(input.Client),
		ExternalKey:        strings.TrimSpace(input.ExternalKey),
		ClientCapabilities: input.ClientCapabilities,
		DeliveryAddress:    input.DeliveryAddress,
		Title:              strings.TrimSpace(input.Title),
		Timezone:           timezone,
		ScheduleMode:       mode,
		RunAt:              input.RunAt,
		IntervalSeconds:    input.IntervalSeconds,
		CronExpr:           strings.TrimSpace(input.CronExpr),
		Prompt:             prompt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if job.Title == "" {
		job.Title = titleFromPrompt(prompt)
	}
	next, err := s.nextDue(job, now)
	if err != nil {
		return Job{}, err
	}
	job.NextDueAt = next
	return job, nil
}

func (s *Service) fireJob(ctx context.Context, job Job, scheduledFor time.Time) (Fire, error) {
	now := s.now().UTC()
	fire := Fire{
		ID:           s.newID("fire"),
		JobID:        job.ID,
		ScheduledFor: scheduledFor.UTC(),
		Status:       FireStatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateAutomationFire(ctx, fire); err != nil {
		if errors.Is(err, ErrDuplicateFire) {
			return s.handleDuplicateFire(ctx, job, scheduledFor)
		}
		return Fire{}, err
	}
	return s.runFire(ctx, job, fire, scheduledFor)
}

func (s *Service) handleDuplicateFire(ctx context.Context, job Job, scheduledFor time.Time) (Fire, error) {
	fire, err := s.store.GetAutomationFireBySchedule(ctx, job.ID, scheduledFor)
	if err != nil {
		return Fire{}, err
	}
	switch fire.Status {
	case FireStatusCompleted:
		if err := s.advanceJob(ctx, job, scheduledFor); err != nil {
			return fire, err
		}
		return fire, nil
	case FireStatusRunning:
		return s.runFire(ctx, job, fire, scheduledFor)
	case FireStatusFailed:
		if err := s.advanceJob(ctx, job, scheduledFor); err != nil {
			return fire, err
		}
		return fire, nil
	default:
		return fire, nil
	}
}

func (s *Service) runFire(ctx context.Context, job Job, fire Fire, scheduledFor time.Time) (Fire, error) {
	now := s.now().UTC()
	result, err := s.runner.AcceptTriggeredRun(ctx, core.HandleTriggeredRunInput{
		TriggerID:          fire.ID,
		Client:             job.Client,
		ExternalKey:        job.ExternalKey,
		ClientCapabilities: job.ClientCapabilities,
		SessionID:          job.SessionID,
		Text:               renderPrompt(job, scheduledFor, now),
	})
	if errors.Is(err, core.ErrRunActive) && now.Sub(scheduledFor) < busyRetryWindow {
		// The fire stays running and the job due: the next tick tries again.
		return fire, nil
	}
	if err != nil {
		fire.Status = FireStatusFailed
		fire.Error = err.Error()
		finished := s.now().UTC()
		fire.UpdatedAt = finished
		fire.FinishedAt = &finished
		resultErr := err
		if updateErr := s.store.UpdateAutomationFire(ctx, fire); updateErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("update failed automation fire: %w", updateErr))
		}
		if advanceErr := s.advanceJob(ctx, job, scheduledFor); advanceErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("advance failed automation job: %w", advanceErr))
		}
		return fire, resultErr
	}

	fire.RunID = result.Run.ID
	fire.Status = FireStatusCompleted
	finished := s.now().UTC()
	fire.UpdatedAt = finished
	fire.FinishedAt = &finished
	if err := s.store.UpdateAutomationFire(ctx, fire); err != nil {
		return fire, err
	}
	if err := s.createRunDeliveries(ctx, job, result); err != nil {
		return fire, err
	}
	if err := s.advanceJob(ctx, job, scheduledFor); err != nil {
		return fire, err
	}
	return fire, nil
}

func (s *Service) advanceJob(ctx context.Context, job Job, scheduledFor time.Time) error {
	current, err := s.store.GetAutomationJob(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.Status != JobStatusActive {
		return nil
	}
	if current.NextDueAt == nil || !current.NextDueAt.UTC().Equal(scheduledFor.UTC()) {
		return nil
	}
	job = current
	now := s.now().UTC()
	job.LastScheduledFor = &scheduledFor
	job.UpdatedAt = now
	switch job.ScheduleMode {
	case ScheduleModeOnce:
		job.Status = JobStatusCompleted
		job.NextDueAt = nil
	case ScheduleModeInterval, ScheduleModeCron:
		next, err := s.nextDue(job, now)
		if err != nil {
			return err
		}
		job.NextDueAt = next
	}
	return s.store.UpdateAutomationJob(ctx, job)
}

func (s *Service) setJobStatus(ctx context.Context, jobID string, status JobStatus) (Job, error) {
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	job.Status = status
	job.UpdatedAt = s.now().UTC()
	if status != JobStatusActive {
		job.NextDueAt = nil
	}
	if err := s.store.UpdateAutomationJob(ctx, job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s *Service) createRunDeliveries(ctx context.Context, job Job, result core.AcceptRunResult) error {
	var errs []error
	for _, target := range s.deliveryTargetsForJob(job) {
		_, err := s.runner.CreateClientDelivery(ctx, core.ClientDelivery{
			Type:        core.ClientDeliveryTypeRun,
			Client:      target.Client,
			ExternalKey: target.ExternalKey,
			SessionID:   result.SessionID,
			RunID:       result.Run.ID,
			TaskID:      job.ID,
			Summary:     job.Title,
			Address:     target.Address,
			Status:      core.ClientDeliveryStatusPending,
		})
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// deliveryTargetsForJob is the job's own client when it receives deliveries,
// and the configured targets.
func (s *Service) deliveryTargetsForJob(job Job) []core.ClientDeliveryTarget {
	var targets []core.ClientDeliveryTarget
	if job.ClientCapabilities.ReceivesDeliveries {
		targets = append(targets, core.ClientDeliveryTarget{Client: job.Client, ExternalKey: job.ExternalKey, Address: job.DeliveryAddress})
	}
	return dedupeDeliveryTargets(append(targets, s.currentSettings().Targets...))
}

// dedupeDeliveryTargets drops targets without a client or place and repeats
// of one client's chat, the first kept.
func dedupeDeliveryTargets(targets []core.ClientDeliveryTarget) []core.ClientDeliveryTarget {
	deduped := make([]core.ClientDeliveryTarget, 0, len(targets))
	seen := map[string]bool{}
	for _, target := range targets {
		target.Client = strings.TrimSpace(target.Client)
		target.ExternalKey = strings.TrimSpace(target.ExternalKey)
		place := textutil.FirstNonEmpty(target.ExternalKey, string(target.Address))
		key := strings.ToLower(target.Client) + "\x00" + place
		if target.Client == "" || place == "" || seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, target)
	}
	return deduped
}
