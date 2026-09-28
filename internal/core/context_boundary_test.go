package core_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestClearContextCoversEveryMessageSoFar(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	session, _ := saveCrashRecoveryRun(t, db, "clear", core.RunStatusCompleted, false)
	messages := sessionMessages(t, db, session.ID)

	cleared, err := app.ClearContext(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if c := cleared.Compaction; c == nil || !c.Cleared || c.CoversThroughSeq != messages[len(messages)-1].Seq || cleared.RunID != "" {
		t.Fatalf("clear boundary = %+v", cleared)
	}
	latest, err := db.LatestCompaction(context.Background(), session.ID)
	if err != nil || latest.ID != cleared.ID || latest.Seq <= cleared.Compaction.CoversThroughSeq {
		t.Fatalf("stored boundary = %+v err = %v", latest, err)
	}
}

func TestUnreadableBoundaryIsIgnored(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	db, err := store.NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	app := core.New(db)
	var seen providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		seen = request
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveNativeRunWithHistory(t, db, "broken", transcript.Message{
		ID: "msg_earlier", Role: transcript.MessageRoleUser, Content: "earlier detail", Parts: transcript.NormalizeMessageParts("earlier detail", nil),
	})
	saveRunRecoveryTestMessage(t, db, transcript.Message{
		ID: "msg_boundary", SessionID: session.ID, Role: transcript.MessageRoleSystem, Content: "Context compacted.",
		Compaction: &transcript.Compaction{Summary: "LOST", CoversThroughSeq: 1},
		CreatedAt:  runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime(),
	})
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(`UPDATE messages SET compaction_json = '{broken' WHERE id = 'msg_boundary'`); err != nil {
		t.Fatal(err)
	}

	if _, err := app.SessionContext(context.Background(), session.ID); err != nil {
		t.Fatalf("SessionContext: %v", err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if len(seen.Messages) == 0 || seen.Messages[0].Content != "earlier detail" {
		t.Fatalf("request messages = %+v, want the whole history", seen.Messages)
	}
}

func TestCompactWithNothingToSummariseIsInvalidInput(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "SUMMARY"}, nil
	})})
	now := runRecoveryTestTime()
	session := core.Session{
		ID: "session_notes", Title: "notes", Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw,
		ProviderID: "recovery-test", ModelID: "test-model", PermissionMode: core.PermissionModeDefault,
		Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateSystemMessage(context.Background(), session.ID, "Model changed."); err != nil {
		t.Fatal(err)
	}

	if _, err := app.CompactSession(context.Background(), session.ID); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("CompactSession error = %v, want ErrInvalidInput", err)
	}
}

func TestClearContextRemovesTheToolOutputsItCovers(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	files := t.TempDir()
	app.WithSessionFiles(files)
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("dump", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		return tools.Result{Content: strings.Repeat("d", 60_000)}, nil
	}}))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "dump_1", Name: "dump", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Dumped."}, nil
	})})
	session, run := saveNativeRunWithHistory(t, db, "prune")
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(files, session.ID, "tool-output")
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Fatalf("kept outputs = %v err = %v", entries, err)
	}

	if _, err := app.ClearContext(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}

	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("outputs after /clear = %v err = %v", entries, err)
	}
}
