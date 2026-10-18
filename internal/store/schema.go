package store

import (
	"database/sql"
	"fmt"

	_ "embed"
)

//go:embed migrations/001_init.sql
var initSchemaSQL string

//go:embed migrations/002_file_checkpoints.sql
var fileCheckpointsSQL string

//go:embed migrations/004_session_kind.sql
var sessionKindSQL string

// migrations are applied in order; a database at user_version n has applied
// the first n. 001_init.sql is the base schema a v0.2 database is converted to.
var migrations = []string{initSchemaSQL, fileCheckpointsSQL, sessionKindSQL}

// schemaVersion is the user_version of a database this binary has migrated.
var schemaVersion = len(migrations)

// migrate brings the database at path to schemaVersion: a database without
// a user_version is converted once by convertV02, then pending migrations run.
func migrate(db *sql.DB, path string) error {
	version, err := userVersion(db)
	if err != nil {
		return err
	}
	if version == 0 {
		var tables int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
			return fmt.Errorf("store: inspect schema: %w", err)
		}
		if tables > 0 {
			if err := convertV02(db, path); err != nil {
				return err
			}
			version = 1
		}
	}
	if version > schemaVersion {
		return fmt.Errorf("store: database schema version %d is newer than this matrixclaw (version %d); use a newer matrixclaw", version, schemaVersion)
	}
	for next := version; next < schemaVersion; next++ {
		if err := applyMigration(db, next+1, migrations[next]); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(db *sql.DB, version int, script string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin migration %d: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(script); err != nil {
		return fmt.Errorf("store: apply migration %d: %w", version, err)
	}
	if err := setUserVersion(tx, version); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migration %d: %w", version, err)
	}
	return nil
}

func userVersion(db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return version, nil
}

func setUserVersion(tx *sql.Tx, version int) error {
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return fmt.Errorf("store: set schema version %d: %w", version, err)
	}
	return nil
}
