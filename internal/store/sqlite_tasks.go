package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

// MarkTasksDelivered records that runID told the tasks' sessions they finished;
// an empty runID means nothing is to be told.
func (s *SQLiteStore) MarkTasksDelivered(ctx context.Context, taskIDs []string, runID string, at time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	args := []any{formatTime(at), strings.TrimSpace(runID), formatTime(at)}
	placeholders := make([]string, 0, len(taskIDs))
	for _, id := range taskIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE tasks SET delivered_at = ?, delivered_run_id = ?, updated_at = ?
WHERE delivered_at IS NULL AND id IN (`+strings.Join(placeholders, ", ")+`)`, args...); err != nil {
		return fmt.Errorf("store: mark tasks delivered: %w", err)
	}
	return nil
}

const taskColumns = `id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal, description, working_dir,
background, agent_name, pid, pgid, leader_start, boot_id, output_path, exit_code, output_cursor, child_session_id, child_run_id, summary, error,
delivered_at, delivered_run_id, started_at, updated_at, finished_at`

func (s *SQLiteStore) CreateTask(ctx context.Context, task core.Task) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks(id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal, description, working_dir,
    background, agent_name, pid, pgid, leader_start, boot_id, output_path, exit_code, output_cursor, child_session_id, child_run_id,
    summary, error, started_at, updated_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.SessionID, task.RunID, task.ParentToolCallID, string(task.Kind), string(task.Status), task.Command,
		task.Description, task.WorkingDir, task.Background, task.AgentName, task.PID, task.PGID, task.LeaderStart, task.BootID, task.OutputPath,
		task.ExitCode, task.OutputCursor, task.ChildSessionID, task.ChildRunID, task.Summary, task.Error,
		formatTime(task.StartedAt), formatTime(task.UpdatedAt), nullableTime(task.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create task: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetTask(ctx context.Context, taskID string) (core.Task, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, strings.TrimSpace(taskID)))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Task{}, core.ErrNotFound
	}
	return task, err
}

// ListTasks lists the tasks the filter selects, newest first.
func (s *SQLiteStore) ListTasks(ctx context.Context, filter core.TaskFilter) ([]core.Task, error) {
	where := []string{"1 = 1"}
	var args []any
	if id := strings.TrimSpace(filter.SessionID); id != "" {
		where = append(where, "session_id = ?")
		args = append(args, id)
	}
	if filter.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, string(filter.Kind))
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, string(status))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	if filter.Background {
		where = append(where, "background = 1")
	}
	if filter.Undelivered {
		where = append(where, "background = 1 AND finished_at IS NOT NULL AND delivered_at IS NULL")
	}
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + strings.Join(where, " AND ") + ` ORDER BY started_at DESC, id DESC`
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tasks []core.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate tasks: %w", err)
	}
	return tasks, nil
}

func (s *SQLiteStore) FinishTask(ctx context.Context, taskID string, status core.TaskStatus, exitCode *int, errText string, at time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, exit_code = ?, error = ?, finished_at = ?, updated_at = ?
WHERE id = ? AND finished_at IS NULL`, string(status), exitCode, errText, formatTime(at), formatTime(at), strings.TrimSpace(taskID))
	if err != nil {
		return false, fmt.Errorf("store: finish task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: finish task rows: %w", err)
	}
	return rows > 0, nil
}

func (s *SQLiteStore) SetTaskCursor(ctx context.Context, taskID string, cursor int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET output_cursor = ? WHERE id = ?`, cursor, strings.TrimSpace(taskID)); err != nil {
		return fmt.Errorf("store: set task cursor: %w", err)
	}
	return nil
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (core.Task, error) {
	var task core.Task
	var kind, status, startedAt, updatedAt string
	var exitCode sql.NullInt64
	var deliveredAt, finishedAt sql.NullString
	if err := scanner.Scan(&task.ID, &task.SessionID, &task.RunID, &task.ParentToolCallID, &kind, &status, &task.Command,
		&task.Description, &task.WorkingDir, &task.Background, &task.AgentName, &task.PID, &task.PGID, &task.LeaderStart, &task.BootID, &task.OutputPath,
		&exitCode, &task.OutputCursor, &task.ChildSessionID, &task.ChildRunID, &task.Summary, &task.Error,
		&deliveredAt, &task.DeliveredRunID, &startedAt, &updatedAt, &finishedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Task{}, err
		}
		return core.Task{}, fmt.Errorf("store: scan task: %w", err)
	}
	task.Kind = core.TaskKind(kind)
	task.Status = core.TaskStatus(status)
	if exitCode.Valid {
		code := int(exitCode.Int64)
		task.ExitCode = &code
	}
	task.StartedAt = mustParseTime(startedAt)
	task.UpdatedAt = mustParseTime(updatedAt)
	task.DeliveredAt = parseNullableTime(deliveredAt)
	task.FinishedAt = parseNullableTime(finishedAt)
	return task, nil
}
