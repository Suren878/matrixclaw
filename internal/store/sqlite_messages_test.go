package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
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

func toolCallMessage(id string, sessionID string) transcript.Message {
	return transcript.Message{ID: id, SessionID: sessionID, Role: transcript.MessageRoleAssistant, CreatedAt: testEpoch, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: "read", Input: `{"path":"a"}`},
	}}}
}

func toolResultMessage(id string, sessionID string, toolCallID string) transcript.Message {
	return transcript.Message{ID: id, SessionID: sessionID, Role: transcript.MessageRoleTool, CreatedAt: testEpoch, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: toolCallID, Name: "read", Content: "ok"},
	}}}
}

func TestMessagePointLookups(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	// Saved in this order, so seq is 1..5.
	saveTestMessage(t, st, transcript.Message{ID: "user", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, toolCallMessage("call-1", "s1"))
	saveTestMessage(t, st, toolResultMessage("result-1", "s1", "call-1"))
	saveTestMessage(t, st, toolResultMessage("result-2", "s2", "call-2"))
	saveTestMessage(t, st, transcript.Message{ID: "answer", SessionID: "s1", Role: transcript.MessageRoleAssistant, CreatedAt: testEpoch})

	t.Run("GetMessage", func(t *testing.T) {
		got, err := st.GetMessage(ctx, "call-1")
		if err != nil {
			t.Fatal(err)
		}
		if got.SessionID != "s1" || got.Seq != 2 || len(got.Parts) != 1 || got.Parts[0].ToolCall == nil || got.Parts[0].ToolCall.Input != `{"path":"a"}` {
			t.Fatalf("GetMessage(call-1)=%+v", got)
		}
		if _, err := st.GetMessage(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("GetMessage(missing) error=%v, want ErrNotFound", err)
		}
	})

	t.Run("HasToolResult", func(t *testing.T) {
		for _, tc := range []struct {
			sessionID, toolCallID string
			want                  bool
		}{
			{"s1", "call-1", true},
			{"s1", "call-2", false},
			{"s2", "call-2", true},
			{"s1", "missing", false},
		} {
			got, err := st.HasToolResult(ctx, tc.sessionID, tc.toolCallID)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("HasToolResult(%s, %s)=%v, want %v", tc.sessionID, tc.toolCallID, got, tc.want)
			}
		}
	})

	t.Run("ListMessagesAfter", func(t *testing.T) {
		for _, tc := range []struct {
			afterSeq int64
			limit    int
			want     []string
		}{
			{0, 0, []string{"user", "call-1", "result-1", "answer"}},
			{2, 0, []string{"result-1", "answer"}},
			{1, 2, []string{"call-1", "result-1"}},
			{5, 0, nil},
		} {
			messages, err := st.ListMessagesAfter(ctx, "s1", tc.afterSeq, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			ids, _ := messageIDsAndSeqs(messages)
			if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
				t.Errorf("ListMessagesAfter(s1, %d, %d)=%v, want %v", tc.afterSeq, tc.limit, ids, tc.want)
			}
		}
	})
}
