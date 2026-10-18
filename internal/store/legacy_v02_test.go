package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
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

	markV02(t, path)
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
	markV02(t, path)
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

	markV02(t, path)
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

func TestPlanningTablesAreDroppedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TABLE session_goals`,
		`CREATE TABLE session_goals (session_id TEXT PRIMARY KEY, goal TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL)`,
		`CREATE TABLE session_plan_items (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, text TEXT NOT NULL)`,
		`CREATE INDEX idx_session_plan_items_session_position ON session_plan_items(session_id)`,
		`CREATE TABLE plan_runs (session_id TEXT PRIMARY KEY, status TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	markV02(t, path)
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}

	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('session_plan_items', 'plan_runs') OR name LIKE 'idx_session_plan_items%'`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("planning tables left = %d err = %v", left, err)
	}
	// Migration 004 makes session_goals anew.
	var goals int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('session_goals') WHERE name = 'objective'`).Scan(&goals); err != nil || goals != 1 {
		t.Fatalf("session_goals objective columns = %d, err = %v", goals, err)
	}
}

func TestSubagentTasksMoveIntoTheTasksTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE subagent_tasks (id TEXT PRIMARY KEY, mode TEXT NOT NULL DEFAULT 'blocking', parent_session_id TEXT NOT NULL,
			parent_run_id TEXT NOT NULL DEFAULT '', parent_tool_call_id TEXT NOT NULL DEFAULT '', child_session_id TEXT NOT NULL DEFAULT '',
			child_run_id TEXT NOT NULL DEFAULT '', runtime TEXT NOT NULL, goal TEXT NOT NULL, status TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', completion_queued_at TEXT, completion_delivered_at TEXT,
			completion_auto_resume_run_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, finished_at TEXT)`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, parent_run_id, parent_tool_call_id, child_run_id, runtime, goal, status, summary,
			completion_queued_at, created_at, updated_at, finished_at)
			VALUES('queued', 'async', 's1', 'r1', 'call_1', 'child_1', 'matrixclaw', 'Read the logs', 'completed', 'All good',
			'2026-09-23T10:00:05Z', '2026-09-23T10:00:00Z', '2026-09-23T10:00:05Z', '2026-09-23T10:00:05Z')`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, runtime, goal, status, completion_queued_at, completion_delivered_at,
			completion_auto_resume_run_id, created_at, updated_at, finished_at)
			VALUES('delivered', 'async', 's1', 'matrixclaw', 'Fix it', 'completed', '2026-09-23T10:00:05Z', '2026-09-23T10:00:06Z',
			'r2', '2026-09-23T10:00:00Z', '2026-09-23T10:00:06Z', '2026-09-23T10:00:05Z')`,
		`INSERT INTO subagent_tasks(id, parent_session_id, runtime, goal, status, created_at, updated_at, finished_at)
			VALUES('blocking', 's1', 'matrixclaw', 'Check', 'failed', '2026-09-23T10:00:00Z', '2026-09-23T10:00:01Z', '2026-09-23T10:00:01Z')`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, runtime, goal, status, created_at, updated_at)
			VALUES('running', 'async', 's1', 'codex', 'Build', 'running', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
		`INSERT INTO subagent_tasks(id, parent_session_id, runtime, goal, status, created_at, updated_at)
			VALUES('orphan', 'gone', 'matrixclaw', 'Lost', 'running', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	markV02(t, path)
	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		pending, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Undelivered: true})
		if err != nil || len(pending) != 1 || pending[0].ID != "queued" {
			t.Fatalf("open %d: pending = %+v, %v", reopen, pending, err)
		}
		queued, err := st.GetTask(ctx, "queued")
		if err != nil {
			t.Fatal(err)
		}
		if queued.Kind != core.TaskKindSubagent || !queued.Background || queued.RunID != "r1" || queued.ParentToolCallID != "call_1" ||
			queued.ChildRunID != "child_1" || queued.Command != "Read the logs" || queued.Summary != "All good" || queued.Isolation != core.SubagentIsolationShared {
			t.Fatalf("open %d: queued = %+v", reopen, queued)
		}
		delivered, err := st.GetTask(ctx, "delivered")
		if err != nil || delivered.DeliveredAt == nil || delivered.DeliveredRunID != "r2" {
			t.Fatalf("open %d: delivered = %+v, %v", reopen, delivered, err)
		}
		blocking, err := st.GetTask(ctx, "blocking")
		if err != nil || blocking.Background || blocking.DeliveredAt == nil {
			t.Fatalf("open %d: blocking = %+v, %v", reopen, blocking, err)
		}
		active, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Kind: core.TaskKindSubagent, Background: true, Statuses: []core.TaskStatus{core.TaskStatusRunning}})
		if err != nil || len(active) != 1 || active[0].ID != "running" || active[0].DeliveredAt != nil {
			t.Fatalf("open %d: active = %+v, %v", reopen, active, err)
		}
		if _, err := st.GetTask(ctx, "orphan"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("open %d: orphan err = %v", reopen, err)
		}
		_ = st.Close()
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var tables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'subagent_tasks'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("subagent_tasks still exists after the migration")
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
	markV02(t, path)
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

func TestOpeningDropsTheWebResearchTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE work_jobs (id TEXT PRIMARY KEY, kind TEXT NOT NULL)`,
		`CREATE TABLE work_artifacts (id TEXT PRIMARY KEY, job_id TEXT NOT NULL, FOREIGN KEY (job_id) REFERENCES work_jobs(id) ON DELETE CASCADE)`,
		`CREATE TABLE work_facts (id TEXT PRIMARY KEY, job_id TEXT NOT NULL, FOREIGN KEY (job_id) REFERENCES work_jobs(id) ON DELETE CASCADE)`,
		`CREATE INDEX idx_work_facts_job ON work_facts(job_id)`,
		`INSERT INTO work_jobs VALUES ('job', 'web_research.research')`,
		`INSERT INTO work_artifacts VALUES ('artifact', 'job')`,
		`INSERT INTO work_facts VALUES ('fact', 'job')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	markV02(t, path)
	for reopen := 0; reopen < 2; reopen++ {
		_ = openTestStore(t, path).Close()
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var left int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'work_%' OR name LIKE 'idx_work_%'`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d work tables or indexes are left", left)
	}
}

func TestOpeningDropsDeliveriesNoTerminalWillFetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deliveries.db")
	_ = openTestStore(t, path).Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, row := range [][]string{
		{"terminal_pending", "terminal:local", "pending"},
		{"terminal_sent", "terminal:local", "sent"},
		{"telegram_pending", "telegram", "pending"},
	} {
		if _, err := db.Exec(`INSERT INTO client_deliveries (id, type, client, external_key, session_id, run_id, task_id, summary, address_json, status, error, created_at, updated_at) VALUES (?, 'run', ?, 'k', 's', 'r', '', '', '', ?, '', '2026-06-10T00:00:00Z', '2026-06-10T00:00:00Z')`, row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}

	markV02(t, path)
	_ = openTestStore(t, path).Close()

	rows, err := db.Query(`SELECT id FROM client_deliveries ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var left []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(left, ",") != "telegram_pending,terminal_sent" {
		t.Fatalf("deliveries left = %v", left)
	}
}

// markV02 makes the database at path look unconverted, as v0.2.0 left it.
func markV02(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestFileVersionsAreDroppedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, path)
	if _, err := db.Exec(`CREATE TABLE file_snapshots (id TEXT PRIMARY KEY, session_id TEXT, path TEXT, content TEXT, version INTEGER, created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO file_snapshots VALUES ('f1', 's1', '/a', 'x', 0, '', '')`); err != nil {
		t.Fatal(err)
	}

	markV02(t, path)
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, db, "file_snapshots") {
		t.Fatal("file_snapshots is still there")
	}
}

