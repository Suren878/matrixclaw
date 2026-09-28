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

// Subagent tasks are the rows of tasks of kind subagent.

func (s *SQLiteStore) CreateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	task = normalizeSubagentTaskForStore(task)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks(
    id, kind, agent_name, description, background, isolation, session_id, run_id, parent_tool_call_id,
    child_session_id, child_run_id, runtime, command_or_goal, status, summary, error, result_message_id,
    started_at, updated_at, finished_at
)
VALUES(?, 'subagent', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.AgentName,
		task.DisplayName,
		task.Mode == core.SubagentTaskModeAsync,
		string(task.Isolation),
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
		task.ResultMessageID,
		formatTime(task.CreatedAt),
		formatTime(task.UpdatedAt),
		nullableTime(task.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create subagent task: %w", err)
	}
	return nil
}

// UpdateSubagentTask saves the task; whether and when it was delivered is kept
// by MarkTasksDelivered alone.
func (s *SQLiteStore) UpdateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	task = normalizeSubagentTaskForStore(task)
	result, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET agent_name = ?, description = ?, background = ?, isolation = ?, session_id = ?, run_id = ?, parent_tool_call_id = ?,
    child_session_id = ?, child_run_id = ?, runtime = ?, command_or_goal = ?, status = ?, summary = ?, error = ?,
    result_message_id = ?, updated_at = ?, finished_at = ?
WHERE id = ? AND kind = 'subagent'`,
		task.AgentName,
		task.DisplayName,
		task.Mode == core.SubagentTaskModeAsync,
		string(task.Isolation),
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
		task.ResultMessageID,
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
SELECT `+subagentTaskColumns+`
FROM tasks
WHERE id = ? AND kind = 'subagent'`, taskID)
	return scanOneSubagentTask(row)
}

func (s *SQLiteStore) GetSubagentTaskByParentToolCall(ctx context.Context, parentSessionID string, parentRunID string, parentToolCallID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM tasks
WHERE kind = 'subagent' AND session_id = ? AND run_id = ? AND parent_tool_call_id = ?
ORDER BY started_at ASC
LIMIT 1`, parentSessionID, parentRunID, parentToolCallID)
	return scanOneSubagentTask(row)
}

func (s *SQLiteStore) GetSubagentTaskByChildRun(ctx context.Context, childRunID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM tasks
WHERE kind = 'subagent' AND child_run_id = ?
ORDER BY started_at ASC
LIMIT 1`, childRunID)
	return scanOneSubagentTask(row)
}

func (s *SQLiteStore) ListSubagentTasks(ctx context.Context, filter core.SubagentTaskFilter) ([]core.SubagentTask, error) {
	where := []string{"kind = 'subagent'"}
	var args []any
	if strings.TrimSpace(filter.ParentSessionID) != "" {
		where = append(where, "session_id = ?")
		args = append(args, strings.TrimSpace(filter.ParentSessionID))
	}
	if filter.Mode != "" {
		where = append(where, "background = ?")
		args = append(args, filter.Mode == core.SubagentTaskModeAsync)
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, string(status))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	query := "SELECT " + subagentTaskColumns + " FROM tasks WHERE " + strings.Join(where, " AND ") + " ORDER BY started_at DESC, id DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	return s.listSubagentTasks(ctx, query, args...)
}

func (s *SQLiteStore) ListActiveSubagentTasksByParent(ctx context.Context, parentSessionID string) ([]core.SubagentTask, error) {
	return s.ListSubagentTasks(ctx, core.SubagentTaskFilter{
		ParentSessionID: strings.TrimSpace(parentSessionID),
		Mode:            core.SubagentTaskModeAsync,
		Statuses: []core.TaskStatus{
			core.TaskStatusPending,
			core.TaskStatusRunning,
			core.TaskStatusWaitingApproval,
		},
	})
}

func (s *SQLiteStore) listSubagentTasks(ctx context.Context, query string, args ...any) ([]core.SubagentTask, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list subagent tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tasks []core.SubagentTask
	for rows.Next() {
		task, err := scanSubagentTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate subagent tasks: %w", err)
	}
	return tasks, nil
}

type subagentTaskScanner interface {
	Scan(dest ...any) error
}

const subagentTaskColumns = `id, agent_name, description, background, isolation, session_id, run_id, parent_tool_call_id,
child_session_id, child_run_id, runtime, command_or_goal, status, summary, error, result_message_id,
delivered_at, delivered_run_id, started_at, updated_at, finished_at`

func scanOneSubagentTask(row *sql.Row) (core.SubagentTask, error) {
	task, err := scanSubagentTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return core.SubagentTask{}, core.ErrNotFound
	}
	return task, err
}

func scanSubagentTask(scanner subagentTaskScanner) (core.SubagentTask, error) {
	var task core.SubagentTask
	var status string
	var background bool
	var isolation string
	var createdAt string
	var updatedAt string
	var finishedAt sql.NullString
	var deliveredAt sql.NullString
	if err := scanner.Scan(
		&task.ID,
		&task.AgentName,
		&task.DisplayName,
		&background,
		&isolation,
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
		&task.ResultMessageID,
		&deliveredAt,
		&task.DeliveredRunID,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.SubagentTask{}, err
		}
		return core.SubagentTask{}, fmt.Errorf("store: scan subagent task: %w", err)
	}
	task.Mode = core.SubagentTaskModeBlocking
	if background {
		task.Mode = core.SubagentTaskModeAsync
	}
	task.Isolation = core.SubagentIsolation(isolation)
	if task.Isolation == "" {
		task.Isolation = core.SubagentIsolationShared
	}
	task.Status = core.TaskStatus(status)
	task.CreatedAt = mustParseTime(createdAt)
	task.UpdatedAt = mustParseTime(updatedAt)
	task.DeliveredAt = parseNullableTime(deliveredAt)
	task.FinishedAt = parseNullableTime(finishedAt)
	return task, nil
}

func parseNullableTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := mustParseTime(value.String)
	return &parsed
}

func normalizeSubagentTaskForStore(task core.SubagentTask) core.SubagentTask {
	task.AgentName = strings.Join(strings.Fields(task.AgentName), " ")
	if task.Mode == "" {
		task.Mode = core.SubagentTaskModeBlocking
	}
	if task.Isolation == "" {
		task.Isolation = core.SubagentIsolationShared
	}
	return task
}
