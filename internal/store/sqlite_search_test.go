package store_test

import (
	"context"
	"sort"
	"strings"
	"testing"

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
