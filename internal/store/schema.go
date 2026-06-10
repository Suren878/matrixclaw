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
	if err := ensureColumn(db, "session_plan_items", "parent_id", `ALTER TABLE session_plan_items ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_plan_items_session_parent_position ON session_plan_items(session_id, parent_id, position)`); err != nil {
		return fmt.Errorf("store: create session plan parent index: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS plan_runs (
    session_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    current_item_id TEXT NOT NULL DEFAULT '',
    last_run_id TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    step_no INTEGER NOT NULL DEFAULT 0,
    attempt INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create plan runs table: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS subagent_tasks (
    id TEXT PRIMARY KEY,
    agent_name TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'blocking',
    isolation TEXT NOT NULL DEFAULT 'shared',
    parent_session_id TEXT NOT NULL,
    parent_run_id TEXT NOT NULL DEFAULT '',
    parent_tool_call_id TEXT NOT NULL DEFAULT '',
    child_session_id TEXT NOT NULL DEFAULT '',
    child_run_id TEXT NOT NULL DEFAULT '',
    runtime TEXT NOT NULL,
    goal TEXT NOT NULL,
    status TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    result_message_id TEXT NOT NULL DEFAULT '',
    completion_queued_at TEXT,
    completion_delivered_at TEXT,
    completion_auto_resume_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    FOREIGN KEY (parent_session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create subagent tasks table: %w", err)
	}
	if err := ensureColumn(db, "subagent_tasks", "agent_name", `ALTER TABLE subagent_tasks ADD COLUMN agent_name TEXT NOT NULL DEFAULT ''`); err != nil {
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
	if err := ensureColumn(db, "subagent_tasks", "display_name", `ALTER TABLE subagent_tasks ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "session_inputs", "delivery_address_json", `ALTER TABLE session_inputs ADD COLUMN delivery_address_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "client_deliveries", "payload_json", `ALTER TABLE client_deliveries ADD COLUMN payload_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "subagent_tasks", "mode", `ALTER TABLE subagent_tasks ADD COLUMN mode TEXT NOT NULL DEFAULT 'blocking'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "subagent_tasks", "isolation", `ALTER TABLE subagent_tasks ADD COLUMN isolation TEXT NOT NULL DEFAULT 'shared'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "subagent_tasks", "result_message_id", `ALTER TABLE subagent_tasks ADD COLUMN result_message_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "subagent_tasks", "completion_queued_at", `ALTER TABLE subagent_tasks ADD COLUMN completion_queued_at TEXT`); err != nil {
		return err
	}
	if err := ensureColumn(db, "subagent_tasks", "completion_delivered_at", `ALTER TABLE subagent_tasks ADD COLUMN completion_delivered_at TEXT`); err != nil {
		return err
	}
	if err := ensureColumn(db, "subagent_tasks", "completion_auto_resume_run_id", `ALTER TABLE subagent_tasks ADD COLUMN completion_auto_resume_run_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id, hidden)`); err != nil {
		return fmt.Errorf("store: create sessions parent index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_subagent_tasks_parent ON subagent_tasks(parent_session_id, parent_run_id, parent_tool_call_id)`); err != nil {
		return fmt.Errorf("store: create subagent tasks parent index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_subagent_tasks_child_run ON subagent_tasks(child_run_id)`); err != nil {
		return fmt.Errorf("store: create subagent tasks child run index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_inputs_session_status_created ON session_inputs(session_id, status, created_at)`); err != nil {
		return fmt.Errorf("store: create session inputs status index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_inputs_target_run ON session_inputs(target_run_id, mode, status)`); err != nil {
		return fmt.Errorf("store: create session inputs target index: %w", err)
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
INSERT INTO message_fts(message_id, session_id, role, content, provider, model)
SELECT m.id, m.session_id, m.role, m.content, m.provider, m.model
FROM messages m
WHERE NOT EXISTS (
    SELECT 1 FROM message_fts f WHERE f.message_id = m.id
)`); err != nil {
		return fmt.Errorf("store: backfill message search: %w", err)
	}
	return nil
}

func ensureColumn(db *sql.DB, table string, column string, alterSQL string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("store: inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("store: scan %s schema: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate %s schema: %w", table, err)
	}
	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("store: add %s.%s: %w", table, column, err)
	}
	return nil
}
