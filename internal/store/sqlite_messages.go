package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) SaveMessage(ctx context.Context, message core.Message) error {
	if err := insertMessage(ctx, s.db, message); err != nil {
		return fmt.Errorf("store: save message: %w", err)
	}
	_ = upsertMessageSearch(ctx, s.db, message)
	return nil
}

func (s *SQLiteStore) UpdateMessage(ctx context.Context, message core.Message) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE messages
SET role = ?, content = ?, parts_json = ?, model = ?, provider = ?, updated_at = ?
WHERE id = ?`,
		string(message.Role),
		message.Content,
		marshalMessageParts(message),
		message.Model,
		message.Provider,
		formatTime(messageUpdatedAt(message)),
		message.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update message: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update message rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	_ = upsertMessageSearch(ctx, s.db, message)
	return nil
}

func (s *SQLiteStore) ListMessages(ctx context.Context, sessionID string, limit int) ([]core.Message, error) {
	query := `
SELECT id, session_id, run_id, role, content, parts_json, model, provider, created_at, updated_at
FROM messages
WHERE session_id = ?
ORDER BY created_at DESC`
	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		rows, err = s.db.QueryContext(ctx, query+`
LIMIT ?`, sessionID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query, sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	defer rows.Close()

	var messages []core.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate messages: %w", err)
	}
	reverseMessages(messages)
	return messages, nil
}

func (s *SQLiteStore) CreateRun(ctx context.Context, run core.Run) error {
	if err := insertRun(ctx, s.db, run); err != nil {
		return fmt.Errorf("store: create run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, user_message_id, client, external_key, client_capabilities_json, status, error, started_at, finished_at, updated_at
FROM runs
WHERE id = ?`, runID)

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Run{}, core.ErrNotFound
		}
		return core.Run{}, fmt.Errorf("store: get run: %w", err)
	}
	return run, nil
}

