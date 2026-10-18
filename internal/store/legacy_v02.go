package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// convertV02 turns a database of v0.2.0 or earlier into schema version 1,
// once. It first copies the database to <path>.pre-v1, then runs the frozen
// upgrade chain, rebuilds every table as 001_init.sql declares it, dropping
// what the schema no longer has, and rewrites rows only older code could read.
func convertV02(db *sql.DB, path string) error {
	backup := path + ".pre-v1"
	if err := backupOnce(db, backup); err != nil {
		return err
	}
	if err := upgradeV02(db); err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: convert database: %w", err)
	}
	defer func() { _ = conn.Close() }()
	// Foreign keys can only be switched outside a transaction.
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("store: convert database: %w", err)
	}
	dropped, err := rebuildV1(ctx, conn)
	if _, fkErr := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err == nil && fkErr != nil {
		err = fmt.Errorf("store: convert database: %w", fkErr)
	}
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("store: compact converted database: %w", err)
	}
	removeRetiredFiles(filepath.Dir(path))
	log.Printf("store: converted %s to schema version 1 (backup %s); dropped %s", path, backup, strings.Join(dropped, ", "))
	return nil
}

// backupOnce copies the database to backup unless an earlier, interrupted
// conversion already did.
func backupOnce(db *sql.DB, backup string) error {
	if _, err := os.Stat(backup); err == nil {
		log.Printf("store: keeping the existing backup %s", backup)
		return nil
	}
	if _, err := db.Exec(`VACUUM INTO ?`, backup); err != nil {
		return fmt.Errorf("store: back up database to %s: %w", backup, err)
	}
	if err := os.Chmod(backup, 0o600); err != nil {
		return fmt.Errorf("store: secure backup %s: %w", backup, err)
	}
	return nil
}

// v02SearchTables are the FTS5 tables (with their shadow tables) the
// conversion keeps as they are; 001_init.sql declares them unchanged.
var v02SearchTables = []string{"message_fts", "skill_fts"}

// rebuildV1 moves every table aside, creates the schema of 001_init.sql and
// copies the rows back by column name, in one transaction. It returns the
// tables and columns it dropped.
func rebuildV1(ctx context.Context, conn *sql.Conn) ([]string, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin conversion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	type object struct{ kind, name string }
	objects, err := queryAll(ctx, tx, "schema objects", func(row rowScanner) (object, error) {
		var o object
		return o, row.Scan(&o.kind, &o.name)
	}, `SELECT type, name FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for _, o := range objects {
		switch {
		case o.kind == "index" || o.kind == "trigger":
			if _, err := tx.ExecContext(ctx, `DROP `+strings.ToUpper(o.kind)+` `+quoteIdent(o.name)); err != nil {
				return nil, fmt.Errorf("store: drop %s %s: %w", o.kind, o.name, err)
			}
		case o.kind == "table" && !isV02SearchTable(o.name):
			if _, err := tx.ExecContext(ctx, `ALTER TABLE `+quoteIdent(o.name)+` RENAME TO `+quoteIdent("v02_"+o.name)); err != nil {
				return nil, fmt.Errorf("store: move table %s aside: %w", o.name, err)
			}
			tables = append(tables, o.name)
		}
	}
	if _, err := tx.ExecContext(ctx, initSchemaSQL); err != nil {
		return nil, fmt.Errorf("store: create schema version 1: %w", err)
	}
	var dropped []string
	for _, table := range tables {
		old, err := txColumns(ctx, tx, "v02_"+table)
		if err != nil {
			return nil, err
		}
		current, err := txColumns(ctx, tx, table)
		if err != nil {
			return nil, err
		}
		if len(current) == 0 {
			dropped = append(dropped, table)
		} else {
			var common []string
			for _, column := range old {
				if slices.Contains(current, column) {
					common = append(common, quoteIdent(column))
				} else {
					dropped = append(dropped, table+"."+column)
				}
			}
			list := strings.Join(common, ", ")
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+quoteIdent(table)+`(`+list+`) SELECT `+list+` FROM `+quoteIdent("v02_"+table)); err != nil {
				return nil, fmt.Errorf("store: copy %s: %w", table, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+quoteIdent("v02_"+table)); err != nil {
			return nil, fmt.Errorf("store: drop old %s: %w", table, err)
		}
	}
	if err := rewriteV02Rows(ctx, tx); err != nil {
		return nil, err
	}
	if err := dropOrphanRows(ctx, tx); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 1`); err != nil {
		return nil, fmt.Errorf("store: set schema version 1: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit conversion: %w", err)
	}
	return dropped, nil
}

