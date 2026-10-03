package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Diagnostics struct {
	Path        string
	Exists      bool
	SchemaReady bool
}

func CheckSQLite(path string) (Diagnostics, error) {
	if path == "" {
		return Diagnostics{}, fmt.Errorf("store: sqlite path is required")
	}
	cleanPath := filepath.Clean(path)
	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Diagnostics{Path: cleanPath}, fmt.Errorf("store: sqlite database does not exist")
		}
		return Diagnostics{Path: cleanPath}, err
	}
	if info.IsDir() {
		return Diagnostics{Path: cleanPath, Exists: true}, fmt.Errorf("store: sqlite path is a directory")
	}

	db, err := sql.Open("sqlite", cleanPath)
	if err != nil {
		return Diagnostics{Path: cleanPath, Exists: true}, err
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		return Diagnostics{Path: cleanPath, Exists: true}, err
	}

	ready, err := canonicalSchemaReady(db)
	if err != nil {
		return Diagnostics{Path: cleanPath, Exists: true}, err
	}
	diag := Diagnostics{
		Path:        cleanPath,
		Exists:      true,
		SchemaReady: ready,
	}
	if !ready {
		return diag, fmt.Errorf("store: sqlite canonical schema is incomplete")
	}
	return diag, nil
}

func canonicalSchemaReady(db *sql.DB) (bool, error) {
	requiredTables := []string{
		"sessions",
		"tasks",
		"client_bindings",
		"messages",
		"memories",
		"runs",
		"run_checkpoints",
		"run_wakeups",
		"session_inputs",
		"run_steps",
		"approvals",
		"client_deliveries",
		"automation_jobs",
		"automation_fires",
		"external_agent_sessions",
		"session_budgets",
		"session_engine_state",
		"session_todos",
		"permission_rules",
	}
	for _, table := range requiredTables {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(1) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
			return false, err
		}
		if exists == 0 {
			return false, nil
		}
	}
	return true, nil
}
