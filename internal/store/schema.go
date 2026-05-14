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
	if err := ensureColumn(db, "external_agent_sessions", "approval_policy", `ALTER TABLE external_agent_sessions ADD COLUMN approval_policy TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "external_agent_sessions", "sandbox", `ALTER TABLE external_agent_sessions ADD COLUMN sandbox TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
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