func isV02SearchTable(name string) bool {
	for _, table := range v02SearchTables {
		if name == table || strings.HasPrefix(name, table+"_") {
			return true
		}
	}
	return false
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// txColumns names the columns of table in order; none when it does not exist.
func txColumns(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	return queryAll(ctx, tx, table+" columns", func(row rowScanner) (string, error) {
		var name string
		return name, row.Scan(&name)
	}, `SELECT name FROM pragma_table_info(?)`, table)
}

// rewriteV02Rows rewrites what only older code could read: client
// capabilities without receives_deliveries, which only Telegram wrote, and
// tool results with is_error or written before large outputs went to files.
func rewriteV02Rows(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"runs", "session_inputs"} {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET client_capabilities_json = json_set(client_capabilities_json, '$.receives_deliveries', json('true'))
WHERE json_valid(client_capabilities_json) AND json_type(client_capabilities_json, '$.receives_deliveries') IS NULL
  AND (json_extract(client_capabilities_json, '$.supports_voice_delivery') OR json_extract(client_capabilities_json, '$.supports_document_delivery'))`); err != nil {
			return fmt.Errorf("store: rewrite %s client capabilities: %w", table, err)
		}
	}
	type stored struct{ id, parts string }
	rows, err := queryAll(ctx, tx, "tool results", func(row rowScanner) (stored, error) {
		var s stored
		return s, row.Scan(&s.id, &s.parts)
	}, `SELECT id, parts_json FROM messages WHERE parts_json LIKE '%"tool_result"%'`)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var legacy []struct {
			ToolResult *struct {
				IsError bool `json:"is_error"`
			} `json:"tool_result"`
		}
		if err := json.Unmarshal([]byte(row.parts), &legacy); err != nil {
			continue
		}
		parts := unmarshalMessageParts(row.parts)
		changed := strings.Contains(row.parts, `"is_error"`)
		for i, part := range parts {
			result := part.ToolResult
			if result == nil {
				continue
			}
			if result.Status == "" && legacy[i].ToolResult != nil && legacy[i].ToolResult.IsError {
				result.Status = "error"
			}
			if content := strings.TrimSpace(result.Content); agentcontext.EstimateTextTokens(content) > agentcontext.LargeOutputTokens {
				result.Content = agentcontext.HeadTail(content, agentcontext.LargeOutputTokens)
				changed = true
			}
		}
		if !changed {
			continue
		}
		body, err := json.Marshal(parts)
		if err != nil {
			return fmt.Errorf("store: rewrite tool result %s: %w", row.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET parts_json = ? WHERE id = ?`, string(body), row.id); err != nil {
			return fmt.Errorf("store: rewrite tool result %s: %w", row.id, err)
		}
		message, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = ?`, row.id))
		if err != nil {
			return fmt.Errorf("store: reread tool result %s: %w", row.id, err)
		}
		if err := upsertMessageSearch(ctx, tx, message); err != nil {
			return err
		}
	}
	return nil
}

// dropOrphanRows deletes rows whose parent row is gone, which tables that
// gained their foreign key later could hold; a deleted row's own children
// go in the next pass.
func dropOrphanRows(ctx context.Context, tx *sql.Tx) error {
	type orphan struct {
		table string
		rowid int64
	}
	for {
		orphans, err := queryAll(ctx, tx, "orphan rows", func(row rowScanner) (orphan, error) {
			var o orphan
			var parent string
			var fk int
			return o, row.Scan(&o.table, &o.rowid, &parent, &fk)
		}, `PRAGMA foreign_key_check`)
		if err != nil {
			return err
		}
		if len(orphans) == 0 {
			return nil
		}
		for _, o := range orphans {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+quoteIdent(o.table)+` WHERE rowid = ?`, o.rowid); err != nil {
				return fmt.Errorf("store: drop orphan row of %s: %w", o.table, err)
			}
		}
		log.Printf("store: dropped %d rows whose parent row was gone", len(orphans))
	}
}

// removeRetiredFiles deletes what retired features left next to the
// database: the go-workflows database and the web research page files.
func removeRetiredFiles(dir string) {
	for _, name := range []string{"matrixclaw-workflows.db", "matrixclaw-workflows.db-wal", "matrixclaw-workflows.db-shm"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			log.Printf("store: remove retired %s: %v", name, err)
		}
	}
	research := filepath.Join(dir, "web-research")
	if info, err := os.Lstat(research); err == nil && info.IsDir() {
		if err := os.RemoveAll(research); err != nil {
			log.Printf("store: remove retired %s: %v", research, err)
		}
	}
}

// upgradeV02 is the upgrade chain v0.2.0 ran on every open, frozen: it brings
// a database of any earlier version to the v0.2.0 shape.
func upgradeV02(db *sql.DB) error {
	if _, err := db.Exec(v02InitSQL); err != nil {
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
	// Terminals follow runs through events and never fetched their run
	// deliveries, which piled up pending.
	if _, err := db.Exec(`DELETE FROM client_deliveries WHERE status = 'pending' AND client LIKE 'terminal%'`); err != nil {
		return fmt.Errorf("store: drop unfetched terminal deliveries: %w", err)
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
	columns, err := tableColumns(db, table)
	return columns[strings.ToLower(column)], err
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

func migratePermissionRules(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS permission_rules (
    id TEXT PRIMARY KEY,
    tool TEXT NOT NULL,
    pattern TEXT NOT NULL DEFAULT '',
    effect TEXT NOT NULL,
    scope TEXT NOT NULL,
    session_id TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create permission rules table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_permission_rules_session ON permission_rules(session_id)`); err != nil {
		return fmt.Errorf("store: create permission rules session index: %w", err)
	}
	return nil
}

