package core_test

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestAcceptRunRenamesMainSessionFromFirstUserMessage(t *testing.T) {
	app, sqliteStore, cleanup := newSessionTitleTestCore(t)
	defer cleanup()

	session, err := app.CreateSession(context.Background(), CreateSessionInput{
		Title:      "Main",
		WorkingDir: "/tmp/project",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := app.AcceptRun(context.Background(), HandleMessageInput{
		SessionID:  session.ID,
		Text:       "fix telegram back button in session menus",
		WorkingDir: "/tmp/project",
	}); err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}

	updated, err := sqliteStore.GetSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if updated.Title != "Fix Telegram Back Button" {
		t.Fatalf("session title = %q, want %q", updated.Title, "Fix Telegram Back Button")
	}
}

func TestAcceptRunKeepsCustomSessionTitle(t *testing.T) {
	app, sqliteStore, cleanup := newSessionTitleTestCore(t)
	defer cleanup()

	session, err := app.CreateSession(context.Background(), CreateSessionInput{
		Title:      "Release Planning",
		WorkingDir: "/tmp/project",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := app.AcceptRun(context.Background(), HandleMessageInput{
		SessionID:  session.ID,
		Text:       "fix telegram back button in session menus",
		WorkingDir: "/tmp/project",
	}); err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}

	updated, err := sqliteStore.GetSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if updated.Title != "Release Planning" {
		t.Fatalf("session title = %q, want custom title unchanged", updated.Title)
	}
}

func newSessionTitleTestCore(t *testing.T) (*Core, *store.SQLiteStore, func()) {
	t.Helper()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	app := New(sqliteStore).
		WithIDGenerator(sessionTitleTestIDs()).
		WithRunStarter(noopRunStarter{})
	return app, sqliteStore, func() { _ = sqliteStore.Close() }
}

type noopRunStarter struct{}

func (noopRunStarter) StartRun(context.Context, string) error {
	return nil
}

func sessionTitleTestIDs() func(string) string {
	var n int
	return func(prefix string) string {
		n++
		return prefix + "_title_test_" + string(rune('a'+n-1))
	}
}
