package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// AppendMessage stores a message, indexes it for search and returns its seq.
func (s *SQLiteStore) AppendMessage(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := insertMessage(ctx, s.db, message)
	if err != nil {
		return 0, fmt.Errorf("store: save message: %w", err)
	}
	_ = upsertMessageSearch(ctx, s.db, message)
	return seq, nil
}

// SaveMessageProgress stores a streaming snapshot without search indexing and returns its seq.
func (s *SQLiteStore) SaveMessageProgress(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := insertMessage(ctx, s.db, message)
	if err != nil {
		return 0, fmt.Errorf("store: save message progress: %w", err)
	}
	return seq, nil
}

func (s *SQLiteStore) UpdateMessage(ctx context.Context, message transcript.Message) error {
	if err := updateMessageRow(ctx, s.db, message); err != nil {
		return err
	}
	_ = upsertMessageSearch(ctx, s.db, message)
	return nil
}

func (s *SQLiteStore) UpdateMessageProgress(ctx context.Context, message transcript.Message) error {
	return updateMessageRow(ctx, s.db, message)
}

func updateMessageRow(ctx context.Context, execer sqlExecer, message transcript.Message) error {
	result, err := execer.ExecContext(ctx, `
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
	return nil
}

func (s *SQLiteStore) GetMessage(ctx context.Context, messageID string) (transcript.Message, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = ?`, strings.TrimSpace(messageID))
	message, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return transcript.Message{}, core.ErrNotFound
	}
	if err != nil {
		return transcript.Message{}, fmt.Errorf("store: get message: %w", err)
	}
	return message, nil
}

// HasToolResult reports whether a tool message of the session answers toolCallID.
func (s *SQLiteStore) HasToolResult(ctx context.Context, sessionID string, runID string, toolCallID string) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM messages m, json_each(CASE WHEN json_valid(m.parts_json) THEN m.parts_json ELSE '[]' END) p
    WHERE m.session_id = ?
      AND m.run_id = ?
      AND m.role = 'tool'
      AND json_extract(p.value, '$.tool_result.tool_call_id') = ?
)`, sessionID, runID, strings.TrimSpace(toolCallID)).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("store: has tool result: %w", err)
	}
	return found, nil
}

// ListMessages returns the latest limit messages (all when limit is 0) in seq order.
func (s *SQLiteStore) ListMessages(ctx context.Context, sessionID string, limit int) ([]transcript.Message, error) {
	query := `SELECT ` + messageColumns + `
FROM messages
WHERE session_id = ?
ORDER BY seq DESC`
	args := []any{sessionID}
	if limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, limit)
	}
	messages, err := s.queryMessages(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	slices.Reverse(messages)
	return messages, nil
}

// ListRunMessages returns the messages of the run in seq order.
func (s *SQLiteStore) ListRunMessages(ctx context.Context, sessionID string, runID string) ([]transcript.Message, error) {
	return s.queryMessages(ctx, `SELECT `+messageColumns+`
FROM messages
WHERE session_id = ? AND run_id = ?
ORDER BY seq ASC`, sessionID, runID)
}

// ListMessagesAfter returns up to limit messages (all when limit is 0) with seq > afterSeq, ascending.
func (s *SQLiteStore) ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error) {
	query := `SELECT ` + messageColumns + `
FROM messages
WHERE session_id = ? AND seq > ?
ORDER BY seq ASC`
	args := []any{sessionID, afterSeq}
	if limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, limit)
	}
	return s.queryMessages(ctx, query, args...)
}

// LatestCompaction returns the session's newest context boundary.
func (s *SQLiteStore) LatestCompaction(ctx context.Context, sessionID string) (transcript.Message, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+messageColumns+`
FROM messages
WHERE session_id = ? AND compaction_json <> ''
ORDER BY seq DESC
LIMIT 1`, sessionID)
	message, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return transcript.Message{}, core.ErrNotFound
	}
	if err != nil {
		return transcript.Message{}, fmt.Errorf("store: get latest compaction: %w", err)
	}
	return message, nil
}

func (s *SQLiteStore) queryMessages(ctx context.Context, query string, args ...any) ([]transcript.Message, error) {
	return queryAll(ctx, s.db, "messages", scanMessage, query, args...)
}

func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, runID)

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
SELECT `+runColumns+`
FROM runs
WHERE session_id = ?
  AND status IN (?, ?, ?, ?)
ORDER BY started_at DESC, updated_at DESC
LIMIT 1`,
		strings.TrimSpace(sessionID),
		string(core.RunStatusAccepted),
		string(core.RunStatusRunning),
		string(core.RunStatusWaitingApproval),
		string(core.RunStatusWaitingEvents),
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