// migrateMessageSeq numbers messages database-wide; rows written before seq
// existed are numbered by parsed created_at, then rowid.
func migrateMessageSeq(db *sql.DB) error {
	if err := ensureColumn(db, "messages", "seq", `ALTER TABLE messages ADD COLUMN seq INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := backfillMessageSeq(db); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_messages_session_created_at`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_seq ON messages(seq)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_seq ON messages(session_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_run ON messages(session_id, run_id, seq)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("store: index message seq: %w", err)
		}
	}
	return nil
}

func backfillMessageSeq(db *sql.DB) error {
	type unsequenced struct {
		rowid     int64
		createdAt time.Time
	}
	rows, err := db.Query(`SELECT rowid, created_at FROM messages WHERE seq = 0 ORDER BY rowid`)
	if err != nil {
		return fmt.Errorf("store: list unsequenced messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pending []unsequenced
	for rows.Next() {
		var row unsequenced
		var createdAt string
		if err := rows.Scan(&row.rowid, &createdAt); err != nil {
			return fmt.Errorf("store: scan unsequenced message: %w", err)
		}
		row.createdAt = mustParseTime(createdAt)
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate unsequenced messages: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].createdAt.Before(pending[j].createdAt) })

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin message seq backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var next int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM messages`).Scan(&next); err != nil {
		return fmt.Errorf("store: read max message seq: %w", err)
	}
	for _, row := range pending {
		next++
		if _, err := tx.Exec(`UPDATE messages SET seq = ? WHERE rowid = ?`, next, row.rowid); err != nil {
			return fmt.Errorf("store: backfill message seq: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit message seq backfill: %w", err)
	}
	return nil
}

// migrateMessageSearch keys message_fts rows by messages.seq, rebuilding an
// index still keyed by message_id, drops the row of every deleted message
// (FTS5 tables do not cascade) and indexes messages that have no row.
func migrateMessageSearch(db *sql.DB) error {
	legacy, err := hasColumn(db, "message_fts", "message_id")
	if err != nil {
		return err
	}
	if legacy {
		if _, err := db.Exec(`DROP TABLE message_fts`); err != nil {
			return fmt.Errorf("store: drop legacy message search: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS message_fts USING fts5(role, content, provider, model, tokenize = 'unicode61')`); err != nil {
		return fmt.Errorf("store: create message search: %w", err)
	}
	if _, err := db.Exec(`
CREATE TRIGGER IF NOT EXISTS messages_search_delete AFTER DELETE ON messages BEGIN
    DELETE FROM message_fts WHERE rowid = old.seq;
END`); err != nil {
		return fmt.Errorf("store: create message search delete trigger: %w", err)
	}
	return indexUnsearchedMessages(db)
}

