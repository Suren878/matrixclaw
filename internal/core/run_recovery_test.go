package core_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestPersistedWorkflowCanRecoverOrphanedRunningRunInline(t *testing.T) {
	t.Parallel()
	app, sqliteStore, cleanup := newRunRecoveryTestCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: &recoveryRuntime{text: "continued inline"}})
	ctx := context.Background()
	session := saveRunRecoveryTestSession(t, sqliteStore, "session_execute_orphan", "Orphan", "/tmp")
	user := transcript.Message{
		ID:        "msg_user_execute_orphan",
		SessionID: session.ID,
		RunID:     "run_execute_orphan",
		Role:      transcript.MessageRoleUser,
		Content:   "work",
		CreatedAt: runRecoveryTestTime(),
		UpdatedAt: runRecoveryTestTime(),
	}
	run := core.Run{
		ID:            "run_execute_orphan",
		SessionID:     session.ID,
		UserMessageID: user.ID,
		Status:        core.RunStatusRunning,
		StartedAt:     runRecoveryTestTime(),
		UpdatedAt:     runRecoveryTestTime(),
	}
	if err := sqliteStore.AcceptMessage(ctx, user, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	got, err := sqliteStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != core.RunStatusCompleted {
		t.Fatalf("run status = %q (%s), want completed", got.Status, got.Error)
	}
	if got.Error != "" {
		t.Fatalf("run error = %q, want empty", got.Error)
	}
	if got.FinishedAt == nil {
		t.Fatal("FinishedAt = nil, want completed run finished timestamp")
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

func saveRunRecoveryTestMessage(t *testing.T, sqliteStore *store.SQLiteStore, message transcript.Message) {
	t.Helper()
	if _, err := sqliteStore.AppendMessage(context.Background(), message); err != nil {
		t.Fatalf("save message: %v", err)
	}
}

func runRecoveryTestTime() time.Time {
	return time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
}
