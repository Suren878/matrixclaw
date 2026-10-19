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
    metadata_json,
    created_at,
    updated_at
)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    agent_id = excluded.agent_id,
    external_thread_id = excluded.external_thread_id,
    external_session_id = excluded.external_session_id,
    cwd = excluded.cwd,
    model = excluded.model,
    metadata_json = excluded.metadata_json,
    updated_at = excluded.updated_at`,
		attachment.SessionID,
		attachment.AgentID,
		attachment.ExternalThreadID,
		attachment.ExternalSessionID,
		attachment.CWD,
		attachment.Model,
		attachment.MetadataJSON,
		formatTime(attachment.CreatedAt),
		formatTime(attachment.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save external agent session: %w", err)
	}
	return nil
}

const externalAgentSessionColumns = `session_id, agent_id, external_thread_id, external_session_id, cwd, model, metadata_json, created_at, updated_at`

func (s *SQLiteStore) GetExternalAgentSession(ctx context.Context, sessionID string) (externalagents.SessionAttachment, error) {
	attachment, err := scanExternalAgentSession(s.db.QueryRowContext(ctx, `
SELECT `+externalAgentSessionColumns+`
FROM external_agent_sessions
WHERE session_id = ?`, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return externalagents.SessionAttachment{}, core.ErrNotFound
	}
	if err != nil {
		return externalagents.SessionAttachment{}, fmt.Errorf("store: get external agent session: %w", err)
	}
	return attachment, nil
}

func (s *SQLiteStore) ListExternalAgentSessions(ctx context.Context, sessionIDs []string) ([]externalagents.SessionAttachment, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		args[i] = id
	}
	return queryAll(ctx, s.db, "external agent sessions", scanExternalAgentSession, `
SELECT `+externalAgentSessionColumns+`
FROM external_agent_sessions
WHERE session_id IN (?`+strings.Repeat(`, ?`, len(sessionIDs)-1)+`)`, args...)
}

func scanExternalAgentSession(row rowScanner) (externalagents.SessionAttachment, error) {
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
		&attachment.MetadataJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return externalagents.SessionAttachment{}, err
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
