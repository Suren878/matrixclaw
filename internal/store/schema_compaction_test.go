package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestLegacyContextMarkersBecomeBoundaries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	system := transcript.MessageRoleSystem
	saveTestMessage(t, st, transcript.Message{ID: "old", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "compact", SessionID: "s1", Role: system, Content: "🧠 Context compacted: ~12k -> ~3.4k tokens\n\nEarlier work summarised.", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "clear", SessionID: "s1", Role: system, Content: "🧹 Context cleared\n\nContext cleared by user.", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "notice", SessionID: "s1", Role: system, Content: "Model changed.", CreatedAt: testEpoch})
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	compact, err := reopened.GetMessage(ctx, "compact")
	want := transcript.Compaction{Summary: "Earlier work summarised.", CoversThroughSeq: compact.Seq - 1, TokensBefore: 12_000, TokensAfter: 3_400}
	if err != nil || compact.Compaction == nil || !reflect.DeepEqual(*compact.Compaction, want) {
		t.Fatalf("migrated compaction = %+v err = %v", compact.Compaction, err)
	}
	latest, err := reopened.LatestCompaction(ctx, "s1")
	if err != nil || latest.ID != "clear" || !latest.Compaction.Cleared || latest.Compaction.CoversThroughSeq != latest.Seq-1 || latest.Compaction.Summary != "" {
		t.Fatalf("latest boundary = %+v err = %v", latest, err)
	}
	if notice, err := reopened.GetMessage(ctx, "notice"); err != nil || notice.Compaction != nil {
		t.Fatalf("plain notice = %+v err = %v", notice, err)
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
	if len(plan) != 1 || !strings.Contains(plan[0], "idx_messages_boundaries") {
		t.Fatalf("query plan = %q", plan)
	}
}
