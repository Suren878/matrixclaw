package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) SaveRunCheckpoint(ctx context.Context, checkpoint core.RunCheckpoint) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_checkpoints(run_id, phase, tool_call_id, tool_name, recovery_count, recovery_reason, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    phase = excluded.phase,
    tool_call_id = excluded.tool_call_id,
    tool_name = excluded.tool_name,
    recovery_count = excluded.recovery_count,
    recovery_reason = excluded.recovery_reason,
    updated_at = excluded.updated_at`,
		checkpoint.RunID,
		string(checkpoint.Phase),
		checkpoint.ToolCallID,
		checkpoint.ToolName,
		checkpoint.RecoveryCount,
		checkpoint.RecoveryReason,
		formatTime(checkpoint.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save run checkpoint: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRunCheckpoint(ctx context.Context, runID string) (core.RunCheckpoint, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT run_id, phase, tool_call_id, tool_name, recovery_count, recovery_reason, updated_at
FROM run_checkpoints
WHERE run_id = ?`, runID)
	var checkpoint core.RunCheckpoint
	var phase string
	var updatedAt string
	if err := row.Scan(
		&checkpoint.RunID,
		&phase,
		&checkpoint.ToolCallID,
		&checkpoint.ToolName,
		&checkpoint.RecoveryCount,
		&checkpoint.RecoveryReason,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.RunCheckpoint{}, core.ErrNotFound
		}
		return core.RunCheckpoint{}, fmt.Errorf("store: get run checkpoint: %w", err)
	}
	checkpoint.Phase = core.RunCheckpointPhase(phase)
	checkpoint.UpdatedAt = mustParseTime(updatedAt)
	return checkpoint, nil
}

func (s *SQLiteStore) DeleteRunCheckpoint(ctx context.Context, runID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM run_checkpoints WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("store: delete run checkpoint: %w", err)
	}
	return nil
}