func (s *SQLiteStore) GetActiveRunBySession(ctx context.Context, sessionID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT id, session_id, user_message_id, client, external_key, client_capabilities_json, status, error, started_at, finished_at, updated_at
	FROM runs
WHERE session_id = ?
  AND status IN (?, ?, ?)
ORDER BY started_at DESC, updated_at DESC
LIMIT 1`,
		strings.TrimSpace(sessionID),
		string(core.RunStatusAccepted),
		string(core.RunStatusRunning),
		string(core.RunStatusWaitingApproval),
	)

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Run{}, core.ErrNotFound
		}
		return core.Run{}, fmt.Errorf("store: get active run by session: %w", err)
	}
	return run, nil
}

func (s *SQLiteStore) ListActiveRuns(ctx context.Context) ([]core.Run, error) {
	rows, err := s.db.QueryContext(ctx, `
	SELECT id, session_id, user_message_id, client, external_key, client_capabilities_json, status, error, started_at, finished_at, updated_at
	FROM runs
	WHERE status IN (?, ?, ?)
	ORDER BY started_at ASC, updated_at ASC`,
		string(core.RunStatusAccepted),
		string(core.RunStatusRunning),
		string(core.RunStatusWaitingApproval),
	)
	if err != nil {
		return nil, fmt.Errorf("store: list active runs: %w", err)
	}
	defer rows.Close()

	runs := []core.Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan active run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate active runs: %w", err)
	}
	return runs, nil
}

func (s *SQLiteStore) UpdateRun(ctx context.Context, run core.Run) error {
	result, err := updateRun(ctx, s.db, run)
	if err != nil {
		return fmt.Errorf("store: update run: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update run rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CompleteRun(ctx context.Context, assistantMessage core.Message, run core.Run) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin complete run: %w", err)
	}

	if err := insertMessage(ctx, tx, assistantMessage); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: insert assistant message: %w", err)
	}

	if _, err := updateRun(ctx, tx, run); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: update completed run: %w", err)
	}

	if err := touchSession(ctx, tx, assistantMessage.SessionID, assistantMessage.CreatedAt); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: update session after complete run: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit complete run: %w", err)
	}
	_ = upsertMessageSearch(ctx, s.db, assistantMessage)
	return nil
}

func (s *SQLiteStore) AcceptMessage(ctx context.Context, message core.Message, run core.Run, deliveries ...core.ClientDelivery) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin accept message: %w", err)
	}

	if err := insertMessage(ctx, tx, message); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: insert accepted message: %w", err)
	}

	if err := insertRun(ctx, tx, run); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: insert accepted run: %w", err)
	}

	for _, delivery := range deliveries {
		if err := insertClientDelivery(ctx, tx, delivery); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: insert accepted delivery: %w", err)
		}
	}

	if err := touchSession(ctx, tx, message.SessionID, message.CreatedAt); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: update session after accepted message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit accept message: %w", err)
	}
	_ = upsertMessageSearch(ctx, s.db, message)
	return nil
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type messageScanner interface {
	Scan(dest ...any) error
}

type runScanner interface {
	Scan(dest ...any) error
}

func insertMessage(ctx context.Context, execer sqlExecer, message core.Message) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO messages(id, session_id, run_id, role, content, parts_json, model, provider, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID,
		message.SessionID,
		message.RunID,
		string(message.Role),
		message.Content,
		marshalMessageParts(message),
		message.Model,
		message.Provider,
		formatTime(message.CreatedAt),
		formatTime(messageUpdatedAt(message)),
	)
	return err
}

func scanMessage(scanner messageScanner) (core.Message, error) {
	var message core.Message
	var role string
	var partsJSON string
	var model string
	var provider string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&message.ID, &message.SessionID, &message.RunID, &role, &message.Content, &partsJSON, &model, &provider, &createdAt, &updatedAt); err != nil {
		return core.Message{}, fmt.Errorf("store: scan message: %w", err)
	}
	message.Role = core.MessageRole(role)
	message.Parts = unmarshalMessageParts(partsJSON)
	message.Model = model
	message.Provider = provider
	message.CreatedAt = mustParseTime(createdAt)
	message.UpdatedAt = message.CreatedAt
	if strings.TrimSpace(updatedAt) != "" {
		message.UpdatedAt = mustParseTime(updatedAt)
	}
	return message, nil
}

func insertRun(ctx context.Context, execer sqlExecer, run core.Run) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO runs(id, session_id, user_message_id, client, external_key, client_capabilities_json, status, error, started_at, finished_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.SessionID,
		run.UserMessageID,
		run.Client,
		run.ExternalKey,
		marshalClientCapabilities(run.ClientCapabilities),
		string(run.Status),
		run.Error,
		formatTime(run.StartedAt),
		nullableTime(run.FinishedAt),
		formatTime(run.UpdatedAt),
	)
	return err
}

func scanRun(scanner runScanner) (core.Run, error) {
	var run core.Run
	var status string
	var capabilitiesJSON string
	var startedAt string
	var finishedAt sql.NullString
	var updatedAt string
	if err := scanner.Scan(&run.ID, &run.SessionID, &run.UserMessageID, &run.Client, &run.ExternalKey, &capabilitiesJSON, &status, &run.Error, &startedAt, &finishedAt, &updatedAt); err != nil {
		return core.Run{}, err
	}
	run.ClientCapabilities = unmarshalClientCapabilities(capabilitiesJSON)
	run.Status = core.RunStatus(status)
	run.StartedAt = mustParseTime(startedAt)
	if finishedAt.Valid {
		parsed := mustParseTime(finishedAt.String)
		run.FinishedAt = &parsed
	}
	run.UpdatedAt = mustParseTime(updatedAt)
	return run, nil
}

func updateRun(ctx context.Context, execer sqlExecer, run core.Run) (sql.Result, error) {
	return execer.ExecContext(ctx, `
UPDATE runs
SET client_capabilities_json = ?, status = ?, error = ?, finished_at = ?, updated_at = ?
WHERE id = ?`,
		marshalClientCapabilities(run.ClientCapabilities),
		string(run.Status),
		run.Error,
		nullableTime(run.FinishedAt),
		formatTime(run.UpdatedAt),
		run.ID,
	)
}

func touchSession(ctx context.Context, execer sqlExecer, sessionID string, updatedAt time.Time) error {
	_, err := execer.ExecContext(ctx, `
UPDATE sessions
SET updated_at = ?
WHERE id = ?`,
		formatTime(updatedAt),
		sessionID,
	)
	return err
}

func messageUpdatedAt(message core.Message) time.Time {
	if !message.UpdatedAt.IsZero() {
		return message.UpdatedAt
	}
	return message.CreatedAt
}

func reverseMessages(messages []core.Message) {
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
}

func marshalMessageParts(message core.Message) string {
	parts := core.NormalizeMessageParts(message.Content, message.Parts)
	if len(parts) == 0 {
		return ""
	}
	body, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	return string(body)
}

func unmarshalMessageParts(raw string) []core.MessagePart {
	if raw == "" {
		return nil
	}

	var parts []core.MessagePart
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return nil
	}
	return parts
}