// migrateSubagentTasks copies the rows of subagent_tasks into tasks and drops
// it. A finished task never queued for its parent counts as delivered; columns
// that older databases lack take their defaults.
func migrateSubagentTasks(db *sql.DB) error {
	columns, err := tableColumns(db, "subagent_tasks")
	if err != nil || len(columns) == 0 {
		return err
	}
	column := func(name, fallback string) string {
		if columns[name] {
			return name
		}
		return fallback
	}
	queued := column("completion_queued_at", "NULL")
	copyRows := `
INSERT OR IGNORE INTO tasks(id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal,
    description, background, agent_name, runtime, isolation, child_session_id, child_run_id, summary, error,
    delivered_at, delivered_run_id, started_at, updated_at, finished_at)
SELECT id, parent_session_id, parent_run_id, parent_tool_call_id, 'subagent', status, goal,
    ` + column("display_name", "''") + `,
    CASE WHEN ` + column("mode", "'blocking'") + ` = 'async' THEN 1 ELSE 0 END,
    ` + column("agent_name", "''") + `, runtime, ` + column("isolation", "'shared'") + `,
    child_session_id, child_run_id, summary, error,
    COALESCE(` + column("completion_delivered_at", "NULL") + `, CASE WHEN ` + queued + ` IS NULL THEN finished_at END),
    ` + column("completion_auto_resume_run_id", "''") + `, created_at, updated_at, finished_at
FROM subagent_tasks
WHERE parent_session_id IN (SELECT id FROM sessions)`
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin subagent_tasks migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(copyRows); err != nil {
		return fmt.Errorf("store: copy subagent_tasks into tasks: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE subagent_tasks`); err != nil {
		return fmt.Errorf("store: drop subagent_tasks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit subagent_tasks migration: %w", err)
	}
	return nil
}

// tableColumns names the columns of table; none when it does not exist.
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("store: inspect %s schema: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: scan %s schema: %w", table, err)
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}

// migrateRunUsage turns each legacy run_usage aggregate into one run step and
// drops the table. Legacy Anthropic input excluded its cached tokens.
func migrateRunUsage(db *sql.DB) error {
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'run_usage'`).Scan(&tables); err != nil {
		return fmt.Errorf("store: inspect run_usage: %w", err)
	}
	if tables == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin run_usage migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT OR IGNORE INTO run_steps(run_id, step, model, provider, prompt_tokens, cache_read_tokens,
    output_tokens, reasoning_tokens, stop_reason, created_at)
SELECT u.run_id, 1, u.model, u.provider,
       CASE WHEN u.provider = 'anthropic-compatible' THEN u.input_tokens + u.cached_tokens ELSE u.input_tokens END,
       CASE WHEN u.provider = 'anthropic-compatible' THEN 0 ELSE u.cached_tokens END,
       u.output_tokens, u.reasoning_tokens, 'legacy', u.created_at
FROM run_usage u
JOIN runs r ON r.id = u.run_id`); err != nil {
		return fmt.Errorf("store: copy run_usage into run_steps: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE run_usage`); err != nil {
		return fmt.Errorf("store: drop run_usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit run_usage migration: %w", err)
	}
	return nil
}

