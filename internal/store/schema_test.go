package store_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

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
