package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const sessionInputColumns = `id, session_id, target_run_id, mode, status, text, parts_json, client, external_key,
client_capabilities_json, delivery_address_json, reply_once, working_dir, consumed_run_id, error, created_at, updated_at, consumed_at`

func (s *SQLiteStore) CreateSessionInput(ctx context.Context, input core.SessionInput) error {
	if _, err := s.db.ExecContext(ctx, `
	INSERT INTO session_inputs(`+sessionInputColumns+`)
	VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID,
		input.SessionID,
		input.TargetRunID,
		string(input.Mode),
		string(input.Status),
		input.Text,
		marshalSessionInputParts(input.Parts),
		input.Client,
		input.ExternalKey,
		marshalClientCapabilities(input.ClientCapabilities),
		string(input.DeliveryAddress),
		input.ReplyOnce,
		input.WorkingDir,
		input.ConsumedRunID,
		input.Error,
		formatTime(input.CreatedAt),
		formatTime(input.UpdatedAt),
		nullableTime(input.ConsumedAt),
	); err != nil {
		return fmt.Errorf("store: create session input: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateSessionInput(ctx context.Context, input core.SessionInput) error {
	result, err := s.db.ExecContext(ctx, `
	UPDATE session_inputs
	SET target_run_id = ?, mode = ?, status = ?, text = ?, parts_json = ?, client = ?, external_key = ?, client_capabilities_json = ?, delivery_address_json = ?, reply_once = ?, working_dir = ?, consumed_run_id = ?, error = ?, updated_at = ?, consumed_at = ?
	WHERE id = ?`,
		input.TargetRunID,
		string(input.Mode),
		string(input.Status),
		input.Text,
		marshalSessionInputParts(input.Parts),
		input.Client,
		input.ExternalKey,
		marshalClientCapabilities(input.ClientCapabilities),
		string(input.DeliveryAddress),
		input.ReplyOnce,
		input.WorkingDir,
		input.ConsumedRunID,
		input.Error,
		formatTime(input.UpdatedAt),
		nullableTime(input.ConsumedAt),
		input.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update session input: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update session input rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListPendingSessionInputs(ctx context.Context, sessionID string) ([]core.SessionInput, error) {
	query := `
	SELECT ` + sessionInputColumns + `
FROM session_inputs
WHERE status = ?`
	args := []any{string(core.SessionInputStatusPending)}
	if strings.TrimSpace(sessionID) != "" {
		query += ` AND session_id = ?`
		args = append(args, strings.TrimSpace(sessionID))
	}
	query += `
ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list pending session inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var inputs []core.SessionInput
	for rows.Next() {
		input, err := scanSessionInput(rows)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate pending session inputs: %w", err)
	}
	return inputs, nil
}

// HasConsumedSessionInput reports whether the run took a message of the session.
func (s *SQLiteStore) HasConsumedSessionInput(ctx context.Context, sessionID string, runID string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM session_inputs WHERE session_id = ? AND consumed_run_id = ? LIMIT 1`,
		strings.TrimSpace(sessionID), strings.TrimSpace(runID)).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: find consumed session input: %w", err)
	}
	return true, nil
}

func (s *SQLiteStore) NextPendingSessionInput(ctx context.Context, sessionID string) (core.SessionInput, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT `+sessionInputColumns+`
FROM session_inputs
WHERE session_id = ?
  AND status = ?
  AND mode IN (?, ?)
ORDER BY created_at ASC, id ASC
LIMIT 1`,
		strings.TrimSpace(sessionID),
		string(core.SessionInputStatusPending),
		string(core.BusyInputModeQueue),
		string(core.BusyInputModeInterrupt),
	)
	input, err := scanSessionInput(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.SessionInput{}, core.ErrNotFound
		}
		return core.SessionInput{}, fmt.Errorf("store: next pending session input: %w", err)
	}
	return input, nil
}

func (s *SQLiteStore) ListPendingSteerInputs(ctx context.Context, sessionID string, runID string) ([]core.SessionInput, error) {
	rows, err := s.db.QueryContext(ctx, `
	SELECT `+sessionInputColumns+`
FROM session_inputs
WHERE session_id = ?
  AND target_run_id = ?
  AND mode = ?
  AND status = ?
ORDER BY created_at ASC, id ASC`,
		strings.TrimSpace(sessionID),
		strings.TrimSpace(runID),
		string(core.BusyInputModeSteer),
		string(core.SessionInputStatusPending),
	)
	if err != nil {
		return nil, fmt.Errorf("store: list pending steer inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var inputs []core.SessionInput
	for rows.Next() {
		input, err := scanSessionInput(rows)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate pending steer inputs: %w", err)
	}
	return inputs, nil
}

type sessionInputScanner interface {
	Scan(dest ...any) error
}

func scanSessionInput(scanner sessionInputScanner) (core.SessionInput, error) {
	var input core.SessionInput
	var mode string
	var status string
	var partsJSON string
	var capabilitiesJSON string
	var deliveryAddressJSON string
	var createdAt string
	var updatedAt string
	var consumedAt sql.NullString
	if err := scanner.Scan(
		&input.ID,
		&input.SessionID,
		&input.TargetRunID,
		&mode,
		&status,
		&input.Text,
		&partsJSON,
		&input.Client,
		&input.ExternalKey,
		&capabilitiesJSON,
		&deliveryAddressJSON,
		&input.ReplyOnce,
		&input.WorkingDir,
		&input.ConsumedRunID,
		&input.Error,
		&createdAt,
		&updatedAt,
		&consumedAt,
	); err != nil {
		return core.SessionInput{}, err
	}
	input.Mode = core.BusyInputMode(mode)
	input.Status = core.SessionInputStatus(status)
	input.Parts = unmarshalMessageParts(partsJSON)
	input.ClientCapabilities = unmarshalClientCapabilities(capabilitiesJSON)
	if strings.TrimSpace(deliveryAddressJSON) != "" {
		input.DeliveryAddress = json.RawMessage(deliveryAddressJSON)
	}
	input.CreatedAt = mustParseTime(createdAt)
	input.UpdatedAt = mustParseTime(updatedAt)
	if consumedAt.Valid && strings.TrimSpace(consumedAt.String) != "" {
		parsed := mustParseTime(consumedAt.String)
		input.ConsumedAt = &parsed
	}
	return input, nil
}

func marshalSessionInputParts(parts []transcript.MessagePart) string {
	if len(parts) == 0 {
		return ""
	}
	data, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	return string(data)
}
