package store_test

import (
	"database/sql"
	"path/filepath"
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
