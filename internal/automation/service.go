package automation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

const (
	DefaultTickInterval = 30 * time.Second
	DefaultTimezone     = "UTC"
	telegramClientName  = "telegram"
)

type Runner interface {
	AcceptTriggeredRun(ctx context.Context, input core.HandleTriggeredRunInput) (core.AcceptRunResult, error)
}

type RunGetter interface {
	GetRun(ctx context.Context, runID string) (core.Run, error)
}

type DeliveryCreator interface {
	CreateClientDelivery(ctx context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error)
}

type Service struct {
	store        Store
	runner       Runner
	timezone     string
	tickInterval time.Duration
	now          func() time.Time
	newID        func(prefix string) string
	deliveries   []core.ClientDeliveryTarget
	mu           sync.Mutex
}

func NewService(store Store, runner Runner, timezone string) *Service {
	if strings.TrimSpace(timezone) == "" {
		timezone = DefaultTimezone
	}
	return &Service{
		store:        store,
		runner:       runner,
		timezone:     strings.TrimSpace(timezone),
		tickInterval: DefaultTickInterval,
		now:          time.Now,
		newID:        randomID,
	}
}

func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) WithTickInterval(interval time.Duration) *Service {
	if interval > 0 {
		s.tickInterval = interval
	}
	return s
}

func (s *Service) WithDeliveryTargets(targets []core.ClientDeliveryTarget) *Service {
	if s == nil {
		return s
	}
	s.deliveries = normalizeDeliveryTargets(targets)
	return s
}

func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	s.tickAndLog(ctx)
	ticker := time.NewTicker(s.tickInterval)
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

func (s *Service) CreateJobForRun(ctx context.Context, runID string, input CreateJobInput) (Job, error) {
	if s == nil {
		return Job{}, fmt.Errorf("%w: automation run lookup not configured", core.ErrExecutionUnavailable)
	}
	runGetter, ok := s.runner.(RunGetter)
	if !ok {
		return Job{}, fmt.Errorf("%w: automation run lookup not configured", core.ErrExecutionUnavailable)
	}
	run, err := runGetter.GetRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(input.SessionID) == "" {
		input.SessionID = run.SessionID
	}
	if strings.TrimSpace(input.Client) == "" {
		input.Client = run.Client
	}
	if strings.TrimSpace(input.ExternalKey) == "" {
		input.ExternalKey = run.ExternalKey
	}
	return s.CreateJob(ctx, input)
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

	now := s.now().UTC()
	jobs, err := s.store.ListAutomationJobs(ctx, JobFilter{
		Status: JobStatusActive,
		Limit:  100,
	})
	if err != nil {
		return err
	}
	var firstErr error
	for _, job := range jobs {
		if job.NextDueAt == nil || job.NextDueAt.After(now) {
			continue
		}
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
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = s.timezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Job{}, fmt.Errorf("%w: invalid timezone %q", core.ErrInvalidInput, timezone)
	}
	mode := input.ScheduleMode
	if mode == "" {
		mode = ScheduleModeOnce
	}
	now := s.now().UTC()
	job := Job{
		ID:              s.newID("job"),
		Kind:            kind,
		Status:          JobStatusActive,
		SessionID:       sessionID,
		Client:          strings.TrimSpace(input.Client),
		ExternalKey:     strings.TrimSpace(input.ExternalKey),
		Title:           strings.TrimSpace(input.Title),
		Timezone:        timezone,
		ScheduleMode:    mode,
		RunAt:           input.RunAt,
		IntervalSeconds: input.IntervalSeconds,
		CronExpr:        strings.TrimSpace(input.CronExpr),
		Prompt:          prompt,
		CreatedAt:       now,
		UpdatedAt:       now,
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
		ClientCapabilities: automationClientCapabilities(job.Client),
		SessionID:          job.SessionID,
		Text:               renderPrompt(job, scheduledFor, now),
	})
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
	if len(s.deliveryTargetsForJob(job)) > 0 {
		if err := s.createRunDeliveries(ctx, job, result); err != nil {
			return fire, err
		}
	}
	if err := s.advanceJob(ctx, job, scheduledFor); err != nil {
		return fire, err
	}
	return fire, nil
}

