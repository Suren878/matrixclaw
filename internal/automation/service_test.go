package automation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

// fakeRunner refuses triggers while busy, as a session with an active run
// does, and records what automation asks of core.
type fakeRunner struct {
	busy          bool
	accepted      []core.HandleTriggeredRunInput
	runs          map[string]core.Run
	runDeliveries []core.ClientDelivery
	created       []core.ClientDelivery
}

func (r *fakeRunner) AcceptTriggeredRun(_ context.Context, input core.HandleTriggeredRunInput) (core.AcceptRunResult, error) {
	if r.busy {
		return core.AcceptRunResult{}, fmt.Errorf("%w: the session is busy with another run", core.ErrRunActive)
	}
	r.accepted = append(r.accepted, input)
	return core.AcceptRunResult{SessionID: input.SessionID, Run: core.Run{ID: "run_" + input.TriggerID, SessionID: input.SessionID}}, nil
}

func (r *fakeRunner) GetRun(_ context.Context, runID string) (core.Run, error) {
	run, ok := r.runs[runID]
	if !ok {
		return core.Run{}, core.ErrNotFound
	}
	return run, nil
}

func (r *fakeRunner) CreateClientDelivery(_ context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error) {
	r.created = append(r.created, delivery)
	return delivery, nil
}

func (r *fakeRunner) ListClientDeliveries(_ context.Context, filter core.ClientDeliveryFilter) ([]core.ClientDelivery, error) {
	var out []core.ClientDelivery
	for _, delivery := range r.runDeliveries {
		if delivery.RunID == filter.RunID && delivery.Type == filter.Type {
			out = append(out, delivery)
		}
	}
	return out, nil
}

func newService(t *testing.T, runner automation.Runner, now *time.Time) *automation.Service {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	session := core.Session{ID: "s1", Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: *now, UpdatedAt: *now}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return automation.NewService(automation.NewSQLiteStore(db.DB()), runner).WithClock(func() time.Time { return *now })
}

func newReminder(t *testing.T, runner automation.Runner, now *time.Time) (*automation.Service, automation.Job) {
	t.Helper()
	service := newService(t, runner, now)
	runAt := now.Add(time.Minute)
	job, err := service.CreateJob(context.Background(), automation.CreateJobInput{SessionID: "s1", ScheduleMode: automation.ScheduleModeOnce, RunAt: &runAt, Prompt: "call mom"})
	if err != nil {
		t.Fatal(err)
	}
	return service, job
}

func TestAReminderDueWhileItsSessionIsBusyFiresOnceItIsFree(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	runner := &fakeRunner{busy: true}
	service, job := newReminder(t, runner, &now)
	ctx := context.Background()

	now = now.Add(2 * time.Minute)
	if err := service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	runner.busy = false
	if err := service.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	stored, err := service.GetJob(ctx, job.ID)
	if err != nil || len(runner.accepted) != 1 || stored.Status != automation.JobStatusCompleted {
		t.Fatalf("accepted = %d, job = %+v, %v", len(runner.accepted), stored, err)
	}
}

func TestAReminderGivesUpOnASessionBusyForAnHour(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	runner := &fakeRunner{busy: true}
	service, job := newReminder(t, runner, &now)
	ctx := context.Background()

	now = now.Add(2 * time.Minute)
	if err := service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := service.Tick(ctx); err == nil {
		t.Fatal("the fire past the retry window did not fail")
	}

	stored, err := service.GetJob(ctx, job.ID)
	if err != nil || stored.Status != automation.JobStatusCompleted || stored.NextDueAt != nil {
		t.Fatalf("job = %+v, %v", stored, err)
	}
}

