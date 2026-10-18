package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
)

const fileCheckpointColumns = `id, session_id, run_id, seq, tool_name, working_dir, tree, created_at`

func (s *SQLiteStore) CreateFileCheckpoint(ctx context.Context, checkpoint core.FileCheckpoint) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO file_checkpoints(`+fileCheckpointColumns+`) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		checkpoint.ID, checkpoint.SessionID, checkpoint.RunID, checkpoint.Seq, checkpoint.ToolName, checkpoint.WorkingDir, checkpoint.Tree, formatTime(checkpoint.CreatedAt)); err != nil {
		return fmt.Errorf("store: create file checkpoint: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetFileCheckpoint(ctx context.Context, id string) (core.FileCheckpoint, error) {
	checkpoint, err := scanFileCheckpoint(s.db.QueryRowContext(ctx, `SELECT `+fileCheckpointColumns+` FROM file_checkpoints WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return core.FileCheckpoint{}, core.ErrNotFound
	}
	if err != nil {
		return core.FileCheckpoint{}, fmt.Errorf("store: get file checkpoint: %w", err)
	}
	return checkpoint, nil
}

// ListFileCheckpoints lists newest first; rows are ordered by insertion.
func (s *SQLiteStore) ListFileCheckpoints(ctx context.Context, sessionID string, limit int) ([]core.FileCheckpoint, error) {
	if limit <= 0 {
		limit = -1
	}
	return queryAll(ctx, s.db, "file checkpoints", scanFileCheckpoint, `SELECT `+fileCheckpointColumns+` FROM file_checkpoints WHERE session_id = ? ORDER BY rowid DESC LIMIT ?`, sessionID, limit)
}

func (s *SQLiteStore) TrimFileCheckpoints(ctx context.Context, workingDir string, keep int) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM file_checkpoints WHERE working_dir = ? AND rowid NOT IN (
    SELECT rowid FROM file_checkpoints WHERE working_dir = ? ORDER BY rowid DESC LIMIT ?
)`, workingDir, workingDir, keep)
	if err != nil {
		return false, fmt.Errorf("store: trim file checkpoints: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: trim file checkpoints: %w", err)
	}
	return count > 0, nil
}

func (s *SQLiteStore) FileCheckpointTrees(ctx context.Context, workingDir string) ([]string, error) {
	return queryAll(ctx, s.db, "file checkpoint trees", func(scanner rowScanner) (string, error) {
		var tree string
		err := scanner.Scan(&tree)
		return tree, err
	}, `SELECT DISTINCT tree FROM file_checkpoints WHERE working_dir = ?`, workingDir)
}

func scanFileCheckpoint(scanner rowScanner) (core.FileCheckpoint, error) {
	var checkpoint core.FileCheckpoint
	var createdAt string
	if err := scanner.Scan(&checkpoint.ID, &checkpoint.SessionID, &checkpoint.RunID, &checkpoint.Seq, &checkpoint.ToolName, &checkpoint.WorkingDir, &checkpoint.Tree, &createdAt); err != nil {
		return core.FileCheckpoint{}, err
	}
	checkpoint.CreatedAt = mustParseTime(createdAt)
	return checkpoint, nil
}
