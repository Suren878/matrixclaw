package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
)

// GetSessionTodo returns the session's todo list; an empty one when none was written.
func (s *SQLiteStore) GetSessionTodo(ctx context.Context, sessionID string) (todo.List, error) {
	sessionID = strings.TrimSpace(sessionID)
	list := todo.List{SessionID: sessionID}
	var items, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT items_json, chain_run_id, updated_run_id, updated_at FROM session_todos WHERE session_id = ?`, sessionID).
		Scan(&items, &list.ChainRunID, &list.UpdatedRunID, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return list, nil
	}
	if err != nil {
		return todo.List{}, fmt.Errorf("store: get session todo: %w", err)
	}
	if err := json.Unmarshal([]byte(items), &list.Items); err != nil {
		return todo.List{}, fmt.Errorf("store: decode session todo: %w", err)
	}
	list.UpdatedAt = mustParseTime(updatedAt)
	return list, nil
}

// SaveSessionTodo replaces the session's todo list.
func (s *SQLiteStore) SaveSessionTodo(ctx context.Context, list todo.List) error {
	items, err := json.Marshal(list.Items)
	if err != nil {
		return fmt.Errorf("store: encode session todo: %w", err)
	}
	if list.Items == nil {
		items = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO session_todos(session_id, items_json, chain_run_id, updated_run_id, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    items_json = excluded.items_json,
    chain_run_id = excluded.chain_run_id,
    updated_run_id = excluded.updated_run_id,
    updated_at = excluded.updated_at`,
		strings.TrimSpace(list.SessionID), string(items), list.ChainRunID, list.UpdatedRunID, formatTime(list.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: save session todo: %w", err)
	}
	return nil
}
