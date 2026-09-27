package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
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
