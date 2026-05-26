package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewSQLiteMigratesLegacySessionsBeforeCreatingSubagentIndexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    provider_id TEXT NOT NULL DEFAULT '',
    model_id TEXT NOT NULL DEFAULT '',
    permission_mode TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'assistant',
    runtime_id TEXT NOT NULL DEFAULT 'matrixclaw'
)`)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close legacy sqlite: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("create legacy sessions: %v", err)
	}

	sqliteStore, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite legacy migration: %v", err)
	}
	defer func() { _ = sqliteStore.Close() }()

	if !sqliteColumnExists(t, sqliteStore.db, "sessions", "parent_session_id") {
		t.Fatal("sessions.parent_session_id was not added")
	}
	if !sqliteColumnExists(t, sqliteStore.db, "sessions", "hidden") {
		t.Fatal("sessions.hidden was not added")
	}
}

func sqliteColumnExists(t *testing.T, db *sql.DB, table string, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
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
			t.Fatalf("scan table info %s: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info %s: %v", table, err)
	}
	return false
}
