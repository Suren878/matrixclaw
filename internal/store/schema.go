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
	return nil
}