func TestTelegramGuestAndInlineAddressesBecomeReplyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, path)
	for _, stmt := range []string{
		`ALTER TABLE client_deliveries DROP COLUMN reply_once`,
		`ALTER TABLE session_inputs DROP COLUMN reply_once`,
		`INSERT INTO client_deliveries(id, type, client, external_key, session_id, run_id, task_id, summary, address_json, status, error, created_at, updated_at) VALUES
			('chat', 'run', 'telegram', 'k', 's', 'r1', '', '', '{"kind":"chat","chat_id":1}', 'sent', '', '', ''),
			('guest', 'run', 'telegram', 'k', 's', 'r2', '', '', '{"kind":"guest","guest_query_id":"q"}', 'sent', '', '', ''),
			('inline', 'run', 'telegram', 'k', 's', 'r3', '', '', '{"inline_message_id":"i"}', 'sent', '', '', ''),
			('none', 'run', 'tui', 'k', 's', 'r4', '', '', '', 'sent', '', '', '')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	markV02(t, path)
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id FROM client_deliveries WHERE reply_once = 1 ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var once []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		once = append(once, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(once, ",") != "guest,inline" {
		t.Fatalf("reply-once deliveries = %v", once)
	}
}

func TestWakeupsOfRunsNotWaitingForEventsAreDroppedOnOpen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	for id, status := range map[string]core.RunStatus{"waiting": core.RunStatusWaitingEvents, "approval": core.RunStatusWaitingApproval} {
		run := core.Run{ID: id, SessionID: "s1", UserMessageID: "m_" + id, Status: status, StartedAt: testEpoch, UpdatedAt: testEpoch}
		if err := st.AcceptMessage(ctx, transcript.Message{ID: "m_" + id, SessionID: "s1", RunID: id, Role: transcript.MessageRoleUser, CreatedAt: testEpoch}, run); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveRunWakeup(ctx, core.RunWakeup{RunID: id, SessionID: "s1", WakeAt: testEpoch}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	markV02(t, path)
	reopened := openTestStore(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	if _, err := reopened.GetRunWakeup(ctx, "waiting"); err != nil {
		t.Fatalf("wakeup of the waiting run: %v", err)
	}
	if _, err := reopened.GetRunWakeup(ctx, "approval"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("wakeup of the run waiting for approval: %v", err)
	}
}

func TestSubagentApprovalsLinkToTheirTaskAndBridgeCopiesGo(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "parent")
	createTestSession(t, st, "child")
	parent := core.Run{ID: "run_parent", SessionID: "parent", UserMessageID: "m_parent", Status: core.RunStatusWaitingApproval, StartedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.AcceptMessage(ctx, transcript.Message{ID: "m_parent", SessionID: "parent", RunID: parent.ID, Role: transcript.MessageRoleUser, CreatedAt: testEpoch}, parent); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateTask(ctx, core.Task{ID: "task_1", Kind: core.TaskKindSubagent, SessionID: "parent", RunID: parent.ID, ParentToolCallID: "call-agent", AgentName: "Neo", ChildSessionID: "child", ChildRunID: "run_child", Status: core.TaskStatusWaitingApproval, StartedAt: testEpoch, UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, path)
	for _, stmt := range []string{
		`DROP INDEX idx_approvals_task`,
		`ALTER TABLE approvals DROP COLUMN task_id`,
		`INSERT INTO approvals(id, session_id, run_id, tool_call_ref, tool_name, params_json, state, requested_at) VALUES
			('child', 'child', 'run_child', 'call-mutate', 'mutate_state', '', 'pending', '2026-09-23T10:00:00Z'),
			('copy', 'parent', 'run_parent', 'call-agent', 'agent', '{"source":"subagent_approval_bridge","task_id":"task_1","child_approval_id":"child","child_run_id":"run_child"}', 'pending', '2026-09-23T10:00:01Z')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	markV02(t, path)
	reopened := openTestStore(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	approvals, err := reopened.ListApprovals(ctx, "parent", core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 || approvals[0].ID != "child" || approvals[0].TaskID != "task_1" || approvals[0].AgentName != "Neo" {
		t.Fatalf("parent's approvals = %+v, %v", approvals, err)
	}
	if run, err := reopened.GetRun(ctx, parent.ID); err != nil || run.Status != core.RunStatusRunning {
		t.Fatalf("parent run = %+v, %v; want it left running for recovery", run, err)
	}
}

func TestRestartDeliveriesBecomeHeldNotices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, path)
	if _, err := db.Exec(`INSERT INTO client_deliveries(id, type, client, external_key, session_id, run_id, task_id, summary, address_json, status, error, created_at, updated_at) VALUES
		('waiting', 'daemon_restart', 'telegram', 'k', '', '', '', 'Daemon restarted.', '{"kind":"chat","chat_id":1,"message_id":7}', 'pending', '', '', ''),
		('ready', 'daemon_restart', 'terminal', 'k', '', '', '', '', '', 'ready', '', '', ''),
		('failed', 'daemon_restart', 'telegram', 'k', '', '', '', '', '', 'failed', 'boom', '', ''),
		('other', 'notice', 'telegram', 'k', '', '', '', 'hi', '', 'pending', '', '', '')`); err != nil {
		t.Fatal(err)
	}

	markV02(t, path)
	for range 2 {
		if err := openTestStore(t, path).Close(); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query(`SELECT id, type, status, payload_json FROM client_deliveries ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := []string{}
	for rows.Next() {
		var id, kind, status, payload string
		if err := rows.Scan(&id, &kind, &status, &payload); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{id, kind, status, payload}, " "))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`failed notice failed {"replace":true}`,
		`other notice pending `,
		`ready notice sent {"replace":true}`,
		`waiting notice held {"replace":true}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("deliveries:\n%s", strings.Join(got, "\n"))
	}
}

// loadFixture builds the database of a testdata script at path.
func loadFixture(t *testing.T, path string, script string) {
	t.Helper()
	sql, err := os.ReadFile(filepath.Join("testdata", script))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB(t, path).Exec(string(sql)); err != nil {
		t.Fatal(err)
	}
}

// newSchema is the schema of a new database.
func newSchema(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "new.db")
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	return schemaOf(t, path)
}

