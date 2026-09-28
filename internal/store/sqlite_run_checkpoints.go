package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) SaveRunCheckpoint(ctx context.Context, checkpoint core.RunCheckpoint) error {
	batch := ""
	if checkpoint.Batch != nil {
		encoded, err := json.Marshal(checkpoint.Batch)
		if err != nil {
			return fmt.Errorf("store: encode run checkpoint batch: %w", err)
		}
		batch = string(encoded)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_checkpoints(run_id, phase, tool_call_id, tool_name, tool_batch, recovery_count, recovery_reason, engine_state, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    phase = excluded.phase,
    tool_call_id = excluded.tool_call_id,
    tool_name = excluded.tool_name,
    tool_batch = excluded.tool_batch,
    recovery_count = excluded.recovery_count,
    recovery_reason = excluded.recovery_reason,
    engine_state = excluded.engine_state,
    updated_at = excluded.updated_at`,
		checkpoint.RunID,
		string(checkpoint.Phase),
		checkpoint.ToolCallID,
		checkpoint.ToolName,
		batch,
		checkpoint.RecoveryCount,
		checkpoint.RecoveryReason,
		string(checkpoint.EngineState),
		formatTime(checkpoint.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save run checkpoint: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRunCheckpoint(ctx context.Context, runID string) (core.RunCheckpoint, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT run_id, phase, tool_call_id, tool_name, tool_batch, recovery_count, recovery_reason, engine_state, updated_at
FROM run_checkpoints
WHERE run_id = ?`, runID)
	var checkpoint core.RunCheckpoint
	var phase string
	var batch string
	var engineState string
	var updatedAt string
	if err := row.Scan(
		&checkpoint.RunID,
		&phase,
		&checkpoint.ToolCallID,
		&checkpoint.ToolName,
		&batch,
		&checkpoint.RecoveryCount,
		&checkpoint.RecoveryReason,
		&engineState,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.RunCheckpoint{}, core.ErrNotFound
		}
		return core.RunCheckpoint{}, fmt.Errorf("store: get run checkpoint: %w", err)
	}
	checkpoint.Phase = core.RunCheckpointPhase(phase)
	if batch != "" {
		if err := json.Unmarshal([]byte(batch), &checkpoint.Batch); err != nil {
			return core.RunCheckpoint{}, fmt.Errorf("store: decode run checkpoint batch: %w", err)
		}
	}
	if engineState != "" {
		checkpoint.EngineState = json.RawMessage(engineState)
	}
	checkpoint.UpdatedAt = mustParseTime(updatedAt)
	return checkpoint, nil
}

func (s *SQLiteStore) DeleteRunCheckpoint(ctx context.Context, runID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM run_checkpoints WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("store: delete run checkpoint: %w", err)
	}
	return nil
}