// GetLatestRunBySession returns the session's newest run, ordered by its user message.
func (s *SQLiteStore) GetLatestRunBySession(ctx context.Context, sessionID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE session_id = ?
ORDER BY (SELECT seq FROM messages WHERE messages.id = runs.user_message_id) DESC
LIMIT 1`, strings.TrimSpace(sessionID))

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Run{}, core.ErrNotFound
		}
		return core.Run{}, fmt.Errorf("store: get latest run by session: %w", err)
	}
	return run, nil
}

func (s *SQLiteStore) ListSessionRuns(ctx context.Context, sessionID string, limit int) ([]core.Run, error) {
	query := `
SELECT ` + runColumns + `
FROM runs
WHERE session_id = ?
ORDER BY (SELECT seq FROM messages WHERE messages.id = runs.user_message_id) DESC`
	args := []any{strings.TrimSpace(sessionID)}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	return queryAll(ctx, s.db, "session runs", scanRun, query, args...)
}

func (s *SQLiteStore) ListActiveRuns(ctx context.Context) ([]core.Run, error) {
	return queryAll(ctx, s.db, "active runs", scanRun, `
SELECT `+runColumns+`
FROM runs
WHERE status IN (?, ?, ?, ?)
ORDER BY started_at ASC, updated_at ASC`,
		string(core.RunStatusAccepted),
		string(core.RunStatusRunning),
		string(core.RunStatusWaitingApproval),
		string(core.RunStatusWaitingEvents),
	)
}

// SealRun writes the run together with the reply it ends or parks with, if
// any, in one transaction: a saved reply is updated, another one inserted. An
// ended run keeps how it ended (core.ErrRunEnded), and the reply is not written.
func (s *SQLiteStore) SealRun(ctx context.Context, run core.Run, reply *transcript.Message, replySaved bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin seal run: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if reply != nil && replySaved {
		err = updateMessageRow(ctx, tx, *reply)
	} else if reply != nil {
		_, err = insertMessage(ctx, tx, *reply)
		if err == nil {
			err = touchSession(ctx, tx, reply.SessionID, reply.CreatedAt)
		}
	}
	if err != nil {
		return fmt.Errorf("store: seal run reply: %w", err)
	}
	if err := updateRun(ctx, tx, run); err != nil {
		return fmt.Errorf("store: seal run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit seal run: %w", err)
	}
	if reply != nil {
		_ = upsertMessageSearch(ctx, s.db, *reply)
	}
	return nil
}

// TouchRun records activity of a run without changing anything else of it.
func (s *SQLiteStore) TouchRun(ctx context.Context, runID string, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE runs SET updated_at = ? WHERE id = ?`, formatTime(at), runID); err != nil {
		return fmt.Errorf("store: touch run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AcceptMessage(ctx context.Context, message transcript.Message, run core.Run, deliveries ...core.ClientDelivery) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin accept message: %w", err)
	}

	if _, err := insertMessage(ctx, tx, message); err != nil {
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

const messageColumns = `id, session_id, run_id, role, origin, content, parts_json, model, provider, created_at, updated_at, seq, compaction_json`

const runColumns = `id, session_id, user_message_id, client, external_key, client_capabilities_json, status, error, stop_reason, continues_run_id, trigger_kind, started_at, finished_at, updated_at`

// insertMessage assigns the next database-wide seq in the same statement and returns it.
func insertMessage(ctx context.Context, queryer sqlExecer, message transcript.Message) (int64, error) {
	var seq int64
	err := queryer.QueryRowContext(ctx, `
INSERT INTO messages(id, session_id, run_id, role, origin, content, parts_json, model, provider, created_at, updated_at, compaction_json, seq)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages))
RETURNING seq`,
		message.ID,
		message.SessionID,
		message.RunID,
		string(message.Role),
		string(message.Origin),
		message.Content,
		marshalMessageParts(message),
		message.Model,
		message.Provider,
		formatTime(message.CreatedAt),
		formatTime(messageUpdatedAt(message)),
		marshalCompaction(message.Compaction),
	).Scan(&seq)
	return seq, err
}

func scanMessage(scanner rowScanner) (transcript.Message, error) {
	var message transcript.Message
	var role string
	var origin string
	var partsJSON string
	var model string
	var provider string
	var createdAt string
	var updatedAt string
	var compactionJSON string
	if err := scanner.Scan(&message.ID, &message.SessionID, &message.RunID, &role, &origin, &message.Content, &partsJSON, &model, &provider, &createdAt, &updatedAt, &message.Seq, &compactionJSON); err != nil {
		return transcript.Message{}, err
	}
	message.Role = transcript.MessageRole(role)
	message.Origin = transcript.Origin(origin)
	message.Parts = unmarshalMessageParts(partsJSON)
	message.Compaction = unmarshalCompaction(compactionJSON)
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
INSERT INTO runs(`+runColumns+`)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.SessionID,
		run.UserMessageID,
		run.Client,
		run.ExternalKey,
		marshalClientCapabilities(run.ClientCapabilities),
		string(run.Status),
		run.Error,
		string(run.StopReason),
		run.ContinuesRunID,
		string(run.Trigger),
		formatTime(run.StartedAt),
		nullableTime(run.FinishedAt),
		formatTime(run.UpdatedAt),
	)
	return err
}

