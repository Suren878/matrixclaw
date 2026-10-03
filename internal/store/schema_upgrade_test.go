package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// rawDB opens the database file behind the store, closing the store first.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
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

	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	if tableExists(t, db, "file_snapshots") {
		t.Fatal("file_snapshots is still there")
	}
}
