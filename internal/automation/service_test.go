package automation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

type fakeStore struct {
	jobs  map[string]Job
	fires map[string]Fire
}

func newFakeStore() *fakeStore {
	return &fakeStore{jobs: map[string]Job{}, fires: map[string]Fire{}}
}

func (s *fakeStore) CreateAutomationJob(_ context.Context, job Job) error {
	s.jobs[job.ID] = job
	return nil
}

func (s *fakeStore) GetAutomationJob(_ context.Context, jobID string) (Job, error) {
	job, ok := s.jobs[jobID]
	if !ok {
		return Job{}, core.ErrNotFound
	}
	return job, nil
}

func (s *fakeStore) ListAutomationJobs(_ context.Context, filter JobFilter) ([]Job, error) {
	jobs := []Job{}
	for _, job := range s.jobs {
		if filter.Status != "" && job.Status != filter.Status {
			continue
		}
		if !filter.IncludeDeleted && job.Status == JobStatusDeleted {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *fakeStore) UpdateAutomationJob(_ context.Context, job Job) error {
	if _, ok := s.jobs[job.ID]; !ok {
		return core.ErrNotFound
	}
	s.jobs[job.ID] = job
	return nil
}

func (s *fakeStore) CreateAutomationFire(_ context.Context, fire Fire) error {
	key := fire.JobID + ":" + fire.ScheduledFor.Format(time.RFC3339Nano)
	if _, ok := s.fires[key]; ok {
		return ErrDuplicateFire
	}
	s.fires[key] = fire
	return nil
}

func (s *fakeStore) GetAutomationFireBySchedule(_ context.Context, jobID string, scheduledFor time.Time) (Fire, error) {
	key := jobID + ":" + scheduledFor.Format(time.RFC3339Nano)
	fire, ok := s.fires[key]
	if !ok {
		return Fire{}, core.ErrNotFound
	}
	return fire, nil
}

func (s *fakeStore) UpdateAutomationFire(_ context.Context, fire Fire) error {
	for key, existing := range s.fires {
		if existing.ID == fire.ID {
			s.fires[key] = fire
			return nil
		}
	}
	return core.ErrNotFound
}

type fakeRunner struct {
	runs int
}

func (r *fakeRunner) AcceptTriggeredRun(_ context.Context, input core.HandleTriggeredRunInput) (core.AcceptRunResult, error) {
	r.runs++
	if input.TriggerID == "" {
		return core.AcceptRunResult{}, errors.New("missing trigger")
	}
	return core.AcceptRunResult{
		SessionID: input.SessionID,
		Run:       core.Run{ID: "run_" + input.TriggerID, SessionID: input.SessionID},
	}, nil
}

type fakeRunGetterRunner struct {
	fakeRunner
	run core.Run
}

func (r *fakeRunGetterRunner) GetRun(context.Context, string) (core.Run, error) {
	if r.run.ID == "" {
		return core.Run{}, core.ErrNotFound
	}
	return r.run, nil
}

type fakeDeliveryRunner struct {
	fakeRunner
	deliveries []core.ClientDelivery
}

func (r *fakeDeliveryRunner) CreateClientDelivery(_ context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error) {
	r.deliveries = append(r.deliveries, delivery)
	return delivery, nil
}

func TestServiceFiresOnceJobOnce(t *testing.T) {
	store := newFakeStore()
	runner := &fakeRunner{}
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	service := NewService(store, runner, "UTC").WithClock(func() time.Time { return now })
	runAt := now.Add(time.Minute)
	job, err := service.CreateJob(context.Background(), CreateJobInput{
		Kind:         JobKindReminder,
		SessionID:    "session_1",
		ScheduleMode: ScheduleModeOnce,
		RunAt:        &runAt,
		Prompt:       "test reminder",
	})
	if err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	now = runAt.Add(time.Second)
	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if runner.runs != 1 {
		t.Fatalf("runs = %d, want 1", runner.runs)
	}
	updated, err := store.GetAutomationJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetAutomationJob() error = %v", err)
	}
	if updated.Status != JobStatusCompleted {
		t.Fatalf("job status = %q, want completed", updated.Status)
	}
	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("second Tick() error = %v", err)
	}
	if runner.runs != 1 {
		t.Fatalf("runs after second tick = %d, want 1", runner.runs)
	}
}

