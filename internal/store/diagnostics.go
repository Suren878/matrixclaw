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

	version, err := userVersion(db)
	if err != nil {
		return Diagnostics{Path: cleanPath, Exists: true}, err
	}
	diag := Diagnostics{
		Path:        cleanPath,
		Exists:      true,
		SchemaReady: version == schemaVersion,
	}
	if !diag.SchemaReady {
		return diag, fmt.Errorf("store: database schema version %d, want %d (the daemon migrates it on start)", version, schemaVersion)
	}
	return diag, nil
}
