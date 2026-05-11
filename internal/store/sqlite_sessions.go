package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) CreateSession(ctx context.Context, session core.Session) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(id, title, working_dir, provider_id, model_id, permission_mode, status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
		session.Title,
		session.WorkingDir,
		session.ProviderID,
		session.ModelID,
		string(core.NormalizePermissionMode(string(session.PermissionMode))),
		string(session.Status),
		formatTime(session.CreatedAt),
		formatTime(session.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, sessionID string) (core.Session, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, title, working_dir, provider_id, model_id, permission_mode, status, created_at, updated_at
FROM sessions
WHERE id = ?`, sessionID)

	var session core.Session
	var status string
	var createdAt string
	var updatedAt string
	var permissionMode string
	if err := row.Scan(&session.ID, &session.Title, &session.WorkingDir, &session.ProviderID, &session.ModelID, &permissionMode, &status, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Session{}, core.ErrNotFound
		}
		return core.Session{}, fmt.Errorf("store: get session: %w", err)
	}

	session.Status = core.SessionStatus(status)
	session.PermissionMode = core.NormalizePermissionMode(permissionMode)
	session.CreatedAt = mustParseTime(createdAt)
	session.UpdatedAt = mustParseTime(updatedAt)
	return session, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, filter core.SessionListFilter) ([]core.Session, error) {
	query := `
SELECT id, title, working_dir, provider_id, model_id, permission_mode, status, created_at, updated_at
FROM sessions`
	args := []any{}
	if !filter.IncludeArchived {
		query += ` WHERE status != ?`
		args = append(args, string(core.SessionStatusArchived))
	}
	query += ` ORDER BY updated_at DESC, created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []core.Session
	for rows.Next() {
		var session core.Session
		var status string
		var permissionMode string
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&session.ID, &session.Title, &session.WorkingDir, &session.ProviderID, &session.ModelID, &permissionMode, &status, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: scan session: %w", err)
		}
		session.Status = core.SessionStatus(status)
		session.PermissionMode = core.NormalizePermissionMode(permissionMode)
		session.CreatedAt = mustParseTime(createdAt)
		session.UpdatedAt = mustParseTime(updatedAt)
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate sessions: %w", err)
	}
	return sessions, nil
}

func (s *SQLiteStore) UpdateSession(ctx context.Context, session core.Session) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE sessions
SET title = ?, working_dir = ?, provider_id = ?, model_id = ?, permission_mode = ?, status = ?, updated_at = ?
WHERE id = ?`,
		session.Title,
		session.WorkingDir,
		session.ProviderID,
		session.ModelID,
		string(core.NormalizePermissionMode(string(session.PermissionMode))),
		string(session.Status),
		formatTime(session.UpdatedAt),
		session.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update session rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM sessions
WHERE id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete session rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) SaveBinding(ctx context.Context, binding core.ClientBinding) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO client_bindings(client, external_key, session_id, updated_at)
VALUES(?, ?, ?, ?)
ON CONFLICT(client, external_key) DO UPDATE SET
	session_id = excluded.session_id,
	updated_at = excluded.updated_at`,
		binding.Client,
		binding.ExternalKey,
		binding.SessionID,
		formatTime(binding.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save binding: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetBinding(ctx context.Context, client string, externalKey string) (core.ClientBinding, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT client, external_key, session_id, updated_at
FROM client_bindings
WHERE client = ? AND external_key = ?`,
		client,
		externalKey,
	)

	var binding core.ClientBinding
	var updatedAt string
	if err := row.Scan(&binding.Client, &binding.ExternalKey, &binding.SessionID, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.ClientBinding{}, core.ErrBindingNotFound
		}
		return core.ClientBinding{}, fmt.Errorf("store: get binding: %w", err)
	}
	binding.UpdatedAt = mustParseTime(updatedAt)
	return binding, nil
}
