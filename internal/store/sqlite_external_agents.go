package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func (s *SQLiteStore) SaveExternalAgentSession(ctx context.Context, attachment externalagents.SessionAttachment) error {
	now := time.Now().UTC()
	if attachment.CreatedAt.IsZero() {
		attachment.CreatedAt = now
	}
	if attachment.UpdatedAt.IsZero() {
		attachment.UpdatedAt = now
	}
	if strings.TrimSpace(attachment.MetadataJSON) == "" {
		attachment.MetadataJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO external_agent_sessions(
    session_id,
    agent_id,
    external_thread_id,
    external_session_id,
    cwd,
    model,
    approval_policy,
    sandbox,
    metadata_json,
    created_at,
    updated_at
)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    agent_id = excluded.agent_id,
    external_thread_id = excluded.external_thread_id,
    external_session_id = excluded.external_session_id,
    cwd = excluded.cwd,
    model = excluded.model,
    approval_policy = excluded.approval_policy,
    sandbox = excluded.sandbox,
    metadata_json = excluded.metadata_json,
    updated_at = excluded.updated_at`,
		attachment.SessionID,
		attachment.AgentID,
		attachment.ExternalThreadID,
		attachment.ExternalSessionID,
		attachment.CWD,
		attachment.Model,
		attachment.ApprovalPolicy,
		attachment.Sandbox,
		attachment.MetadataJSON,
		formatTime(attachment.CreatedAt),
		formatTime(attachment.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save external agent session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetExternalAgentSession(ctx context.Context, sessionID string) (externalagents.SessionAttachment, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT session_id, agent_id, external_thread_id, external_session_id, cwd, model, approval_policy, sandbox, metadata_json, created_at, updated_at
FROM external_agent_sessions
WHERE session_id = ?`, sessionID)

	var attachment externalagents.SessionAttachment
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&attachment.SessionID,
		&attachment.AgentID,
		&attachment.ExternalThreadID,
		&attachment.ExternalSessionID,
		&attachment.CWD,
		&attachment.Model,
		&attachment.ApprovalPolicy,
		&attachment.Sandbox,
		&attachment.MetadataJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return externalagents.SessionAttachment{}, core.ErrNotFound
		}
		return externalagents.SessionAttachment{}, fmt.Errorf("store: get external agent session: %w", err)
	}
	attachment.CreatedAt = mustParseTime(createdAt)
	attachment.UpdatedAt = mustParseTime(updatedAt)
	return attachment, nil
}

func (s *SQLiteStore) DeleteExternalAgentSession(ctx context.Context, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM external_agent_sessions WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("store: delete external agent session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete external agent session rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}
