package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// searchIDs returns the sorted ids of the matching messages, comma-joined.
func searchIDs(t *testing.T, st *store.SQLiteStore, filter core.SearchFilter) string {
	t.Helper()
	results, err := st.SearchMessages(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.MessageID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func TestSearchMessages(t *testing.T) {
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	saveTestMessage(t, st, transcript.Message{ID: "plain", SessionID: "s1", Content: "the zebra crossing", Provider: "openai", Model: "gpt", CreatedAt: testEpoch})
	saveTestMessage(t, st, toolResultMessage("tool", "s1", "call-1"))
	saveTestMessage(t, st, transcript.Message{ID: "other", SessionID: "s2", Content: "zebra elsewhere", CreatedAt: testEpoch})

	results, err := st.SearchMessages(context.Background(), core.SearchFilter{Query: "crossing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results=%+v, want one", results)
	}
	got := results[0]
	if got.MessageID != "plain" || got.SessionID != "s1" || got.Role != "user" || got.Snippet != "the zebra [crossing]" ||
		got.Provider != "openai" || got.Model != "gpt" || !got.CreatedAt.Equal(testEpoch) {
		t.Fatalf("result=%+v", got)
	}

	for _, tc := range []struct {
		filter core.SearchFilter
		want   string
	}{
		{core.SearchFilter{Query: "zebra"}, "other,plain"},
		{core.SearchFilter{Query: "zebra", SessionID: "s2"}, "other"},
		{core.SearchFilter{Query: "ok"}, "tool"},
		{core.SearchFilter{Query: "missing"}, ""},
	} {
		if got := searchIDs(t, st, tc.filter); got != tc.want {
			t.Errorf("SearchMessages(%+v)=%s, want %s", tc.filter, got, tc.want)
		}
	}
}

func TestUpdateMessageReplacesSearchText(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	message := transcript.Message{ID: "m1", SessionID: "s1", Content: "alpha", CreatedAt: testEpoch}
	saveTestMessage(t, st, message)
	message.Content = "beta"
	message.Parts = nil
	for i := 0; i < 2; i++ {
		if err := st.UpdateMessage(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	if got := searchIDs(t, st, core.SearchFilter{Query: "alpha"}); got != "" {
		t.Fatalf("alpha matches %s after update", got)
	}
	if got := searchIDs(t, st, core.SearchFilter{Query: "beta"}); got != "m1" {
		t.Fatalf("beta matches %q, want m1 once", got)
	}
}

func TestLegacySearchIndexIsRebuilt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	parts, err := json.Marshal(toolResultMessage("m-parts", "s1", "call-1").Parts)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DROP TABLE message_fts`,
		`CREATE VIRTUAL TABLE message_fts USING fts5(message_id UNINDEXED, session_id UNINDEXED, role, content, provider, model, tokenize = 'unicode61')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at, seq) VALUES('m-indexed', 's1', '', 'user', 'haystack', '2026-09-23T10:00:00Z', 1)`,
		`INSERT INTO messages(id, session_id, run_id, role, content, parts_json, created_at, seq) VALUES('m-parts', 's1', '', 'tool', '', '` + string(parts) + `', '2026-09-23T10:00:01Z', 2)`,
		`INSERT INTO message_fts(message_id, session_id, role, content, provider, model) VALUES('m-indexed', 's1', 'user', 'haystack', '', '')`,
		`INSERT INTO message_fts(message_id, session_id, role, content, provider, model) VALUES('gone', 's-gone', 'tool', 'haystack ok', '', '')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		haystack := searchIDs(t, st, core.SearchFilter{Query: "haystack"})
		ok := searchIDs(t, st, core.SearchFilter{Query: "ok"})
		_ = st.Close()
		if haystack != "m-indexed" || ok != "m-parts" {
			t.Fatalf("open %d: haystack=%q ok=%q, want m-indexed and m-parts", reopen, haystack, ok)
		}
		if rows := countSearchRows(t, path); rows != 2 {
			t.Fatalf("open %d: %d search rows, want 2", reopen, rows)
		}
	}
}

func TestReopenDoesNotRescanSearchIndex(t *testing.T) {
	const messages = 5000
	path := filepath.Join(t.TempDir(), "big.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := raw.Begin()
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("lorem ipsum dolor sit amet ", 8)
	for i := 1; i <= messages; i++ {
		if _, err := tx.Exec(`INSERT INTO messages(id, session_id, run_id, role, content, created_at, seq) VALUES(?, 's1', '', 'user', ?, '2026-09-23T10:00:00Z', ?)`,
			fmt.Sprintf("m%d", i), fmt.Sprintf("%s token%d", body, i), i); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	st = openTestStore(t, path)
	_ = st.Close()

	started := time.Now()
	st = openTestStore(t, path)
	elapsed := time.Since(started)
	defer func() { _ = st.Close() }()
	if elapsed > time.Second {
		t.Fatalf("reopen with %d indexed messages took %s", messages, elapsed)
	}
	if got := searchIDs(t, st, core.SearchFilter{Query: "token4999"}); got != "m4999" {
		t.Fatalf("token4999 matches %q, want m4999", got)
	}
}

func TestDeleteSessionDropsSearchRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	saveTestMessage(t, st, transcript.Message{ID: "keep", SessionID: "s1", Content: "shared", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "drop-1", SessionID: "s2", Content: "shared", CreatedAt: testEpoch})
	saveTestMessage(t, st, toolResultMessage("drop-2", "s2", "call-1"))
	if err := st.DeleteSession(context.Background(), "s2"); err != nil {
		t.Fatal(err)
	}
	got := searchIDs(t, st, core.SearchFilter{Query: "shared"})
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if got != "keep" {
		t.Fatalf("shared matches %q, want keep", got)
	}
	if rows := countSearchRows(t, path); rows != 1 {
		t.Fatalf("%d search rows after deleting s2, want 1", rows)
	}
}

func countSearchRows(t *testing.T, path string) int {
	t.Helper()
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var rows int
	if err := check.QueryRow(`SELECT COUNT(*) FROM message_fts`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}