func automationClientCapabilities(client string) core.ClientCapabilities {
	if strings.EqualFold(strings.TrimSpace(client), telegramClientName) {
		return core.ClientCapabilities{
			SupportsVoiceDelivery:    true,
			SupportsDocumentDelivery: true,
		}
	}
	return core.ClientCapabilities{}
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
	if s == nil {
		return fmt.Errorf("%w: automation delivery creator not configured", core.ErrExecutionUnavailable)
	}
	deliveryCreator, ok := s.runner.(DeliveryCreator)
	if !ok {
		return fmt.Errorf("%w: automation delivery creator not configured", core.ErrExecutionUnavailable)
	}
	var firstErr error
	for _, target := range s.deliveryTargetsForJob(job) {
		_, err := deliveryCreator.CreateClientDelivery(ctx, core.ClientDelivery{
			Type:        core.ClientDeliveryTypeRun,
			Client:      target.Client,
			ExternalKey: target.ExternalKey,
			SessionID:   firstNonEmpty(result.SessionID, target.SessionID),
			RunID:       firstNonEmpty(result.Run.ID, target.RunID),
			TaskID:      firstNonEmpty(job.ID, target.TaskID),
			Summary:     firstNonEmpty(job.Title, target.Summary),
			Address:     target.Address,
			Status:      core.ClientDeliveryStatusPending,
		})
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) deliveryTargetsForJob(job Job) []core.ClientDeliveryTarget {
	targets := []core.ClientDeliveryTarget{}
	if strings.TrimSpace(job.Client) != "" && strings.TrimSpace(job.ExternalKey) != "" {
		targets = append(targets, core.ClientDeliveryTarget{
			Client:      job.Client,
			ExternalKey: job.ExternalKey,
			Address:     deliveryAddressForJob(job),
		})
	}
	targets = append(targets, s.deliveries...)
	return dedupeDeliveryTargets(targets)
}

func normalizeDeliveryTargets(targets []core.ClientDeliveryTarget) []core.ClientDeliveryTarget {
	normalized := make([]core.ClientDeliveryTarget, 0, len(targets))
	for _, target := range targets {
		target.Client = strings.TrimSpace(target.Client)
		target.ExternalKey = strings.TrimSpace(target.ExternalKey)
		target.SessionID = strings.TrimSpace(target.SessionID)
		target.RunID = strings.TrimSpace(target.RunID)
		target.TaskID = strings.TrimSpace(target.TaskID)
		target.Summary = strings.TrimSpace(target.Summary)
		if target.Client == "" {
			continue
		}
		if target.ExternalKey == "" && len(target.Address) == 0 {
			continue
		}
		if len(target.Address) == 0 {
			target.Address = nil
		}
		normalized = append(normalized, target)
	}
	return normalized
}

func dedupeDeliveryTargets(targets []core.ClientDeliveryTarget) []core.ClientDeliveryTarget {
	normalized := normalizeDeliveryTargets(targets)
	deduped := make([]core.ClientDeliveryTarget, 0, len(normalized))
	seen := map[string]bool{}
	for _, target := range normalized {
		keyPart := target.ExternalKey
		if len(target.Address) > 0 {
			keyPart = string(target.Address)
		}
		key := strings.ToLower(target.Client) + "\x00" + keyPart
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, target)
	}
	return deduped
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func deliveryAddressForJob(job Job) json.RawMessage {
	if !strings.EqualFold(strings.TrimSpace(job.Client), telegramClientName) {
		return nil
	}
	chatID, ok := telegramDeliveryAddressFromExternalKey(job.ExternalKey)
	if !ok {
		return nil
	}
	address := struct {
		ChatID int64 `json:"chat_id"`
	}{
		ChatID: chatID,
	}
	data, err := json.Marshal(address)
	if err != nil {
		return nil
	}
	return data
}

func telegramDeliveryAddressFromExternalKey(value string) (int64, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, false
	}
	chatID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || chatID == 0 {
		return 0, false
	}
	if len(parts) == 1 {
		return chatID, true
	}
	if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
		return 0, false
	}
	return chatID, true
}

func randomID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}
