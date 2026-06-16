package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) SearchMessages(ctx context.Context, filter core.SearchFilter) ([]core.SearchResult, error) {
	query := buildFTSQuery(filter.Query)
	if query == "" {
		return nil, core.ErrInvalidInput
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	sqlQuery := `
SELECT f.message_id, f.session_id, f.role,
       snippet(message_fts, 3, '[', ']', '...', 12) AS snippet,
       f.provider, f.model, bm25(message_fts) AS rank, m.created_at
FROM message_fts f
JOIN messages m ON m.id = f.message_id
WHERE message_fts MATCH ?`
	args := []any{query}
	if strings.TrimSpace(filter.SessionID) != "" {
		sqlQuery += " AND f.session_id = ?"
		args = append(args, strings.TrimSpace(filter.SessionID))
	}
	sqlQuery += "\nORDER BY rank\nLIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("store: search messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []core.SearchResult
	for rows.Next() {
		var result core.SearchResult
		var createdAt string
		if err := rows.Scan(&result.MessageID, &result.SessionID, &result.Role, &result.Snippet, &result.Provider, &result.Model, &result.Rank, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan search result: %w", err)
		}
		result.CreatedAt = mustParseTime(createdAt)
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate search results: %w", err)
	}
	return results, nil
}

func upsertMessageSearch(ctx context.Context, execer sqlExecer, message core.Message) error {
	if strings.TrimSpace(message.ID) == "" {
		return nil
	}
	if _, err := execer.ExecContext(ctx, `DELETE FROM message_fts WHERE message_id = ?`, message.ID); err != nil {
		return fmt.Errorf("store: clear message search row: %w", err)
	}
	if _, err := execer.ExecContext(ctx, `
INSERT INTO message_fts(message_id, session_id, role, content, provider, model)
VALUES(?, ?, ?, ?, ?, ?)`,
		message.ID,
		message.SessionID,
		string(message.Role),
		messageSearchContent(message),
		message.Provider,
		message.Model,
	); err != nil {
		return fmt.Errorf("store: upsert message search row: %w", err)
	}
	return nil
}

func messageSearchContent(message core.Message) string {
	parts := []string{message.Content}
	for _, part := range message.Parts {
		switch part.Kind {
		case core.MessagePartKindText:
			if part.Text != nil {
				parts = append(parts, part.Text.Text)
			}
		case core.MessagePartKindReasoning:
			if part.Reasoning != nil {
				parts = append(parts, part.Reasoning.Text)
			}
		case core.MessagePartKindToolCall:
			if part.ToolCall != nil {
				parts = append(parts, part.ToolCall.Name, part.ToolCall.Input)
			}
		case core.MessagePartKindToolResult:
			if part.ToolResult != nil {
				parts = append(parts, part.ToolResult.Name, part.ToolResult.Content)
			}
		case core.MessagePartKindFinish:
			if part.Finish != nil {
				parts = append(parts, part.Finish.Message)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func buildFTSQuery(query string) string {
	fields := strings.Fields(strings.TrimSpace(query))
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, `"`)
		field = strings.ReplaceAll(field, `"`, `""`)
		if field != "" {
			terms = append(terms, `"`+field+`"`)
		}
	}
	return strings.Join(terms, " ")
}
