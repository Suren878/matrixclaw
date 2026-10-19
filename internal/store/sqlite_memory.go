package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Suren878/matrixclaw/internal/memory"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) CreateMemory(ctx context.Context, entry memory.Entry) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memories(id, scope, key, content, working_dir, pending, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		string(entry.Scope),
		entry.Key,
		entry.Content,
		entry.WorkingDir,
		boolInt(entry.Pending),
		formatTime(entry.CreatedAt),
		formatTime(entry.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create memory: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateMemory(ctx context.Context, entry memory.Entry) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE memories
SET scope = ?, key = ?, content = ?, working_dir = ?, pending = ?, updated_at = ?
WHERE id = ?`,
		string(entry.Scope),
		entry.Key,
		entry.Content,
		entry.WorkingDir,
		boolInt(entry.Pending),
		formatTime(entry.UpdatedAt),
		entry.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update memory: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: update memory rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteMemory(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete memory: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: delete memory rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetMemory(ctx context.Context, id string) (memory.Entry, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, scope, key, content, working_dir, pending, created_at, updated_at
FROM memories
WHERE id = ?`, id)
	entry, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return memory.Entry{}, core.ErrNotFound
		}
		return memory.Entry{}, fmt.Errorf("store: get memory: %w", err)
	}
	return entry, nil
}

func (s *SQLiteStore) ListMemories(ctx context.Context, filter memory.Filter) ([]memory.Entry, error) {
	query := `
SELECT id, scope, key, content, working_dir, pending, created_at, updated_at
FROM memories`
	args := []any{boolInt(filter.Pending)}
	clauses := []string{"pending = ?"}
	if filter.Scope != "" {
		clauses = append(clauses, "scope = ?")
		args = append(args, string(filter.Scope))
	}
	if filter.WorkingDir != "" {
		clauses = append(clauses, "(working_dir = '' OR working_dir = ?)")
		args = append(args, filter.WorkingDir)
	}
	query += " WHERE " + strings.Join(clauses, " AND ")
	query += " ORDER BY updated_at DESC, created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	return queryAll(ctx, s.db, "memories", scanMemory, query, args...)
}

func scanMemory(scanner rowScanner) (memory.Entry, error) {
	var entry memory.Entry
	var scope string
	var createdAt string
	var updatedAt string
	var pending int
	if err := scanner.Scan(&entry.ID, &scope, &entry.Key, &entry.Content, &entry.WorkingDir, &pending, &createdAt, &updatedAt); err != nil {
		return memory.Entry{}, err
	}
	entry.Pending = pending != 0
	entry.Scope = memory.Scope(scope)
	entry.CreatedAt = mustParseTime(createdAt)
	entry.UpdatedAt = mustParseTime(updatedAt)
	return entry, nil
}