func TestCreateJobForRunUsesOptionalRunLookup(t *testing.T) {
	store := newFakeStore()
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	runner := &fakeRunGetterRunner{
		run: core.Run{
			ID:          "run_1",
			SessionID:   "session_from_run",
			Client:      "terminal",
			ExternalKey: "local",
		},
	}
	service := NewService(store, runner, "UTC").WithClock(func() time.Time { return now })
	runAt := now.Add(time.Minute)

	job, err := service.CreateJobForRun(context.Background(), "run_1", CreateJobInput{
		Kind:         JobKindReminder,
		ScheduleMode: ScheduleModeOnce,
		RunAt:        &runAt,
		Prompt:       "test reminder",
	})
	if err != nil {
		t.Fatalf("CreateJobForRun() error = %v", err)
	}
	if job.SessionID != "session_from_run" {
		t.Fatalf("job.SessionID = %q, want session_from_run", job.SessionID)
	}
	if job.Client != "terminal" || job.ExternalKey != "local" {
		t.Fatalf("job client binding = %q/%q, want terminal/local", job.Client, job.ExternalKey)
	}
}

func TestServiceCreatesTelegramDeliveryAddress(t *testing.T) {
	store := newFakeStore()
	runner := &fakeDeliveryRunner{}
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	service := NewService(store, runner, "UTC").WithClock(func() time.Time { return now })
	runAt := now.Add(time.Minute)

	_, err := service.CreateJob(context.Background(), CreateJobInput{
		Kind:         JobKindReminder,
		SessionID:    "session_1",
		Client:       "telegram",
		ExternalKey:  "42:7",
		ScheduleMode: ScheduleModeOnce,
		RunAt:        &runAt,
		Prompt:       "test reminder",
	})
	if err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}
	now = runAt.Add(time.Second)
	if err := service.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(runner.deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(runner.deliveries))
	}
	delivery := runner.deliveries[0]
	if delivery.SessionID != "session_1" || delivery.RunID == "" || delivery.TaskID == "" {
		t.Fatalf("delivery refs = %#v, want session/run/task fields", delivery)
	}
	if delivery.Summary == "" {
		t.Fatalf("delivery summary is empty: %#v", delivery)
	}
	var address struct {
		ChatID   int64 `json:"chat_id"`
		ThreadID int64 `json:"thread_id"`
	}
	if err := json.Unmarshal(delivery.Address, &address); err != nil {
		t.Fatalf("delivery address decode error = %v", err)
	}
	if address.ChatID != 42 || address.ThreadID != 7 {
		t.Fatalf("delivery address = %+v, want chat=42 thread=7", address)
	}
}

func TestServiceRejectsPastOnceJob(t *testing.T) {
	store := newFakeStore()
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	service := NewService(store, &fakeRunner{}, "UTC").WithClock(func() time.Time { return now })
	runAt := now.Add(-time.Minute)

	_, err := service.CreateJob(context.Background(), CreateJobInput{
		Kind:         JobKindReminder,
		SessionID:    "session_1",
		ScheduleMode: ScheduleModeOnce,
		RunAt:        &runAt,
		Prompt:       "test reminder",
	})
	if err == nil {
		t.Fatal("CreateJob() error = nil, want invalid input")
	}
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("CreateJob() error = %v, want ErrInvalidInput", err)
	}
}

func TestRenderPromptFormatsReminderCompactly(t *testing.T) {
	scheduledFor := time.Date(2026, 4, 27, 16, 0, 0, 0, time.UTC)
	got := renderPrompt(Job{
		Kind:     JobKindReminder,
		Title:    "Dentist",
		Timezone: "Europe/Moscow",
		Prompt:   "go to the dentist",
	}, scheduledFor, scheduledFor)

	want := "⏰ Reminder: Dentist\n🕒 2026-04-27 19:00 MSK\n📝 go to the dentist"
	if got != want {
		t.Fatalf("renderPrompt() = %q, want %q", got, want)
	}
}
