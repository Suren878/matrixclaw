package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestMessageInsertsReturnTheirSeq(t *testing.T) {
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if err := st.CreateSession(ctx, core.Session{
		ID: "s1", Title: "s1", Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw,
		PermissionMode: core.PermissionModeDefault, Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	message := func(id string) transcript.Message {
		return transcript.Message{ID: id, SessionID: "s1", Role: transcript.MessageRoleUser, Content: id, CreatedAt: now, UpdatedAt: now}
	}

	first, err := st.AppendMessage(ctx, message("m1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.SaveMessageProgress(ctx, message("m2"))
	if err != nil {
		t.Fatal(err)
	}

	if first <= 0 || second != first+1 {
		t.Fatalf("seqs = %d, %d; want consecutive positive", first, second)
	}
	stored, err := st.GetMessage(ctx, "m2")
	if err != nil || stored.Seq != second {
		t.Fatalf("stored m2 = %+v, err = %v", stored, err)
	}
}
