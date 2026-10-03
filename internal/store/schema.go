package store

import (
	"database/sql"
	"fmt"

	_ "embed"
)

//go:embed migrations/001_init.sql
var canonicalSchemaSQL string

func applyCanonicalSchema(db *sql.DB) error {
	if _, err := db.Exec(canonicalSchemaSQL); err != nil {
		return fmt.Errorf("store: apply canonical schema: %w", err)
	}
	if err := ensureColumn(db, "sessions", "kind", `ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'assistant'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "sessions", "runtime_id", `ALTER TABLE sessions ADD COLUMN runtime_id TEXT NOT NULL DEFAULT 'matrixclaw'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "sessions", "parent_session_id", `ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "sessions", "hidden", `ALTER TABLE sessions ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := migrateMessageSeq(db); err != nil {
		return err
	}
	if err := ensureColumn(db, "messages", "origin", `ALTER TABLE messages ADD COLUMN origin TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := migrateCompactionMarkers(db); err != nil {
		return err
	}
	if err := migrateRunUsage(db); err != nil {
		return err
	}
	if err := ensureColumn(db, "external_agent_sessions", "approval_policy", `ALTER TABLE external_agent_sessions ADD COLUMN approval_policy TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "external_agent_sessions", "sandbox", `ALTER TABLE external_agent_sessions ADD COLUMN sandbox TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    key TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("store: create memories table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_scope_workdir_updated ON memories(scope, working_dir, updated_at DESC)`); err != nil {
		return fmt.Errorf("store: create memories index: %w", err)
	}
	if err := dropRetiredTables(db); err != nil {
		return err
	}
	if err := migrateSubagentTasks(db); err != nil {
		return err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_inputs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    target_run_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    parts_json TEXT NOT NULL DEFAULT '',
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    delivery_address_json TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    consumed_run_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    consumed_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session inputs table: %w", err)
	}
	if err := ensureColumn(db, "session_inputs", "delivery_address_json", `ALTER TABLE session_inputs ADD COLUMN delivery_address_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "client_capabilities_json", `ALTER TABLE runs ADD COLUMN client_capabilities_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "stop_reason", `ALTER TABLE runs ADD COLUMN stop_reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "continues_run_id", `ALTER TABLE runs ADD COLUMN continues_run_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "trigger_kind", `ALTER TABLE runs ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "run_checkpoints", "engine_state", `ALTER TABLE run_checkpoints ADD COLUMN engine_state TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "run_checkpoints", "tool_batch", `ALTER TABLE run_checkpoints ADD COLUMN tool_batch TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "session_inputs", "client_capabilities_json", `ALTER TABLE session_inputs ADD COLUMN client_capabilities_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "client_deliveries", "payload_json", `ALTER TABLE client_deliveries ADD COLUMN payload_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "approvals", "reason", `ALTER TABLE approvals ADD COLUMN reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "tasks", "leader_start", `ALTER TABLE tasks ADD COLUMN leader_start TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "tasks", "boot_id", `ALTER TABLE tasks ADD COLUMN boot_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "approvals", "suggestion_json", `ALTER TABLE approvals ADD COLUMN suggestion_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_approvals_session_run_state ON approvals(session_id, run_id, state)`); err != nil {
		return fmt.Errorf("store: create approvals run index: %w", err)
	}
	if err := migrateApprovalTasks(db); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id, hidden)`); err != nil {
		return fmt.Errorf("store: create sessions parent index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_inputs_session_status_created ON session_inputs(session_id, status, created_at)`); err != nil {
		return fmt.Errorf("store: create session inputs status index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_inputs_target_run ON session_inputs(target_run_id, mode, status)`); err != nil {
		return fmt.Errorf("store: create session inputs target index: %w", err)
	}
	// Restart notices became held notices: a pending one waits for this start,
	// one marked ready was for a client that never took it.
	if _, err := db.Exec(`UPDATE client_deliveries
SET type = 'notice',
    status = CASE status WHEN 'pending' THEN 'held' WHEN 'ready' THEN 'sent' ELSE status END,
    payload_json = '{"replace":true}'
WHERE type = 'daemon_restart'`); err != nil {
		return fmt.Errorf("store: convert restart deliveries: %w", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET runtime_id = 'external_agent' WHERE kind = 'external_agent' AND runtime_id IN ('matrixclaw', 'codex', 'codex-app')`); err != nil {
		return fmt.Errorf("store: backfill external session runtime: %w", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET permission_mode = 'full_auto' WHERE kind = 'external_agent' AND permission_mode = 'default'`); err != nil {
		return fmt.Errorf("store: backfill external session permission mode: %w", err)
	}
	if _, err := db.Exec(`UPDATE external_agent_sessions SET approval_policy = 'never' WHERE approval_policy = ''`); err != nil {
		return fmt.Errorf("store: backfill external session approval policy: %w", err)
	}
	if _, err := db.Exec(`UPDATE external_agent_sessions SET sandbox = 'danger-full-access' WHERE sandbox = ''`); err != nil {
		return fmt.Errorf("store: backfill external session sandbox: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_budgets (
    session_id TEXT PRIMARY KEY,
    steps INTEGER,
    active_seconds INTEGER,
    tokens INTEGER,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session budgets table: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_engine_state (
    session_id TEXT PRIMARY KEY,
    engine_state TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session engine state table: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_todos (
    session_id TEXT PRIMARY KEY,
    items_json TEXT NOT NULL,
    chain_run_id TEXT NOT NULL DEFAULT '',
    updated_run_id TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session todos table: %w", err)
	}
	if err := migratePermissionRules(db); err != nil {
		return err
	}
	// Only a run waiting for events has a wakeup; older builds left others.
	if _, err := db.Exec(`DELETE FROM run_wakeups WHERE run_id NOT IN (SELECT id FROM runs WHERE status = 'waiting_events')`); err != nil {
		return fmt.Errorf("store: drop stale run wakeups: %w", err)
	}
	for _, table := range []struct{ name, address string }{{"client_deliveries", "address_json"}, {"session_inputs", "delivery_address_json"}} {
		if err := migrateReplyOnce(db, table.name, table.address); err != nil {
			return err
		}
	}
	return migrateMessageSearch(db)
}

// migrateReplyOnce adds reply_once to table and sets it where the address is
// a Telegram guest query or inline message, which a later message cannot reach.
func migrateReplyOnce(db *sql.DB, table string, address string) error {
	exists, err := hasColumn(db, table, "reply_once")
	if err != nil || exists {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN reply_once INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("store: add %s.reply_once: %w", table, err)
	}
	field := func(name string) string {
		return `COALESCE(CASE WHEN json_valid(` + address + `) THEN json_extract(` + address + `, '$.` + name + `') END, '')`
	}
	if _, err := db.Exec(`UPDATE ` + table + ` SET reply_once = 1 WHERE ` + field("kind") + ` IN ('guest', 'inline') OR ` + field("guest_query_id") + ` <> '' OR ` + field("inline_message_id") + ` <> ''`); err != nil {
		return fmt.Errorf("store: backfill %s.reply_once: %w", table, err)
	}
	return nil
}

// dropRetiredTables removes the Planning Mode tables that todo lists replaced
// and the web research job tables (children before work_jobs).
func dropRetiredTables(db *sql.DB) error {
	for _, table := range []string{"plan_runs", "session_plan_items", "session_goals", "work_facts", "work_artifacts", "work_jobs", "file_snapshots"} {
		if _, err := db.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
			return fmt.Errorf("store: drop %s: %w", table, err)
		}
	}
	return nil
}

func ensureColumn(db *sql.DB, table string, column string, alterSQL string) error {
	exists, err := hasColumn(db, table, column)
	if err != nil || exists {
		return err
	}
	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("store: add %s.%s: %w", table, column, err)
	}
	return nil
}

func hasColumn(db *sql.DB, table string, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("store: inspect %s schema: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("store: scan %s schema: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("store: iterate %s schema: %w", table, err)
	}
	return false, nil
}

// migrateApprovalTasks links each subagent's approval to its task. The copies
// older builds made of them in the parent session (params source
// subagent_approval_bridge) are dropped, and a parent run parked on such a
// copy is left running, so recovery resumes its agent call.
func migrateApprovalTasks(db *sql.DB) error {
	exists, err := hasColumn(db, "approvals", "task_id")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.Exec(`ALTER TABLE approvals ADD COLUMN task_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("store: add approvals.task_id: %w", err)
		}
		const bridge = `params_json LIKE '%"source":"subagent_approval_bridge"%'`
		for _, stmt := range []string{
			`UPDATE approvals SET task_id = COALESCE((SELECT t.id FROM tasks t WHERE t.kind = 'subagent' AND t.child_session_id = approvals.session_id), '') WHERE task_id = ''`,
			`UPDATE runs SET status = 'running' WHERE status = 'waiting_approval' AND id IN (SELECT run_id FROM approvals WHERE ` + bridge + `)`,
			`DELETE FROM approvals WHERE ` + bridge,
		} {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("store: link approvals to subagent tasks: %w", err)
			}
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_approvals_task ON approvals(task_id) WHERE task_id <> ''`); err != nil {
		return fmt.Errorf("store: create approvals task index: %w", err)
	}
	return nil
}
