package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
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

// rawDB opens the database file behind the store, closing the store first.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
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
	if _, err := st.AppendMessage(context.Background(), message); err != nil {
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

func toolCallMessage(id string, sessionID string) transcript.Message {
	return transcript.Message{ID: id, SessionID: sessionID, RunID: "r1", Role: transcript.MessageRoleAssistant, CreatedAt: testEpoch, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: "read", Input: `{"path":"a"}`},
	}}}
}

func toolResultMessage(id string, sessionID string, toolCallID string) transcript.Message {
	return transcript.Message{ID: id, SessionID: sessionID, RunID: "r1", Role: transcript.MessageRoleTool, CreatedAt: testEpoch, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: toolCallID, Name: "read", Content: "ok"},
	}}}
}

// assistantToolResultMessage carries a tool_result part on a non-tool role, to pin
// that HasToolResult only counts role='tool' messages.
func assistantToolResultMessage(id string, sessionID string, toolCallID string) transcript.Message {
	return transcript.Message{ID: id, SessionID: sessionID, RunID: "r1", Role: transcript.MessageRoleAssistant, CreatedAt: testEpoch, Parts: []transcript.MessagePart{{
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
	saveTestMessage(t, st, assistantToolResultMessage("misfiled-result", "s2", "call-3"))

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
			sessionID, runID, toolCallID string
			want                         bool
		}{
			{"s1", "r1", "call-1", true},
			{"s1", "r2", "call-1", false},
			{"s1", "r1", "call-2", false},
			{"s2", "r1", "call-2", true},
			{"s1", "r1", "missing", false},
			{"s2", "r1", "call-3", false},
		} {
			got, err := st.HasToolResult(ctx, tc.sessionID, tc.runID, tc.toolCallID)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("HasToolResult(%s, %s, %s)=%v, want %v", tc.sessionID, tc.runID, tc.toolCallID, got, tc.want)
			}
		}
	})

	t.Run("ListRunMessages", func(t *testing.T) {
		got, err := st.ListRunMessages(ctx, "s1", "r1")
		if err != nil || len(got) != 2 || got[0].ID != "call-1" || got[1].ID != "result-1" {
			t.Fatalf("ListRunMessages(s1, r1)=%+v, %v", got, err)
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

func TestMessageOriginIsStored(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	saveTestMessage(t, st, transcript.Message{ID: "note", SessionID: "s1", RunID: "r1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "plain", SessionID: "s1", CreatedAt: testEpoch})

	stored, err := st.GetMessage(ctx, "note")
	if err != nil || stored.Origin != transcript.OriginEngine {
		t.Fatalf("stored note = %+v err = %v", stored, err)
	}
	listed, err := st.ListMessages(ctx, "s1", 0)
	if err != nil || len(listed) != 2 || listed[0].Origin != transcript.OriginEngine || listed[1].Origin != "" {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
}

func TestLatestCompactionIsTheNewestBoundaryOfTheSession(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	if _, err := st.LatestCompaction(ctx, "s1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("LatestCompaction without a boundary: err = %v, want ErrNotFound", err)
	}
	system := transcript.MessageRoleSystem
	saveTestMessage(t, st, transcript.Message{ID: "m1", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "b1", SessionID: "s1", Role: system, Content: "Context compacted.", Compaction: &transcript.Compaction{Summary: "first", CoversThroughSeq: 1}, CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "m2", SessionID: "s1", CreatedAt: testEpoch})
	want := transcript.Compaction{Summary: "second", Kept: []string{"User: task"}, CoversThroughSeq: 3, RunID: "r1", TokensBefore: 900, TokensAfter: 300}
	saveTestMessage(t, st, transcript.Message{ID: "b2", SessionID: "s1", Role: system, Content: "Context compacted.", Compaction: &want, CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "other", SessionID: "s2", Role: system, Content: "Context cleared.", Compaction: &transcript.Compaction{Cleared: true}, CreatedAt: testEpoch})

	latest, err := st.LatestCompaction(ctx, "s1")
	if err != nil || latest.ID != "b2" || latest.Compaction == nil || !reflect.DeepEqual(*latest.Compaction, want) {
		t.Fatalf("latest = %+v err = %v", latest, err)
	}
	plain, err := st.GetMessage(ctx, "m2")
	if err != nil || plain.Compaction != nil {
		t.Fatalf("plain message = %+v err = %v", plain, err)
	}
}

func TestLatestBoundaryLookupUsesTheBoundaryIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	st := openTestStore(t, path)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT id FROM messages WHERE session_id = ? AND compaction_json <> '' ORDER BY seq DESC LIMIT 1`, "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || !strings.Contains(plan[0], "idx_messages_boundaries") {
		t.Fatalf("query plan = %q", plan)
	}
}
