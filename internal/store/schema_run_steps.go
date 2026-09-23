package store

import (
	"database/sql"
	"fmt"
)

// migrateRunUsage turns each legacy run_usage aggregate into one run step and
// drops the table. Legacy Anthropic input excluded its cached tokens.
func migrateRunUsage(db *sql.DB) error {
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'run_usage'`).Scan(&tables); err != nil {
		return fmt.Errorf("store: inspect run_usage: %w", err)
	}
	if tables == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin run_usage migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT OR IGNORE INTO run_steps(run_id, step, model, provider, prompt_tokens, cache_read_tokens,
    output_tokens, reasoning_tokens, stop_reason, created_at)
SELECT u.run_id, 1, u.model, u.provider,
       CASE WHEN u.provider = 'anthropic-compatible' THEN u.input_tokens + u.cached_tokens ELSE u.input_tokens END,
       CASE WHEN u.provider = 'anthropic-compatible' THEN 0 ELSE u.cached_tokens END,
       u.output_tokens, u.reasoning_tokens, 'legacy', u.created_at
FROM run_usage u
JOIN runs r ON r.id = u.run_id`); err != nil {
		return fmt.Errorf("store: copy run_usage into run_steps: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE run_usage`); err != nil {
		return fmt.Errorf("store: drop run_usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit run_usage migration: %w", err)
	}
	return nil
}
