package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// SaveRunStep appends step as the next step number of its run.
func (s *SQLiteStore) SaveRunStep(ctx context.Context, step core.RunStep) error {
	runID := strings.TrimSpace(step.RunID)
	if runID == "" {
		return core.ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_steps(run_id, step, model, provider, prompt_tokens, cache_read_tokens, cache_write_tokens,
    output_tokens, reasoning_tokens, stop_reason, latency_ms, tool_calls, created_at)
VALUES(?, (SELECT COALESCE(MAX(step), 0) + 1 FROM run_steps WHERE run_id = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, runID, step.Model, step.Provider,
		step.PromptTokens, step.CacheReadTokens, step.CacheWriteTokens,
		step.OutputTokens, step.ReasoningTokens, step.StopReason,
		step.LatencyMillis, step.ToolCalls, formatTime(step.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save run step: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListRunSteps(ctx context.Context, runID string) ([]core.RunStep, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, step, model, provider, prompt_tokens, cache_read_tokens, cache_write_tokens,
       output_tokens, reasoning_tokens, stop_reason, latency_ms, tool_calls, created_at
FROM run_steps
WHERE run_id = ?
ORDER BY step`, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("store: list run steps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []core.RunStep
	for rows.Next() {
		var step core.RunStep
		var createdAt string
		if err := rows.Scan(&step.RunID, &step.Step, &step.Model, &step.Provider,
			&step.PromptTokens, &step.CacheReadTokens, &step.CacheWriteTokens,
			&step.OutputTokens, &step.ReasoningTokens, &step.StopReason,
			&step.LatencyMillis, &step.ToolCalls, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan run step: %w", err)
		}
		step.CreatedAt = mustParseTime(createdAt)
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate run steps: %w", err)
	}
	return steps, nil
}

// ListUsageRecords sums run_steps per run, oldest run first; Limit keeps the latest runs.
func (s *SQLiteStore) ListUsageRecords(ctx context.Context, filter core.UsageFilter) ([]core.UsageRecord, error) {
	// MAX(s.step) is the only min/max aggregate, so SQLite takes the bare
	// provider, model and created_at columns from each run's last step.
	query := `
SELECT r.session_id, s.run_id, MAX(s.step), s.provider, s.model,
       SUM(s.prompt_tokens), SUM(s.cache_read_tokens), SUM(s.cache_write_tokens),
       SUM(s.output_tokens), SUM(s.reasoning_tokens), s.created_at
FROM run_steps s
JOIN runs r ON r.id = s.run_id`
	args := make([]any, 0, 3)
	clauses := make([]string, 0, 2)
	if sessionID := strings.TrimSpace(filter.SessionID); sessionID != "" {
		clauses = append(clauses, "r.session_id = ?")
		args = append(args, sessionID)
	}
	if runID := strings.TrimSpace(filter.RunID); runID != "" {
		clauses = append(clauses, "s.run_id = ?")
		args = append(args, runID)
	}
	if len(clauses) > 0 {
		query += "\nWHERE " + strings.Join(clauses, " AND ")
	}
	query += "\nGROUP BY s.run_id\nORDER BY r.rowid DESC"
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
		var record core.UsageRecord
		var updatedAt string
		if err := rows.Scan(&record.SessionID, &record.RunID, &record.Steps, &record.Provider, &record.Model,
			&record.PromptTokens, &record.CacheReadTokens, &record.CacheWriteTokens,
			&record.OutputTokens, &record.ReasoningTokens, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: scan usage record: %w", err)
		}
		record.UpdatedAt = mustParseTime(updatedAt)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate usage records: %w", err)
	}
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
	return records, nil
}
