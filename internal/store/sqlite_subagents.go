package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) CreateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	task = normalizeSubagentTaskForStore(task)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO subagent_tasks(
    id, agent_name, display_name, mode, isolation, parent_session_id, parent_run_id, parent_tool_call_id,
    child_session_id, child_run_id, runtime, goal, status, summary, error, result_message_id,
    completion_queued_at, completion_delivered_at, completion_auto_resume_run_id,
    created_at, updated_at, finished_at
)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.AgentName,
		task.DisplayName,
		string(task.Mode),
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
		nullableTime(task.CompletionQueuedAt),
		nullableTime(task.CompletionDeliveredAt),
		task.CompletionAutoResumeRunID,
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
	task = normalizeSubagentTaskForStore(task)
	result, err := s.db.ExecContext(ctx, `
UPDATE subagent_tasks
SET agent_name = ?, display_name = ?, mode = ?, isolation = ?, parent_session_id = ?, parent_run_id = ?, parent_tool_call_id = ?,
    child_session_id = ?, child_run_id = ?, runtime = ?, goal = ?, status = ?, summary = ?, error = ?,
    result_message_id = ?, completion_queued_at = ?, completion_delivered_at = ?, completion_auto_resume_run_id = ?,
    updated_at = ?, finished_at = ?
WHERE id = ?`,
		task.AgentName,
		task.DisplayName,
		string(task.Mode),
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
		nullableTime(task.CompletionQueuedAt),
		nullableTime(task.CompletionDeliveredAt),
		task.CompletionAutoResumeRunID,
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

func (s *SQLiteStore) GetSubagentTaskByParentToolCall(ctx context.Context, parentSessionID string, parentRunID string, parentToolCallID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM subagent_tasks
WHERE parent_session_id = ? AND parent_run_id = ? AND parent_tool_call_id = ?
ORDER BY created_at ASC
LIMIT 1`, parentSessionID, parentRunID, parentToolCallID)
	task, err := scanSubagentTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.SubagentTask{}, core.ErrNotFound
		}
		return core.SubagentTask{}, err
	}
	return task, nil
}

func (s *SQLiteStore) GetSubagentTaskByChildRun(ctx context.Context, childRunID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM subagent_tasks
WHERE child_run_id = ?
ORDER BY created_at ASC
LIMIT 1`, childRunID)
	task, err := scanSubagentTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.SubagentTask{}, core.ErrNotFound
		}
		return core.SubagentTask{}, err
	}
	return task, nil
}

func (s *SQLiteStore) ListSubagentTasks(ctx context.Context, filter core.SubagentTaskFilter) ([]core.SubagentTask, error) {
	var where []string
	var args []any
	if strings.TrimSpace(filter.ParentSessionID) != "" {
		where = append(where, "parent_session_id = ?")
		args = append(args, strings.TrimSpace(filter.ParentSessionID))
	}
	if filter.Mode != "" {
		where = append(where, "mode = ?")
		args = append(args, string(filter.Mode))
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, string(status))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	query := "SELECT " + subagentTaskColumns + " FROM subagent_tasks"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC"
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
		Statuses: []core.SubagentTaskStatus{
			core.SubagentTaskStatusPending,
			core.SubagentTaskStatusRunning,
			core.SubagentTaskStatusWaitingApproval,
		},
	})
}

func (s *SQLiteStore) ListPendingSubagentCompletionTasks(ctx context.Context, limit int) ([]core.SubagentTask, error) {
	query := "SELECT " + subagentTaskColumns + ` FROM subagent_tasks
WHERE mode = ? AND completion_queued_at IS NOT NULL AND completion_delivered_at IS NULL
ORDER BY completion_queued_at ASC, id ASC`
	var args []any
	args = append(args, string(core.SubagentTaskModeAsync))
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	return s.listSubagentTasks(ctx, query, args...)
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

const subagentTaskColumns = `id, agent_name, display_name, mode, isolation, parent_session_id, parent_run_id, parent_tool_call_id,
child_session_id, child_run_id, runtime, goal, status, summary, error, result_message_id,
completion_queued_at, completion_delivered_at, completion_auto_resume_run_id, created_at, updated_at, finished_at`

func scanSubagentTask(scanner subagentTaskScanner) (core.SubagentTask, error) {
	var task core.SubagentTask
	var status string
	var mode string
	var isolation string
	var createdAt string
	var updatedAt string
	var finishedAt sql.NullString
	var completionQueuedAt sql.NullString
	var completionDeliveredAt sql.NullString
	if err := scanner.Scan(
		&task.ID,
		&task.AgentName,
		&task.DisplayName,
		&mode,
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
		&completionQueuedAt,
		&completionDeliveredAt,
		&task.CompletionAutoResumeRunID,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		return core.SubagentTask{}, fmt.Errorf("store: scan subagent task: %w", err)
	}
	task.Mode = core.SubagentTaskMode(mode)
	if task.Mode == "" {
		task.Mode = core.SubagentTaskModeBlocking
	}
	task.Isolation = core.SubagentIsolation(isolation)
	if task.Isolation == "" {
		task.Isolation = core.SubagentIsolationShared
	}
	task.Status = core.SubagentTaskStatus(status)
	task.CreatedAt = mustParseTime(createdAt)
	task.UpdatedAt = mustParseTime(updatedAt)
	if completionQueuedAt.Valid && completionQueuedAt.String != "" {
		parsed := mustParseTime(completionQueuedAt.String)
		task.CompletionQueuedAt = &parsed
	}
	if completionDeliveredAt.Valid && completionDeliveredAt.String != "" {
		parsed := mustParseTime(completionDeliveredAt.String)
		task.CompletionDeliveredAt = &parsed
	}
	if finishedAt.Valid && finishedAt.String != "" {
		parsed := mustParseTime(finishedAt.String)
		task.FinishedAt = &parsed
	}
	return task, nil
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
