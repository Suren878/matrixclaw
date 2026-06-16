package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) SaveUsageRecord(ctx context.Context, record core.UsageRecord) error {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.SessionID) == "" || strings.TrimSpace(record.RunID) == "" {
		return core.ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_usage(
    id, session_id, run_id, message_id, provider, model,
    input_tokens, output_tokens, total_tokens, cached_tokens, reasoning_tokens,
    estimated, provider_raw, created_at
)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    message_id = excluded.message_id,
    provider = excluded.provider,
    model = excluded.model,
    input_tokens = excluded.input_tokens,
    output_tokens = excluded.output_tokens,
    total_tokens = excluded.total_tokens,
    cached_tokens = excluded.cached_tokens,
    reasoning_tokens = excluded.reasoning_tokens,
    estimated = excluded.estimated,
    provider_raw = excluded.provider_raw,
    created_at = excluded.created_at`,
		record.ID,
		record.SessionID,
		record.RunID,
		record.MessageID,
		record.Provider,
		record.Model,
		record.InputTokens,
		record.OutputTokens,
		record.TotalTokens,
		record.CachedTokens,
		record.ReasoningTokens,
		boolInt(record.Estimated),
		record.ProviderRaw,
		formatTime(record.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save usage record: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListUsageRecords(ctx context.Context, filter core.UsageFilter) ([]core.UsageRecord, error) {
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.RunID = strings.TrimSpace(filter.RunID)
	query := `
SELECT id, session_id, run_id, message_id, provider, model,
       input_tokens, output_tokens, total_tokens, cached_tokens, reasoning_tokens,
       estimated, provider_raw, created_at
FROM run_usage`
	args := make([]any, 0, 3)
	clauses := make([]string, 0, 2)
	if filter.SessionID != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.RunID != "" {
		clauses = append(clauses, "run_id = ?")
		args = append(args, filter.RunID)
	}
	if len(clauses) > 0 {
		query += "\nWHERE " + strings.Join(clauses, " AND ")
	}
	query += "\nORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list usage records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []core.UsageRecord
	for rows.Next() {
		record, err := scanUsageRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate usage records: %w", err)
	}
	reverseUsageRecords(records)
	return records, nil
}

func scanUsageRecord(scanner interface{ Scan(dest ...any) error }) (core.UsageRecord, error) {
	var record core.UsageRecord
	var estimated int
	var createdAt string
	if err := scanner.Scan(
		&record.ID,
		&record.SessionID,
		&record.RunID,
		&record.MessageID,
		&record.Provider,
		&record.Model,
		&record.InputTokens,
		&record.OutputTokens,
		&record.TotalTokens,
		&record.CachedTokens,
		&record.ReasoningTokens,
		&estimated,
		&record.ProviderRaw,
		&createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return core.UsageRecord{}, core.ErrNotFound
		}
		return core.UsageRecord{}, fmt.Errorf("store: scan usage record: %w", err)
	}
	record.Estimated = estimated != 0
	record.CreatedAt = mustParseTime(createdAt)
	return record, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func reverseUsageRecords(records []core.UsageRecord) {
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
}
