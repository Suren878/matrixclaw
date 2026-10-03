package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
INSERT INTO approvals(id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, suggestion_json, requested_at, decided_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		approval.ID,
		approval.SessionID,
		approval.RunID,
		approval.ToolCallRef,
		approval.ToolName,
		approval.Description,
		approval.Action,
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
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, suggestion_json, requested_at, decided_at
FROM approvals
WHERE id = ?`, approvalID)

	var approval core.Approval
	var state string
	var paramsJSON string
	var suggestionJSON string
	var requestedAt string
	var decidedAt sql.NullString
	if err := row.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &state, &approval.Reason, &suggestionJSON, &requestedAt, &decidedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Approval{}, core.ErrNotFound
		}
		return core.Approval{}, fmt.Errorf("store: get approval: %w", err)
	}
	approval.State = core.ApprovalState(state)
	approval.Params = json.RawMessage(paramsJSON)
	approval.Suggestion = decodeSuggestion(suggestionJSON)
	approval.RequestedAt = mustParseTime(requestedAt)
	if decidedAt.Valid {
		parsed := mustParseTime(decidedAt.String)
		approval.DecidedAt = &parsed
	}
	return approval, nil
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

const approvalColumns = `id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, suggestion_json, requested_at, decided_at`

// ListApprovals lists the session's approvals in state (any when empty), newest first.
func (s *SQLiteStore) ListApprovals(ctx context.Context, sessionID string, state core.ApprovalState) ([]core.Approval, error) {
	query := `SELECT ` + approvalColumns + ` FROM approvals WHERE session_id = ?`
	args := []any{sessionID}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, string(state))
	}
	return s.queryApprovals(ctx, query+` ORDER BY requested_at DESC`, args...)
}

// ListRunApprovals lists the approvals the run asked for, newest first.
func (s *SQLiteStore) ListRunApprovals(ctx context.Context, sessionID string, runID string) ([]core.Approval, error) {
	return s.queryApprovals(ctx, `SELECT `+approvalColumns+` FROM approvals WHERE session_id = ? AND run_id = ? ORDER BY requested_at DESC`, sessionID, runID)
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
		if err := rows.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &rawState, &approval.Reason, &suggestionJSON, &requestedAt, &decidedAt); err != nil {
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
