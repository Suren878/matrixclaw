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
	if err := dropPlanningTables(db); err != nil {
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
	if err := ensureColumn(db, "approvals", "suggestion_json", `ALTER TABLE approvals ADD COLUMN suggestion_json TEXT NOT NULL DEFAULT ''`); err != nil {
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
	return migrateMessageSearch(db)
}

// dropPlanningTables removes the Planning Mode tables that todo lists replaced.
func dropPlanningTables(db *sql.DB) error {
	for _, table := range []string{"plan_runs", "session_plan_items", "session_goals"} {
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
