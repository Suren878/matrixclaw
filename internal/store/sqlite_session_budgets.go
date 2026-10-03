package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

// GetSessionBudget returns the session's budget overrides; none stored is the zero value.
func (s *SQLiteStore) GetSessionBudget(ctx context.Context, sessionID string) (core.SessionBudget, error) {
	var steps, activeSeconds, tokens sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT steps, active_seconds, tokens FROM session_budgets WHERE session_id = ?`, sessionID).Scan(&steps, &activeSeconds, &tokens)
	if errors.Is(err, sql.ErrNoRows) {
		return core.SessionBudget{}, nil
	}
	if err != nil {
		return core.SessionBudget{}, fmt.Errorf("store: get session budget: %w", err)
	}
	return core.SessionBudget{Steps: scannedCount[int](steps), ActiveSeconds: scannedCount[int64](activeSeconds), Tokens: scannedCount[int64](tokens)}, nil
}

// SaveSessionBudget replaces the session's budget overrides; an empty budget removes them.
func (s *SQLiteStore) SaveSessionBudget(ctx context.Context, sessionID string, budget core.SessionBudget, updatedAt time.Time) error {
	if budget.IsZero() {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM session_budgets WHERE session_id = ?`, sessionID); err != nil {
			return fmt.Errorf("store: delete session budget: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_budgets(session_id, steps, active_seconds, tokens, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    steps = excluded.steps,
    active_seconds = excluded.active_seconds,
    tokens = excluded.tokens,
    updated_at = excluded.updated_at`,
		sessionID,
		nullableCount(budget.Steps),
		nullableCount(budget.ActiveSeconds),
		nullableCount(budget.Tokens),
		formatTime(updatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save session budget: %w", err)
	}
	return nil
}

func nullableCount[T int | int64](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func scannedCount[T int | int64](value sql.NullInt64) *T {
	if !value.Valid {
		return nil
	}
	count := T(value.Int64)
	return &count
}
