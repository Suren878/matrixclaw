package webresearch

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestWorkStoreCreatesWorkTablesNotLegacyWebResearchTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "research.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	if err := store.CreateSession(context.Background(), ResearchSession{
		ID:        "wr_test",
		Task:      "test work storage",
		Query:     "test",
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	if !sqliteTableExists(t, db, "work_jobs") {
		t.Fatal("work_jobs table was not created")
	}
	if sqliteTableExists(t, db, "web_research_sessions") {
		t.Fatal("legacy web_research_sessions table should not be created for new stores")
	}
}

func sqliteTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("query sqlite_master for %s: %v", table, err)
	}
	return count > 0
}
