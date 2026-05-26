package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) CreateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO subagent_tasks(id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id, runtime, goal, status, summary, error, created_at, updated_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.ParentSessionID,
		task.ParentRunID,
		task.ParentToolCallID,
		task.ChildSessionID,
		task.ChildRunID,
		task.Runtime,
		task.Goal,
		string(task.Status),
		task.Summary,
		task.Error,
		formatTime(task.CreatedAt),
		formatTime(task.UpdatedAt),
		nullableTime(task.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create subagent task: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE subagent_tasks
SET parent_session_id = ?, parent_run_id = ?, parent_tool_call_id = ?, child_session_id = ?, child_run_id = ?, runtime = ?, goal = ?, status = ?, summary = ?, error = ?, updated_at = ?, finished_at = ?
WHERE id = ?`,
		task.ParentSessionID,
		task.ParentRunID,
		task.ParentToolCallID,
		task.ChildSessionID,
		task.ChildRunID,
		task.Runtime,
		task.Goal,
		string(task.Status),
		task.Summary,
		task.Error,
		formatTime(task.UpdatedAt),
		nullableTime(task.FinishedAt),
		task.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update subagent task: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: update subagent task rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetSubagentTask(ctx context.Context, taskID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id, runtime, goal, status, summary, error, created_at, updated_at, finished_at
FROM subagent_tasks
WHERE id = ?`, taskID)
	task, err := scanSubagentTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.SubagentTask{}, core.ErrNotFound
		}
		return core.SubagentTask{}, err
	}
	return task, nil
}

type subagentTaskScanner interface {
	Scan(dest ...any) error
}

func scanSubagentTask(scanner subagentTaskScanner) (core.SubagentTask, error) {
	var task core.SubagentTask
	var status string
	var createdAt string
	var updatedAt string
	var finishedAt sql.NullString
	if err := scanner.Scan(
		&task.ID,
		&task.ParentSessionID,
		&task.ParentRunID,
		&task.ParentToolCallID,
		&task.ChildSessionID,
		&task.ChildRunID,
		&task.Runtime,
		&task.Goal,
		&status,
		&task.Summary,
		&task.Error,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		return core.SubagentTask{}, fmt.Errorf("store: scan subagent task: %w", err)
	}
	task.Status = core.SubagentTaskStatus(status)
	task.CreatedAt = mustParseTime(createdAt)
	task.UpdatedAt = mustParseTime(updatedAt)
	if finishedAt.Valid && finishedAt.String != "" {
		parsed := mustParseTime(finishedAt.String)
		task.FinishedAt = &parsed
	}
	return task, nil
}
