package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func (s *SQLiteStore) CreateApproval(ctx context.Context, approval core.Approval) error {
	suggestion := ""
	if approval.Suggestion != nil {
		body, err := json.Marshal(approval.Suggestion)
		if err != nil {
			return fmt.Errorf("store: encode approval suggestion: %w", err)
		}
		suggestion = string(body)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO approvals(id, session_id, run_id, task_id, tool_call_ref, tool_name, description, params_json, path, state, reason, suggestion_json, requested_at, decided_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		approval.ID,
		approval.SessionID,
		approval.RunID,
		approval.TaskID,
		approval.ToolCallRef,
		approval.ToolName,
		approval.Description,
		string(approval.Params),
		approval.Path,
		string(approval.State),
		approval.Reason,
		suggestion,
		formatTime(approval.RequestedAt),
		nullableTime(approval.DecidedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create approval: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetApproval(ctx context.Context, approvalID string) (core.Approval, error) {
	approvals, err := s.queryApprovals(ctx, approvalSelect+` WHERE a.id = ?`, approvalID)
	if err != nil {
		return core.Approval{}, err
	}
	if len(approvals) == 0 {
		return core.Approval{}, core.ErrNotFound
	}
	return approvals[0], nil
}

func (s *SQLiteStore) UpdateApproval(ctx context.Context, approval core.Approval) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE approvals
SET state = ?, reason = ?, decided_at = ?
WHERE id = ?`,
		string(approval.State),
		approval.Reason,
		nullableTime(approval.DecidedAt),
		approval.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update approval: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update approval rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

// approvalSelect reads approvals with the name of the subagent that asked.
const approvalSelect = `SELECT a.id, a.session_id, a.run_id, a.task_id, COALESCE(t.agent_name, ''), a.tool_call_ref, a.tool_name,
a.description, a.params_json, a.path, a.state, a.reason, a.suggestion_json, a.requested_at, a.decided_at
FROM approvals a LEFT JOIN tasks t ON t.id = a.task_id AND a.task_id <> ''`

// ListApprovals lists the approvals in state (any when empty) the session's
// runs and its subagents asked for, newest first.
func (s *SQLiteStore) ListApprovals(ctx context.Context, sessionID string, state core.ApprovalState) ([]core.Approval, error) {
	query := approvalSelect + ` WHERE (a.session_id = ? OR a.task_id IN (SELECT id FROM tasks WHERE session_id = ? AND kind = 'subagent'))`
	args := []any{sessionID, sessionID}
	if state != "" {
		query += ` AND a.state = ?`
		args = append(args, string(state))
	}
	return s.queryApprovals(ctx, query+` ORDER BY a.requested_at DESC`, args...)
}

// ListRunApprovals lists the approvals the run asked for, newest first.
func (s *SQLiteStore) ListRunApprovals(ctx context.Context, sessionID string, runID string) ([]core.Approval, error) {
	return s.queryApprovals(ctx, approvalSelect+` WHERE a.session_id = ? AND a.run_id = ? ORDER BY a.requested_at DESC`, sessionID, runID)
}

func (s *SQLiteStore) queryApprovals(ctx context.Context, query string, args ...any) ([]core.Approval, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var approvals []core.Approval
	for rows.Next() {
		var approval core.Approval
		var rawState string
		var paramsJSON string
		var suggestionJSON string
		var requestedAt string
		var decidedAt sql.NullString
		if err := rows.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.TaskID, &approval.AgentName, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &paramsJSON, &approval.Path, &rawState, &approval.Reason, &suggestionJSON, &requestedAt, &decidedAt); err != nil {
			return nil, fmt.Errorf("store: scan approval: %w", err)
		}
		approval.State = core.ApprovalState(rawState)
		approval.Params = json.RawMessage(paramsJSON)
		approval.Suggestion = decodeSuggestion(suggestionJSON)
		approval.RequestedAt = mustParseTime(requestedAt)
		if decidedAt.Valid {
			parsed := mustParseTime(decidedAt.String)
			approval.DecidedAt = &parsed
		}
		approvals = append(approvals, approval)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate approvals: %w", err)
	}
	return approvals, nil
}

// decodeSuggestion reads a stored suggestion; an unreadable one offers none.
func decodeSuggestion(raw string) *permission.Suggestion {
	if raw == "" {
		return nil
	}
	var suggestion permission.Suggestion
	if json.Unmarshal([]byte(raw), &suggestion) != nil {
		return nil
	}
	return &suggestion
}
