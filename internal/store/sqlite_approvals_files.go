package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) CreateApproval(ctx context.Context, approval core.Approval) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO approvals(id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, requested_at, decided_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
SELECT id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, requested_at, decided_at
FROM approvals
WHERE id = ?`, approvalID)

	var approval core.Approval
	var state string
	var paramsJSON string
	var requestedAt string
	var decidedAt sql.NullString
	if err := row.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &state, &requestedAt, &decidedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Approval{}, core.ErrNotFound
		}
		return core.Approval{}, fmt.Errorf("store: get approval: %w", err)
	}
	approval.State = core.ApprovalState(state)
	approval.Params = json.RawMessage(paramsJSON)
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
SET state = ?, decided_at = ?
WHERE id = ?`,
		string(approval.State),
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

func (s *SQLiteStore) ListApprovals(ctx context.Context, sessionID string, state core.ApprovalState) ([]core.Approval, error) {
	query := `
SELECT id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, requested_at, decided_at
FROM approvals
WHERE session_id = ?`
	args := []any{sessionID}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, string(state))
	}
	query += ` ORDER BY requested_at DESC`

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
		var requestedAt string
		var decidedAt sql.NullString
		if err := rows.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &rawState, &requestedAt, &decidedAt); err != nil {
			return nil, fmt.Errorf("store: scan approval: %w", err)
		}
		approval.State = core.ApprovalState(rawState)
		approval.Params = json.RawMessage(paramsJSON)
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

func (s *SQLiteStore) CreateFileSnapshot(ctx context.Context, snapshot core.FileSnapshot) (core.FileSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.FileSnapshot{}, fmt.Errorf("store: begin file snapshot: %w", err)
	}

	var nextVersion int
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(version), -1) + 1
FROM file_snapshots
WHERE session_id = ? AND path = ?`,
		snapshot.SessionID,
		snapshot.Path,
	).Scan(&nextVersion); err != nil {
		_ = tx.Rollback()
		return core.FileSnapshot{}, fmt.Errorf("store: next file snapshot version: %w", err)
	}

	snapshot.Version = nextVersion
	if _, err := tx.ExecContext(ctx, `
INSERT INTO file_snapshots(id, session_id, path, content, version, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)`,
		snapshot.ID,
		snapshot.SessionID,
		snapshot.Path,
		snapshot.Content,
		snapshot.Version,
		formatTime(snapshot.CreatedAt),
		formatTime(snapshot.UpdatedAt),
	); err != nil {
		_ = tx.Rollback()
		return core.FileSnapshot{}, fmt.Errorf("store: create file snapshot: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return core.FileSnapshot{}, fmt.Errorf("store: commit file snapshot: %w", err)
	}
	return snapshot, nil
}

func (s *SQLiteStore) ListFileSnapshots(ctx context.Context, sessionID string) ([]core.FileSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, path, content, version, created_at, updated_at
FROM file_snapshots
WHERE session_id = ?
ORDER BY path ASC, version ASC, created_at ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list file snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []core.FileSnapshot
	for rows.Next() {
		var snapshot core.FileSnapshot
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&snapshot.ID, &snapshot.SessionID, &snapshot.Path, &snapshot.Content, &snapshot.Version, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: scan file snapshot: %w", err)
		}
		snapshot.CreatedAt = mustParseTime(createdAt)
		snapshot.UpdatedAt = mustParseTime(updatedAt)
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate file snapshots: %w", err)
	}
	return snapshots, nil
}
