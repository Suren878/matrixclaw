package core_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestClearContextCoversEveryMessageSoFar(t *testing.T) {
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