// Text prefixes older versions gave the system messages that marked a
// compaction or a /clear.
const (
	legacyCompactPrefix = "🧠 Context compacted"
	legacyClearPrefix   = "🧹 Context cleared"
)

var legacyCompactStats = regexp.MustCompile(`~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s*->\s*~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s+tokens`)

// migrateCompactionMarkers adds messages.compaction_json with an index of the
// boundaries and turns the text markers older versions wrote into boundaries;
// such a marker covered every message of its session written before it.
func migrateCompactionMarkers(db *sql.DB) error {
	if err := ensureColumn(db, "messages", "compaction_json", `ALTER TABLE messages ADD COLUMN compaction_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_boundaries ON messages(session_id, seq) WHERE compaction_json <> ''`); err != nil {
		return fmt.Errorf("store: index context boundaries: %w", err)
	}
	rows, err := db.Query(`SELECT id, seq, content FROM messages WHERE role = 'system' AND compaction_json = '' AND (content LIKE ? OR content LIKE ?)`, legacyCompactPrefix+"%", legacyClearPrefix+"%")
	if err != nil {
		return fmt.Errorf("store: list legacy context markers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	updates := map[string]string{}
	for rows.Next() {
		var id, content string
		var seq int64
		if err := rows.Scan(&id, &seq, &content); err != nil {
			return fmt.Errorf("store: scan legacy context marker: %w", err)
		}
		if compaction, ok := legacyCompaction(seq, content); ok {
			updates[id] = marshalCompaction(&compaction)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate legacy context markers: %w", err)
	}
	for id, body := range updates {
		if _, err := db.Exec(`UPDATE messages SET compaction_json = ? WHERE id = ?`, body, id); err != nil {
			return fmt.Errorf("store: migrate legacy context marker: %w", err)
		}
	}
	return nil
}

func legacyCompaction(seq int64, content string) (transcript.Compaction, bool) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, legacyClearPrefix) {
		return transcript.Compaction{CoversThroughSeq: seq - 1, Cleared: true}, true
	}
	rest, ok := strings.CutPrefix(content, legacyCompactPrefix)
	if !ok {
		return transcript.Compaction{}, false
	}
	compaction := transcript.Compaction{CoversThroughSeq: seq - 1}
	rest = strings.TrimSpace(rest)
	if header, summary, found := strings.Cut(rest, "\n\n"); found && strings.HasPrefix(header, ":") {
		if match := legacyCompactStats.FindStringSubmatch(header); match != nil {
			compaction.TokensBefore = legacyTokenCount(match[1])
			compaction.TokensAfter = legacyTokenCount(match[2])
		}
		rest = summary
	}
	compaction.Summary = strings.TrimSpace(rest)
	return compaction, true
}

func legacyTokenCount(value string) int {
	value = strings.ToLower(strings.TrimSpace(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "k"):
		multiplier, value = 1_000, strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "m"):
		multiplier, value = 1_000_000, strings.TrimSuffix(value, "m")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return int(math.Round(parsed * multiplier))
}

// v02InitSQL is 001_init.sql as v0.2.0 shipped it.
const v02InitSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'assistant',
    runtime_id TEXT NOT NULL DEFAULT 'matrixclaw',
    parent_session_id TEXT NOT NULL DEFAULT '',
    hidden INTEGER NOT NULL DEFAULT 0,
    working_dir TEXT NOT NULL DEFAULT '',
    provider_id TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    permission_mode TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    parent_tool_call_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    command_or_goal TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    background INTEGER NOT NULL DEFAULT 1,
    readonly INTEGER NOT NULL DEFAULT 0,
    agent_name TEXT NOT NULL DEFAULT '',
    runtime TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    isolation TEXT NOT NULL DEFAULT '',
    pid INTEGER NOT NULL DEFAULT 0,
    pgid INTEGER NOT NULL DEFAULT 0,
    leader_start TEXT NOT NULL DEFAULT '',
    boot_id TEXT NOT NULL DEFAULT '',
    output_path TEXT NOT NULL DEFAULT '',
    exit_code INTEGER,
    output_cursor INTEGER NOT NULL DEFAULT 0,
    child_session_id TEXT NOT NULL DEFAULT '',
    child_run_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    result_message_id TEXT NOT NULL DEFAULT '',
    delivered_at TEXT,
    delivered_run_id TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS client_bindings (
    client TEXT NOT NULL,
    external_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (client, external_key),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    parts_json TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT '',
    seq INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    key TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    user_message_id TEXT NOT NULL,
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    client_capabilities_json TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_checkpoints (
    run_id TEXT PRIMARY KEY,
    recovery_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_wakeups (
    run_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    wake_at INTEGER NOT NULL,
    task_ids_json TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

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
    client_capabilities_json TEXT NOT NULL DEFAULT '',
    delivery_address_json TEXT NOT NULL DEFAULT '',
    reply_once INTEGER NOT NULL DEFAULT 0,
    working_dir TEXT NOT NULL DEFAULT '',
    consumed_run_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    consumed_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_steps (
    run_id TEXT NOT NULL,
    step INTEGER NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    stop_reason TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    tool_calls INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (run_id, step),
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS approvals (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    tool_call_ref TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    params_json TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    requested_at TEXT NOT NULL,
    decided_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS client_deliveries (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    client TEXT NOT NULL,
    external_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    summary TEXT NOT NULL,
    address_json TEXT NOT NULL,
    reply_once INTEGER NOT NULL DEFAULT 0,
    payload_json TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE TABLE IF NOT EXISTS automation_jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    session_id TEXT NOT NULL,
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'UTC',
    schedule_mode TEXT NOT NULL,
    run_at TEXT,
    interval_seconds INTEGER NOT NULL DEFAULT 0,
    cron_expr TEXT NOT NULL DEFAULT '',
    next_due_at TEXT,
    last_scheduled_for TEXT,
    prompt TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS automation_fires (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    scheduled_for TEXT NOT NULL,
    status TEXT NOT NULL,
    result_state TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT,
    UNIQUE(job_id, scheduled_for),
    FOREIGN KEY (job_id) REFERENCES automation_jobs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS external_agent_sessions (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    external_thread_id TEXT NOT NULL,
    external_session_id TEXT NOT NULL DEFAULT '',
    cwd TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    approval_policy TEXT NOT NULL DEFAULT '',
    sandbox TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tasks_session_status
    ON tasks(session_id, status);

CREATE INDEX IF NOT EXISTS idx_tasks_parent_call
    ON tasks(session_id, run_id, parent_tool_call_id);

CREATE INDEX IF NOT EXISTS idx_tasks_child_run
    ON tasks(child_run_id);

CREATE INDEX IF NOT EXISTS idx_tasks_child_session
    ON tasks(child_session_id);

CREATE INDEX IF NOT EXISTS idx_run_wakeups_wake_at
    ON run_wakeups(wake_at);

CREATE INDEX IF NOT EXISTS idx_runs_session_started_at
    ON runs(session_id, started_at);

CREATE INDEX IF NOT EXISTS idx_session_inputs_session_status_created
    ON session_inputs(session_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_session_inputs_target_run
    ON session_inputs(target_run_id, mode, status);

CREATE INDEX IF NOT EXISTS idx_memories_scope_workdir_updated
    ON memories(scope, working_dir, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_client_deliveries_status
    ON client_deliveries(client, type, status, created_at);

CREATE INDEX IF NOT EXISTS idx_client_deliveries_session
    ON client_deliveries(session_id, run_id, task_id);

CREATE INDEX IF NOT EXISTS idx_automation_jobs_due
    ON automation_jobs(status, next_due_at);

CREATE INDEX IF NOT EXISTS idx_automation_fires_job
    ON automation_fires(job_id, scheduled_for);

CREATE INDEX IF NOT EXISTS idx_external_agent_sessions_agent
    ON external_agent_sessions(agent_id, external_thread_id);
`
