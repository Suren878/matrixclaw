package store_test

import (
	"database/sql"
	"path/filepath"
	"strings"
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
	if strings.Join(once, ",") != "guest,inline" {
		t.Fatalf("reply-once deliveries = %v", once)
	}
}
