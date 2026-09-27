package store_test

import (
	"context"
	"path/filepath"
	"reflect"
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
