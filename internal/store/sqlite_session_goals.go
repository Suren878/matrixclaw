package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
)

const sessionGoalColumns = `session_id, objective, check_command, max_runs, token_budget, status, reason,
    runs_used, tokens_used, last_run_id, last_stop, last_check, check_failure, check_failures,
    last_blocker, blocker_repeats, blocker_run_id, created_at, updated_at`

// GetSessionGoal returns the session's goal, core.ErrNotFound when it has none.
func (s *SQLiteStore) GetSessionGoal(ctx context.Context, sessionID string) (core.SessionGoal, error) {
	goal, err := scanSessionGoal(s.db.QueryRowContext(ctx, `SELECT `+sessionGoalColumns+` FROM session_goals WHERE session_id = ?`, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return core.SessionGoal{}, core.ErrNotFound
	}
	if err != nil {
		return core.SessionGoal{}, fmt.Errorf("store: get session goal: %w", err)
	}
	return goal, nil
}

// SaveSessionGoal writes the session's goal, replacing the one it had.
func (s *SQLiteStore) SaveSessionGoal(ctx context.Context, goal core.SessionGoal) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO session_goals(`+sessionGoalColumns+`)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		goal.SessionID, goal.Objective, goal.CheckCommand, goal.MaxRuns, goal.TokenBudget, string(goal.Status), goal.Reason,
		goal.RunsUsed, goal.TokensUsed, goal.LastRunID, goal.LastStop, goal.LastCheck, goal.CheckFailure, goal.CheckFailures,
		goal.LastBlocker, goal.BlockerRepeats, goal.BlockerRunID, formatTime(goal.CreatedAt), formatTime(goal.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: save session goal: %w", err)
	}
	return nil
}

// DeleteSessionGoal removes the session's goal; none is not an error.
func (s *SQLiteStore) DeleteSessionGoal(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM session_goals WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("store: delete session goal: %w", err)
	}
	return nil
}

// ListActiveSessionGoals lists the goals sessions are working on.
func (s *SQLiteStore) ListActiveSessionGoals(ctx context.Context) ([]core.SessionGoal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionGoalColumns+` FROM session_goals WHERE status = ? ORDER BY session_id`, string(core.GoalActive))
	if err != nil {
		return nil, fmt.Errorf("store: list active session goals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var goals []core.SessionGoal
	for rows.Next() {
		goal, err := scanSessionGoal(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan session goal: %w", err)
		}
		goals = append(goals, goal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list active session goals: %w", err)
	}
	return goals, nil
}

func scanSessionGoal(row interface{ Scan(...any) error }) (core.SessionGoal, error) {
	var goal core.SessionGoal
	var status, createdAt, updatedAt string
	err := row.Scan(&goal.SessionID, &goal.Objective, &goal.CheckCommand, &goal.MaxRuns, &goal.TokenBudget, &status, &goal.Reason,
		&goal.RunsUsed, &goal.TokensUsed, &goal.LastRunID, &goal.LastStop, &goal.LastCheck, &goal.CheckFailure, &goal.CheckFailures,
		&goal.LastBlocker, &goal.BlockerRepeats, &goal.BlockerRunID, &createdAt, &updatedAt)
	if err != nil {
		return core.SessionGoal{}, err
	}
	goal.Status = core.GoalStatus(status)
	goal.CreatedAt, goal.UpdatedAt = mustParseTime(createdAt), mustParseTime(updatedAt)
	return goal, nil
}