// schemaOf lists the schema objects of the database at path, one per line.
func schemaOf(t *testing.T, path string) string {
	t.Helper()
	rows, err := rawDB(t, path).Query(`SELECT type || ' ' || name || ' ' || COALESCE(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var objects []string
	for rows.Next() {
		var object string
		if err := rows.Scan(&object); err != nil {
			t.Fatal(err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(objects, "\n")
}

func rowCounts(t *testing.T, path string, tables []string) map[string]int {
	t.Helper()
	db := rawDB(t, path)
	counts := map[string]int{}
	for _, table := range tables {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts[table] = n
	}
	return counts
}

func TestV02DatabaseConvertsToTheSchemaOfANewOne(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "matrixclaw.db")
	loadFixture(t, path, "v020.sql")
	research := filepath.Join(dir, "web-research", "res1", "page.txt")
	for _, file := range []string{research, filepath.Join(dir, "matrixclaw-workflows.db")} {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	kept := []string{"sessions", "client_bindings", "messages", "message_fts", "runs", "run_steps", "run_checkpoints", "approvals", "tasks",
		"session_inputs", "external_agent_sessions", "memories", "permission_rules", "automation_jobs", "skills", "skill_fts"}
	before := rowCounts(t, path, kept)

	st := openTestStore(t, path)
	t.Cleanup(func() { _ = st.Close() })
	if got, want := schemaOf(t, path), newSchema(t); got != want {
		t.Fatalf("converted schema:\n%s\n\nnew schema:\n%s", got, want)
	}
	if after := rowCounts(t, path, kept); !reflect.DeepEqual(after, before) {
		t.Fatalf("rows after = %v, before = %v", after, before)
	}
	if deliveries := rowCounts(t, path, []string{"client_deliveries"}); deliveries["client_deliveries"] != 1 {
		t.Fatalf("deliveries = %v, want the unfetched terminal one gone", deliveries)
	}
	backup := rawDB(t, path+".pre-v1")
	var version, orphans int
	if err := backup.QueryRow(`SELECT user_version, (SELECT COUNT(*) FROM history) FROM pragma_user_version`).Scan(&version, &orphans); err != nil || version != 0 || orphans != 1 {
		t.Fatalf("backup: version %d, history rows %d, %v", version, orphans, err)
	}
	for _, gone := range []string{filepath.Join(dir, "web-research"), filepath.Join(dir, "matrixclaw-workflows.db")} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s is still there: %v", gone, err)
		}
	}

	for id, want := range map[string]bool{"r1": true, "r2": false} {
		if run, err := st.GetRun(ctx, id); err != nil || run.ClientCapabilities.ReceivesDeliveries != want {
			t.Fatalf("run %s capabilities = %+v, %v", id, run.ClientCapabilities, err)
		}
	}
	db := rawDB(t, path)
	var inputDelivers, skillEnabled bool
	if err := db.QueryRow(`SELECT json_extract(client_capabilities_json, '$.receives_deliveries'), (SELECT enabled FROM skills) FROM session_inputs`).Scan(&inputDelivers, &skillEnabled); err != nil || !inputDelivers || !skillEnabled {
		t.Fatalf("input delivers = %v, skill enabled = %v, %v", inputDelivers, skillEnabled, err)
	}
	failed, err := st.GetMessage(ctx, "m3")
	if err != nil || !failed.Parts[0].ToolResult.IsError() {
		t.Fatalf("failed result = %+v, %v", failed.Parts, err)
	}
	var legacyFlags int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE parts_json LIKE '%is_error%'`).Scan(&legacyFlags); err != nil || legacyFlags != 0 {
		t.Fatalf("%d rows keep is_error, %v", legacyFlags, err)
	}
	big, err := st.GetMessage(ctx, "m4")
	if err != nil {
		t.Fatal(err)
	}
	if content := big.Parts[0].ToolResult.Content; agentcontext.EstimateTextTokens(content) > agentcontext.LargeOutputTokens || !strings.HasPrefix(content, "HEAD") || !strings.HasSuffix(content, "TAIL") {
		t.Fatalf("oversized result kept %d tokens", agentcontext.EstimateTextTokens(content))
	}
	if got := searchIDs(t, st, core.SearchFilter{Query: "permitted"}); got != "m3" {
		t.Fatalf("search = %q", got)
	}
}

func TestV01DatabaseConvertsToTheSchemaOfANewOne(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	loadFixture(t, path, "v010.sql")
	st := openTestStore(t, path)
	t.Cleanup(func() { _ = st.Close() })
	if got, want := schemaOf(t, path), newSchema(t); got != want {
		t.Fatalf("converted schema:\n%s\n\nnew schema:\n%s", got, want)
	}
	messages, err := st.ListMessages(ctx, "s1", 0)
	if err != nil || len(messages) != 3 || messages[2].Seq != 3 || messages[2].Compaction == nil || messages[2].Compaction.CoversThroughSeq != 2 {
		t.Fatalf("messages = %+v, %v", messages, err)
	}
	notices, err := st.ListClientDeliveries(ctx, core.ClientDeliveryFilter{})
	if err != nil || len(notices) != 1 || notices[0].Type != "notice" || notices[0].Status != "held" || !notices[0].ReplyOnce {
		t.Fatalf("deliveries = %+v, %v", notices, err)
	}
	if got := searchIDs(t, st, core.SearchFilter{Query: "world"}); got != "m1" {
		t.Fatalf("search = %q", got)
	}
}
