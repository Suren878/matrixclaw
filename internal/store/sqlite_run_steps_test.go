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

func createTestRun(t *testing.T, st *store.SQLiteStore, sessionID string, runID string) {
	t.Helper()
	acceptTestRun(t, st, core.Run{ID: runID, SessionID: sessionID, UserMessageID: "msg_" + runID, Status: core.RunStatusCompleted, StartedAt: testEpoch, UpdatedAt: testEpoch})
}

func acceptTestRun(t *testing.T, st *store.SQLiteStore, run core.Run) {
	t.Helper()
	user := transcript.Message{ID: run.UserMessageID, SessionID: run.SessionID, RunID: run.ID, Role: transcript.MessageRoleUser, CreatedAt: run.StartedAt, UpdatedAt: run.StartedAt}
	if err := st.AcceptMessage(context.Background(), user, run); err != nil {
		t.Fatal(err)
	}
}

func TestRunStepsAreNumberedAndAggregatedPerRun(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r1")
	createTestRun(t, st, "s1", "r2")
	for _, step := range []core.RunStep{
		{RunID: "r1", Model: "m-a", Provider: "p", PromptTokens: 100, CacheReadTokens: 80, CacheWriteTokens: 10, OutputTokens: 5, StopReason: "tool_use", ToolCalls: 2, LatencyMillis: 900, CreatedAt: testEpoch},
		{RunID: "r1", Model: "m-b", Provider: "p", PromptTokens: 120, CacheReadTokens: 100, OutputTokens: 7, ReasoningTokens: 3, StopReason: "end_turn", CreatedAt: testEpoch.Add(time.Second)},
		{RunID: "r2", Model: "m-c", Provider: "q", PromptTokens: 50, OutputTokens: 1, StopReason: "end_turn", CreatedAt: testEpoch.Add(2 * time.Second)},
	} {
		if err := st.SaveRunStep(ctx, step); err != nil {
			t.Fatal(err)
		}
	}

	steps, err := st.ListRunSteps(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Step != 1 || steps[0].ToolCalls != 2 || steps[0].LatencyMillis != 900 || steps[0].CacheWriteTokens != 10 ||
		steps[1].Step != 2 || steps[1].StopReason != "end_turn" || !steps[1].CreatedAt.Equal(testEpoch.Add(time.Second)) {
		t.Fatalf("steps=%+v", steps)
	}

	for _, tc := range []struct {
		name   string
		filter core.UsageFilter
		want   []core.UsageRecord
	}{
		{"session", core.UsageFilter{SessionID: "s1"}, []core.UsageRecord{
			{SessionID: "s1", RunID: "r1", Provider: "p", Model: "m-b", Steps: 2, PromptTokens: 220, CacheReadTokens: 180, CacheWriteTokens: 10, OutputTokens: 12, ReasoningTokens: 3, UpdatedAt: testEpoch.Add(time.Second)},
			{SessionID: "s1", RunID: "r2", Provider: "q", Model: "m-c", Steps: 1, PromptTokens: 50, OutputTokens: 1, UpdatedAt: testEpoch.Add(2 * time.Second)},
		}},
		{"run", core.UsageFilter{RunID: "r2"}, []core.UsageRecord{
			{SessionID: "s1", RunID: "r2", Provider: "q", Model: "m-c", Steps: 1, PromptTokens: 50, OutputTokens: 1, UpdatedAt: testEpoch.Add(2 * time.Second)},
		}},
		{"latest run only", core.UsageFilter{SessionID: "s1", Limit: 1}, []core.UsageRecord{
			{SessionID: "s1", RunID: "r2", Provider: "q", Model: "m-c", Steps: 1, PromptTokens: 50, OutputTokens: 1, UpdatedAt: testEpoch.Add(2 * time.Second)},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records, err := st.ListUsageRecords(ctx, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != len(tc.want) {
				t.Fatalf("records=%+v, want %+v", records, tc.want)
			}
			for i := range records {
				if !records[i].UpdatedAt.Equal(tc.want[i].UpdatedAt) {
					t.Fatalf("record %d updated_at=%s, want %s", i, records[i].UpdatedAt, tc.want[i].UpdatedAt)
				}
				records[i].UpdatedAt = tc.want[i].UpdatedAt
				if records[i] != tc.want[i] {
					t.Fatalf("record %d=%+v, want %+v", i, records[i], tc.want[i])
				}
			}
		})
	}
}

func TestLegacyRunUsageBecomesOneRunStep(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r-anthropic")
	createTestRun(t, st, "s1", "r-openai")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE run_usage (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT NOT NULL UNIQUE, message_id TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, total_tokens INTEGER NOT NULL DEFAULT 0, cached_tokens INTEGER NOT NULL DEFAULT 0, reasoning_tokens INTEGER NOT NULL DEFAULT 0, estimated INTEGER NOT NULL DEFAULT 0, provider_raw TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE INDEX idx_run_usage_session_created_at ON run_usage(session_id, created_at)`,
		`INSERT INTO run_usage(id, session_id, run_id, provider, model, input_tokens, output_tokens, total_tokens, cached_tokens, created_at) VALUES('u1', 's1', 'r-anthropic', 'anthropic-compatible', 'claude', 100, 7, 107, 40, '2026-09-23T10:00:00Z')`,
		`INSERT INTO run_usage(id, session_id, run_id, provider, model, input_tokens, output_tokens, total_tokens, cached_tokens, reasoning_tokens, created_at) VALUES('u2', 's1', 'r-openai', 'openai-compatible', 'gpt', 50, 5, 55, 10, 2, '2026-09-23T10:00:01Z')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	want := []core.UsageRecord{
		{SessionID: "s1", RunID: "r-anthropic", Provider: "anthropic-compatible", Model: "claude", Steps: 1, PromptTokens: 140, OutputTokens: 7},
		{SessionID: "s1", RunID: "r-openai", Provider: "openai-compatible", Model: "gpt", Steps: 1, PromptTokens: 50, CacheReadTokens: 10, OutputTokens: 5, ReasoningTokens: 2},
	}
	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		records, err := st.ListUsageRecords(ctx, core.UsageFilter{SessionID: "s1"})
		_ = st.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != len(want) {
			t.Fatalf("open %d: records=%+v", reopen, records)
		}
		for i := range records {
			records[i].UpdatedAt = time.Time{}
			if records[i] != want[i] {
				t.Fatalf("open %d: record %d=%+v, want %+v", reopen, i, records[i], want[i])
			}
		}
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var tables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'run_usage'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("run_usage table still exists after migration")
	}
}
