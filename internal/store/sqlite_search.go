package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
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
SELECT m.id, m.session_id, f.role,
       snippet(message_fts, 1, '[', ']', '...', 12) AS snippet,
       f.provider, f.model, bm25(message_fts) AS rank, m.created_at
FROM message_fts f
JOIN messages m ON m.seq = f.rowid
WHERE message_fts MATCH ?`
	args := []any{query}
	if filter.SessionID != "" {
		sqlQuery += " AND m.session_id = ?"
		args = append(args, filter.SessionID)
	}
	sqlQuery += "\nORDER BY rank\nLIMIT ?"
	args = append(args, limit)
	return queryAll(ctx, s.db, "search results", func(row rowScanner) (core.SearchResult, error) {
		var result core.SearchResult
		var createdAt string
		if err := row.Scan(&result.MessageID, &result.SessionID, &result.Role, &result.Snippet, &result.Provider, &result.Model, &result.Rank, &createdAt); err != nil {
			return core.SearchResult{}, err
		}
		result.CreatedAt = mustParseTime(createdAt)
		return result, nil
	}, sqlQuery, args...)
}

// upsertMessageSearch replaces the search row of a stored message; rows are
// keyed by messages.seq.
func upsertMessageSearch(ctx context.Context, execer sqlExecer, message transcript.Message) error {
	if message.ID == "" {
		return nil
	}
	if _, err := execer.ExecContext(ctx, `DELETE FROM message_fts WHERE rowid = (SELECT seq FROM messages WHERE id = ?)`, message.ID); err != nil {
		return fmt.Errorf("store: clear message search row: %w", err)
	}
	if _, err := execer.ExecContext(ctx, `
INSERT INTO message_fts(rowid, role, content, provider, model)
SELECT seq, ?, ?, ?, ? FROM messages WHERE id = ?`,
		string(message.Role),
		messageSearchContent(message),
		message.Provider,
		message.Model,
		message.ID,
	); err != nil {
		return fmt.Errorf("store: upsert message search row: %w", err)
	}
	return nil
}

func messageSearchContent(message transcript.Message) string {
	parts := []string{message.Content}
	for _, part := range message.Parts {
		switch part.Kind {
		case transcript.MessagePartKindText:
			if part.Text != nil {
				parts = append(parts, part.Text.Text)
			}
		case transcript.MessagePartKindReasoning:
			if part.Reasoning != nil {
				parts = append(parts, part.Reasoning.Text)
			}
		case transcript.MessagePartKindToolCall:
			if part.ToolCall != nil {
				parts = append(parts, part.ToolCall.Name, part.ToolCall.Input)
			}
		case transcript.MessagePartKindToolResult:
			if part.ToolResult != nil {
				parts = append(parts, part.ToolResult.Name, part.ToolResult.Content)
			}
		case transcript.MessagePartKindFinish:
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

// indexUnsearchedMessages indexes messages without a search row, such as
// streamed snapshots whose final update never arrived.
func indexUnsearchedMessages(db *sql.DB) error {
	rows, err := db.Query(`SELECT seq FROM messages WHERE seq NOT IN (SELECT rowid FROM message_fts) ORDER BY seq`)
	if err != nil {
		return fmt.Errorf("store: list unsearched messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pending []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return fmt.Errorf("store: scan unsearched message: %w", err)
		}
		pending = append(pending, seq)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate unsearched messages: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin message search backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, seq := range pending {
		message, err := scanMessage(tx.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE seq = ?`, seq))
		if err != nil {
			return fmt.Errorf("store: read message for search backfill: %w", err)
		}
		if err := upsertMessageSearch(ctx, tx, message); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit message search backfill: %w", err)
	}
	return nil
}
