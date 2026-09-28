package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GetSessionEngineState returns the engine counters the session's next run
// starts from; nil when none were stored.
func (s *SQLiteStore) GetSessionEngineState(ctx context.Context, sessionID string) (json.RawMessage, error) {
	var state string
	err := s.db.QueryRowContext(ctx, `SELECT engine_state FROM session_engine_state WHERE session_id = ?`, strings.TrimSpace(sessionID)).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get session engine state: %w", err)
	}
	return json.RawMessage(state), nil
}

// SaveSessionEngineState replaces the engine counters the session's next run starts from.
func (s *SQLiteStore) SaveSessionEngineState(ctx context.Context, sessionID string, state json.RawMessage, updatedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_engine_state(session_id, engine_state, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    engine_state = excluded.engine_state,
    updated_at = excluded.updated_at`,
		strings.TrimSpace(sessionID), string(state), formatTime(updatedAt))
	if err != nil {
		return fmt.Errorf("store: save session engine state: %w", err)
	}
	return nil
}
