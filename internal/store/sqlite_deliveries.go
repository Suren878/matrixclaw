package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) CreateClientDelivery(ctx context.Context, delivery core.ClientDelivery) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO client_deliveries(id, type, client, external_key, session_id, run_id, task_id, summary, address_json, status, error, created_at, updated_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		delivery.ID,
		delivery.Type,
		delivery.Client,
		delivery.ExternalKey,
		delivery.SessionID,
		delivery.RunID,
		delivery.TaskID,
		delivery.Summary,
		string(delivery.Address),
		string(delivery.Status),
		delivery.Error,
		formatTime(delivery.CreatedAt),
		formatTime(delivery.UpdatedAt),
		nullableTime(delivery.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create client delivery: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListClientDeliveries(ctx context.Context, filter core.ClientDeliveryFilter) ([]core.ClientDelivery, error) {
	query := `
SELECT id, type, client, external_key, session_id, run_id, task_id, summary, address_json, status, error, created_at, updated_at, finished_at
FROM client_deliveries`
	args := []any{}
	clauses := []string{}
	if strings.TrimSpace(filter.Client) != "" {
		clauses = append(clauses, "client = ?")
		args = append(args, strings.TrimSpace(filter.Client))
	}
	if strings.TrimSpace(filter.ExternalKey) != "" {
		clauses = append(clauses, "external_key = ?")
		args = append(args, strings.TrimSpace(filter.ExternalKey))
	}
	if strings.TrimSpace(filter.SessionID) != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, strings.TrimSpace(filter.SessionID))
	}
	if strings.TrimSpace(filter.RunID) != "" {
		clauses = append(clauses, "run_id = ?")
		args = append(args, strings.TrimSpace(filter.RunID))
	}
	if strings.TrimSpace(filter.TaskID) != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, strings.TrimSpace(filter.TaskID))
	}
	if strings.TrimSpace(filter.Type) != "" {
		clauses = append(clauses, "type = ?")
		args = append(args, strings.TrimSpace(filter.Type))
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

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list client deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := []core.ClientDelivery{}
	for rows.Next() {
		delivery, err := scanClientDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate client deliveries: %w", err)
	}
	return deliveries, nil
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

type clientDeliveryScanner interface {
	Scan(dest ...any) error
}

func scanClientDelivery(scanner clientDeliveryScanner) (core.ClientDelivery, error) {
	var delivery core.ClientDelivery
	var status string
	var address string
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
		&status,
		&delivery.Error,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		return core.ClientDelivery{}, fmt.Errorf("store: scan client delivery: %w", err)
	}
	delivery.Status = core.ClientDeliveryStatus(status)
	if address != "" {
		delivery.Address = json.RawMessage(address)
	}
	delivery.CreatedAt = mustParseTime(createdAt)
	delivery.UpdatedAt = mustParseTime(updatedAt)
	if finishedAt.Valid {
		parsed := mustParseTime(finishedAt.String)
		delivery.FinishedAt = &parsed
	}
	return delivery, nil
}
