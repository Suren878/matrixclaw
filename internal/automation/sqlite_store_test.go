package automation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestSQLiteStorePersistsAutomationJobsAndFires(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "automation.db")
	coreStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("store.NewSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		_ = coreStore.Close()
	})

	now := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	if err := coreStore.CreateSession(ctx, core.Session{
		ID:        "session_automation",
		Title:     "Automation",
		Status:    core.SessionStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	automationStore, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = automationStore.Close()
	})

	runAt := now.Add(time.Hour)
	job := Job{
		ID:           "job_1",
		Kind:         JobKindReminder,
		Status:       JobStatusActive,
		SessionID:    "session_automation",
		Timezone:     "UTC",
		ScheduleMode: ScheduleModeOnce,
		RunAt:        &runAt,
		NextDueAt:    &runAt,
		Prompt:       "check docs",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := automationStore.CreateAutomationJob(ctx, job); err != nil {
		t.Fatalf("CreateAutomationJob() error = %v", err)
	}

	jobs, err := automationStore.ListAutomationJobs(ctx, JobFilter{Status: JobStatusActive})
	if err != nil {
		t.Fatalf("ListAutomationJobs() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v, want one active job", jobs)
	}
	job.Status = JobStatusPaused
	job.UpdatedAt = now.Add(time.Minute)
	if err := automationStore.UpdateAutomationJob(ctx, job); err != nil {
		t.Fatalf("UpdateAutomationJob() error = %v", err)
	}
	updatedJob, err := automationStore.GetAutomationJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetAutomationJob() error = %v", err)
	}
	if updatedJob.Status != JobStatusPaused {
		t.Fatalf("GetAutomationJob().Status = %q, want paused", updatedJob.Status)
	}

	fire := Fire{
		ID:           "fire_1",
		JobID:        job.ID,
		ScheduledFor: runAt,
		Status:       FireStatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := automationStore.CreateAutomationFire(ctx, fire); err != nil {
		t.Fatalf("CreateAutomationFire() error = %v", err)
	}
	finishedAt := now.Add(2 * time.Minute)
	fire.Status = FireStatusCompleted
	fire.RunID = "run_1"
	fire.UpdatedAt = finishedAt
	fire.FinishedAt = &finishedAt
	if err := automationStore.UpdateAutomationFire(ctx, fire); err != nil {
		t.Fatalf("UpdateAutomationFire() error = %v", err)
	}
	updatedFire, err := automationStore.GetAutomationFireBySchedule(ctx, job.ID, runAt)
	if err != nil {
		t.Fatalf("GetAutomationFireBySchedule() error = %v", err)
	}
	if updatedFire.Status != FireStatusCompleted || updatedFire.RunID != "run_1" {
		t.Fatalf("updated fire = %#v, want completed run_1", updatedFire)
	}
	if err := automationStore.CreateAutomationFire(ctx, Fire{ID: "fire_2", JobID: job.ID, ScheduledFor: runAt, Status: FireStatusRunning, CreatedAt: now, UpdatedAt: now}); !errors.Is(err, ErrDuplicateFire) {
		t.Fatalf("CreateAutomationFire(duplicate) error = %v, want ErrDuplicateFire", err)
	}
}
