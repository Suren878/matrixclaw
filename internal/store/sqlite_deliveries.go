package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

const clientDeliveryColumns = `id, type, client, external_key, session_id, run_id, task_id, summary, address_json, reply_once,
payload_json, status, error, created_at, updated_at, finished_at`

func (s *SQLiteStore) CreateClientDelivery(ctx context.Context, delivery core.ClientDelivery) error {
	if err := insertClientDelivery(ctx, s.db, delivery); err != nil {
		return fmt.Errorf("store: create client delivery: %w", err)
	}
	return nil
}

func insertClientDelivery(ctx context.Context, execer sqlExecer, delivery core.ClientDelivery) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO client_deliveries(`+clientDeliveryColumns+`)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		delivery.ID,
		delivery.Type,
		delivery.Client,
		delivery.ExternalKey,
		delivery.SessionID,
		delivery.RunID,
		delivery.TaskID,
		delivery.Summary,
		string(delivery.Address),
		delivery.ReplyOnce,
		string(delivery.Payload),
		string(delivery.Status),
		delivery.Error,
		formatTime(delivery.CreatedAt),
		formatTime(delivery.UpdatedAt),
		nullableTime(delivery.FinishedAt),
	)
	return err
}

func (s *SQLiteStore) ListClientDeliveries(ctx context.Context, filter core.ClientDeliveryFilter) ([]core.ClientDelivery, error) {
	query := `
SELECT ` + clientDeliveryColumns + `
FROM client_deliveries`
	args := []any{}
	clauses := []string{}
	if filter.Client != "" {
		clauses = append(clauses, "client = ?")
		args = append(args, filter.Client)
	}
	if filter.ExternalKey != "" {
		clauses = append(clauses, "external_key = ?")
		args = append(args, filter.ExternalKey)
	}
	if filter.SessionID != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.RunID != "" {
		clauses = append(clauses, "run_id = ?")
		args = append(args, filter.RunID)
	}
	if filter.TaskID != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, filter.TaskID)
	}
	if filter.Type != "" {
		clauses = append(clauses, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(filter.Status))
	}
	if !filter.CreatedAfter.IsZero() {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, formatTime(filter.CreatedAfter))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at ASC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	return queryAll(ctx, s.db, "client deliveries", scanClientDelivery, query, args...)
}

func (s *SQLiteStore) UpdateClientDelivery(ctx context.Context, delivery core.ClientDelivery) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE client_deliveries
SET status = ?, error = ?, updated_at = ?, finished_at = ?
WHERE id = ?`,
		string(delivery.Status),
		delivery.Error,
		formatTime(delivery.UpdatedAt),
		nullableTime(delivery.FinishedAt),
		delivery.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update client delivery: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: update client delivery rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}

func scanClientDelivery(scanner rowScanner) (core.ClientDelivery, error) {
	var delivery core.ClientDelivery
	var status string
	var address string
	var payload string
	var createdAt string
	var updatedAt string
	var finishedAt sql.NullString
	if err := scanner.Scan(
		&delivery.ID,
		&delivery.Type,
		&delivery.Client,
		&delivery.ExternalKey,
		&delivery.SessionID,
		&delivery.RunID,
		&delivery.TaskID,
		&delivery.Summary,
		&address,
		&delivery.ReplyOnce,
		&payload,
		&status,
		&delivery.Error,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		return core.ClientDelivery{}, err
	}
	delivery.Status = core.ClientDeliveryStatus(status)
	if address != "" {
		delivery.Address = json.RawMessage(address)
	}
	if payload != "" {
		delivery.Payload = json.RawMessage(payload)
	}
	delivery.CreatedAt = mustParseTime(createdAt)
	delivery.UpdatedAt = mustParseTime(updatedAt)
	delivery.FinishedAt = parseNullableTime(finishedAt)
	return delivery, nil
}