func scanRun(scanner rowScanner) (core.Run, error) {
	var run core.Run
	var status string
	var stopReason string
	var trigger string
	var capabilitiesJSON string
	var startedAt string
	var finishedAt sql.NullString
	var updatedAt string
	if err := scanner.Scan(&run.ID, &run.SessionID, &run.UserMessageID, &run.Client, &run.ExternalKey, &capabilitiesJSON, &status, &run.Error, &stopReason, &run.ContinuesRunID, &trigger, &startedAt, &finishedAt, &updatedAt); err != nil {
		return core.Run{}, err
	}
	run.ClientCapabilities = unmarshalClientCapabilities(capabilitiesJSON)
	run.Status = core.RunStatus(status)
	run.StopReason = agent.StopReason(stopReason)
	run.Trigger = core.RunTrigger(trigger)
	run.StartedAt = mustParseTime(startedAt)
	run.FinishedAt = parseNullableTime(finishedAt)
	run.UpdatedAt = mustParseTime(updatedAt)
	return run, nil
}

// updateRun writes the run unless it already ended with another status: an
// ended run keeps how it ended (core.ErrRunEnded).
func updateRun(ctx context.Context, execer sqlExecer, run core.Run) error {
	result, err := execer.ExecContext(ctx, `
UPDATE runs
SET client_capabilities_json = ?, status = ?, error = ?, stop_reason = ?, finished_at = ?, updated_at = ?
WHERE id = ? AND (status NOT IN ('completed', 'failed', 'canceled') OR status = ?)`,
		marshalClientCapabilities(run.ClientCapabilities),
		string(run.Status),
		run.Error,
		string(run.StopReason),
		nullableTime(run.FinishedAt),
		formatTime(run.UpdatedAt),
		run.ID,
		string(run.Status),
	)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count > 0 {
		return err
	}
	var exists int
	err = execer.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE id = ?`, run.ID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return core.ErrNotFound
	}
	if err != nil {
		return err
	}
	return core.ErrRunEnded
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

func messageUpdatedAt(message transcript.Message) time.Time {
	if !message.UpdatedAt.IsZero() {
		return message.UpdatedAt
	}
	return message.CreatedAt
}

func marshalCompaction(compaction *transcript.Compaction) string {
	if compaction == nil {
		return ""
	}
	body, err := json.Marshal(compaction)
	if err != nil {
		return ""
	}
	return string(body)
}

func unmarshalCompaction(raw string) *transcript.Compaction {
	if raw == "" {
		return nil
	}
	var compaction transcript.Compaction
	if err := json.Unmarshal([]byte(raw), &compaction); err != nil {
		return nil
	}
	return &compaction
}

func marshalMessageParts(message transcript.Message) string {
	parts := transcript.NormalizeMessageParts(message.Content, message.Parts)
	if len(parts) == 0 {
		return ""
	}
	body, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	return string(body)
}

func unmarshalMessageParts(raw string) []transcript.MessagePart {
	if raw == "" {
		return nil
	}

	var parts []transcript.MessagePart
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return nil
	}
	return parts
}