func TestAJobKeepsTheCapabilitiesAndAddressOfTheRunThatCreatedIt(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	capabilities := core.ClientCapabilities{SupportsDocumentDelivery: true, ReceivesDeliveries: true}
	address := json.RawMessage(`{"kind":"chat","chat_id":-100,"message_id":42}`)
	runner := &fakeRunner{
		runs:          map[string]core.Run{"r1": {ID: "r1", SessionID: "s1", Client: "chat-app", ExternalKey: "-100", ClientCapabilities: capabilities}},
		runDeliveries: []core.ClientDelivery{{RunID: "r1", Type: core.ClientDeliveryTypeRun, Address: address}},
	}
	service := newService(t, runner, &now)
	ctx := context.Background()
	runAt := now.Add(time.Minute)
	job, err := service.CreateJobForRun(ctx, "r1", automation.CreateJobInput{ScheduleMode: automation.ScheduleModeOnce, RunAt: &runAt, Prompt: "standup"})
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Minute)
	if err := service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := service.GetJob(ctx, job.ID)
	if err != nil || stored.ClientCapabilities != capabilities || string(stored.DeliveryAddress) != string(address) {
		t.Fatalf("stored job = %+v, %v", stored, err)
	}
	if len(runner.accepted) != 1 || runner.accepted[0].ClientCapabilities != capabilities || runner.accepted[0].Client != "chat-app" {
		t.Fatalf("triggered runs = %+v", runner.accepted)
	}
	if len(runner.created) != 1 || runner.created[0].ExternalKey != "-100" || string(runner.created[0].Address) != string(address) || runner.created[0].TaskID != job.ID {
		t.Fatalf("deliveries = %+v", runner.created)
	}
}

func TestAJobDoesNotKeepAnAddressThatTakesOneReply(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	runner := &fakeRunner{
		runs:          map[string]core.Run{"r1": {ID: "r1", SessionID: "s1", Client: "chat-app", ExternalKey: "guest:q", ClientCapabilities: core.ClientCapabilities{ReceivesDeliveries: true}}},
		runDeliveries: []core.ClientDelivery{{RunID: "r1", Type: core.ClientDeliveryTypeRun, Address: json.RawMessage(`{"kind":"guest","guest_query_id":"q"}`), ReplyOnce: true}},
	}
	service := newService(t, runner, &now)
	runAt := now.Add(time.Minute)
	job, err := service.CreateJobForRun(context.Background(), "r1", automation.CreateJobInput{ScheduleMode: automation.ScheduleModeOnce, RunAt: &runAt, Prompt: "ping"})
	if err != nil || job.DeliveryAddress != nil {
		t.Fatalf("job = %+v, %v", job, err)
	}
}

func TestConfigureAppliesToLaterJobsAndFires(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	runner := &fakeRunner{}
	service, job := newReminder(t, runner, &now)
	ctx := context.Background()

	service.Configure(automation.Settings{
		Timezone: "Asia/Almaty",
		Targets:  []core.ClientDeliveryTarget{{Client: "chat-app", ExternalKey: "7"}, {Client: "chat-app", ExternalKey: "7", Address: json.RawMessage(`{"chat_id":7}`)}},
	})
	now = now.Add(2 * time.Minute)
	if err := service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(runner.created) != 1 || runner.created[0].Client != "chat-app" || runner.created[0].ExternalKey != "7" || runner.created[0].TaskID != job.ID {
		t.Fatalf("deliveries after configuring a target = %+v", runner.created)
	}
	runAt := now.Add(time.Hour)
	later, err := service.CreateJob(ctx, automation.CreateJobInput{SessionID: "s1", ScheduleMode: automation.ScheduleModeOnce, RunAt: &runAt, Prompt: "stretch"})
	if err != nil || later.Timezone != "Asia/Almaty" || job.Timezone != "UTC" {
		t.Fatalf("timezones = %q then %q, %v", job.Timezone, later.Timezone, err)
	}
}

func TestTickFiresTheJobsDueByNowAcrossTimePrecisions(t *testing.T) {
	now := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	runner := &fakeRunner{}
	service := newService(t, runner, &now)
	ctx := context.Background()
	create := func(at time.Time) automation.Job {
		job, err := service.CreateJob(ctx, automation.CreateJobInput{SessionID: "s1", ScheduleMode: automation.ScheduleModeOnce, RunAt: &at, Prompt: "at " + at.String()})
		if err != nil {
			t.Fatal(err)
		}
		return job
	}
	due := create(time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC))
	notDue := create(time.Date(2026, 10, 4, 9, 0, 0, 900_000_000, time.UTC))

	now = time.Date(2026, 10, 4, 9, 0, 0, 500_000_000, time.UTC)
	if err := service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(runner.accepted) != 1 || runner.accepted[0].Text == "" {
		t.Fatalf("fired = %+v", runner.accepted)
	}
	for _, job := range []struct {
		id   string
		want automation.JobStatus
	}{{due.ID, automation.JobStatusCompleted}, {notDue.ID, automation.JobStatusActive}} {
		stored, err := service.GetJob(ctx, job.id)
		if err != nil || stored.Status != job.want {
			t.Fatalf("job %s = %s, want %s (%v)", job.id, stored.Status, job.want, err)
		}
	}
}
