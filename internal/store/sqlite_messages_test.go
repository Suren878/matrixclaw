package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

var testEpoch = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

func openTestStore(t *testing.T, path string) *store.SQLiteStore {
	t.Helper()
	st, err := store.NewSQLite(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func newTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	st := openTestStore(t, filepath.Join(t.TempDir(), "matrixclaw.db"))
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func createTestSession(t *testing.T, st *store.SQLiteStore, id string) {
	t.Helper()
	session := core.Session{ID: id, Title: id, Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
}

func saveTestMessage(t *testing.T, st *store.SQLiteStore, message transcript.Message) {
	t.Helper()
	if message.Role == "" {
		message.Role = transcript.MessageRoleUser
	}
	if message.Content == "" {
		message.Content = message.ID
	}
	if err := st.SaveMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
}

func messageIDsAndSeqs(messages []transcript.Message) ([]string, []int64) {
	ids := make([]string, 0, len(messages))
	seqs := make([]int64, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
		seqs = append(seqs, message.Seq)
	}
	return ids, seqs
}

func TestMessagesAreOrderedByInsertionSeq(t *testing.T) {
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	saveTestMessage(t, st, transcript.Message{ID: "first", SessionID: "s1", CreatedAt: testEpoch.Add(time.Second)})
	saveTestMessage(t, st, transcript.Message{ID: "second", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "third", SessionID: "s1", CreatedAt: testEpoch.Add(500 * time.Millisecond)})

	for _, tc := range []struct {
		limit int
		want  []string
	}{
		{0, []string{"first", "second", "third"}},
		{2, []string{"second", "third"}},
	} {
		messages, err := st.ListMessages(context.Background(), "s1", tc.limit)
		if err != nil {
			t.Fatal(err)
		}
		ids, seqs := messageIDsAndSeqs(messages)
		if len(ids) != len(tc.want) {
			t.Fatalf("limit %d: ids=%v, want %v", tc.limit, ids, tc.want)
		}
		for i := range ids {
			if ids[i] != tc.want[i] || (i > 0 && seqs[i] <= seqs[i-1]) {
				t.Fatalf("limit %d: ids=%v seqs=%v, want %v with increasing seq", tc.limit, ids, seqs, tc.want)
			}
		}
	}
}

func TestLegacyMessagesGetSeqInCreatedAtOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'assistant', runtime_id TEXT NOT NULL DEFAULT 'matrixclaw', parent_session_id TEXT NOT NULL DEFAULT '', hidden INTEGER NOT NULL DEFAULT 0, working_dir TEXT NOT NULL DEFAULT '', provider_id TEXT NOT NULL DEFAULT '', model_id TEXT NOT NULL DEFAULT '', permission_mode TEXT NOT NULL DEFAULT 'default', status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, parts_json TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL DEFAULT '', FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE)`,
		`CREATE INDEX idx_messages_session_created_at ON messages(session_id, created_at)`,
		`INSERT INTO sessions(id, title, status, created_at, updated_at) VALUES('s1', 's1', 'active', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('late', 's1', '', 'user', 'late', '2026-09-23T10:00:00.5Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('early', 's1', '', 'user', 'early', '2026-09-23T10:00:00Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('tie-a', 's1', '', 'user', 'tie-a', '2026-09-23T10:00:01Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('tie-b', 's1', '', 'user', 'tie-b', '2026-09-23T10:00:01Z')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	wantIDs := []string{"early", "late", "tie-a", "tie-b"}
	wantSeqs := []int64{1, 2, 3, 4}
	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		messages, err := st.ListMessages(context.Background(), "s1", 0)
		_ = st.Close()
		if err != nil {
			t.Fatal(err)
		}
		ids, seqs := messageIDsAndSeqs(messages)
		for i := range wantIDs {
			if len(ids) != len(wantIDs) || ids[i] != wantIDs[i] || seqs[i] != wantSeqs[i] {
				t.Fatalf("open %d: ids=%v seqs=%v, want %v %v", reopen, ids, seqs, wantIDs, wantSeqs)
			}
		}
	}

	st := openTestStore(t, path)
	defer func() { _ = st.Close() }()
	saveTestMessage(t, st, transcript.Message{ID: "new", SessionID: "s1", CreatedAt: testEpoch})
	messages, err := st.ListMessages(context.Background(), "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "new" || messages[0].Seq != 5 {
		t.Fatalf("new message=%+v, want id new seq 5", messages)
	}
}
