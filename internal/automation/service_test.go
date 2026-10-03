package automation_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

// busyRunner refuses triggers while busy, as a session with an active run does.
type busyRunner struct {
	busy     bool
	accepted int
}

func (r *busyRunner) AcceptTriggeredRun(_ context.Context, input core.HandleTriggeredRunInput) (core.AcceptRunResult, error) {
	if r.busy {
		return core.AcceptRunResult{}, fmt.Errorf("%w: the session is busy with another run", core.ErrRunActive)
	}
	r.accepted++
	return core.AcceptRunResult{SessionID: input.SessionID, Run: core.Run{ID: "run_" + input.TriggerID, SessionID: input.SessionID}}, nil
}

func newReminder(t *testing.T, runner automation.Runner, now *time.Time) (*automation.Service, automation.Job) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	db, err := store.NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	session := core.Session{ID: "s1", Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: *now, UpdatedAt: *now}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	jobs, err := automation.NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobs.Close() })
	service := automation.NewService(jobs, runner, "UTC").WithClock(func() time.Time { return *now })
	runAt := now.Add(time.Minute)
	job, err := service.CreateJob(context.Background(), automation.CreateJobInput{SessionID: session.ID, ScheduleMode: automation.ScheduleModeOnce, RunAt: &runAt, Prompt: "call mom"})
	if err != nil {
		t.Fatal(err)
	}
	return service, job
}

func TestAReminderDueWhileItsSessionIsBusyFiresOnceItIsFree(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	runner := &busyRunner{busy: true}
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
	if err != nil || runner.accepted != 1 || stored.Status != automation.JobStatusCompleted {
		t.Fatalf("accepted = %d, job = %+v, %v", runner.accepted, stored, err)
	}
}

func TestAReminderGivesUpOnASessionBusyForAnHour(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	runner := &busyRunner{busy: true}
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
