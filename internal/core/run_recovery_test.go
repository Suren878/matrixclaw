package core_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestExecuteRunFailsOrphanedRunningRunInsteadOfIgnoringIt(t *testing.T) {
	app, sqliteStore, cleanup := newRunRecoveryTestCore(t)
	defer cleanup()
	ctx := context.Background()
	session := saveRunRecoveryTestSession(t, sqliteStore, "session_execute_orphan", "Orphan", "/tmp")
	user := core.Message{
		ID:        "msg_user_execute_orphan",
		SessionID: session.ID,
		RunID:     "run_execute_orphan",
		Role:      core.MessageRoleUser,
		Content:   "work",
		CreatedAt: runRecoveryTestTime(),
		UpdatedAt: runRecoveryTestTime(),
	}
	saveRunRecoveryTestMessage(t, sqliteStore, user)
	run := core.Run{
		ID:            "run_execute_orphan",
		SessionID:     session.ID,
		UserMessageID: user.ID,
		Status:        core.RunStatusRunning,
		StartedAt:     runRecoveryTestTime(),
		UpdatedAt:     runRecoveryTestTime(),
	}
	if err := sqliteStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	err := app.ExecuteRun(ctx, run.ID)
	if err == nil || !strings.Contains(err.Error(), "left running without an active executor") {
		t.Fatalf("ExecuteRun error = %v, want orphaned executor failure", err)
	}
	got, err := sqliteStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != core.RunStatusFailed {
		t.Fatalf("run status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "left running without an active executor") {
		t.Fatalf("run error = %q, want orphaned executor detail", got.Error)
	}
	if got.FinishedAt == nil {
		t.Fatal("FinishedAt = nil, want failed run finished timestamp")
	}
}

func newRunRecoveryTestCore(t *testing.T) (*core.Core, *store.SQLiteStore, func()) {
	t.Helper()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	app := core.New(sqliteStore)
	return app, sqliteStore, func() { _ = sqliteStore.Close() }
}

func saveRunRecoveryTestSession(t *testing.T, sqliteStore *store.SQLiteStore, id string, title string, workingDir string) core.Session {
	t.Helper()
	now := runRecoveryTestTime()
	session := core.Session{
		ID:             id,
		Title:          title,
		Kind:           core.SessionKindAssistant,
		RuntimeID:      core.SessionRuntimeMatrixClaw,
		WorkingDir:     workingDir,
		PermissionMode: core.PermissionModeDefault,
		Status:         core.SessionStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	return session
}

func saveRunRecoveryTestMessage(t *testing.T, sqliteStore *store.SQLiteStore, message core.Message) {
	t.Helper()
	if err := sqliteStore.SaveMessage(context.Background(), message); err != nil {
		t.Fatalf("save message: %v", err)
	}
}

func runRecoveryTestTime() time.Time {
	return time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
}
