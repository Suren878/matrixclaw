package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) CreateMemory(ctx context.Context, entry core.MemoryEntry) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO memories(id, scope, key, content, working_dir, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		string(entry.Scope),
		entry.Key,
		entry.Content,
		entry.WorkingDir,
		formatTime(entry.CreatedAt),
		formatTime(entry.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create memory: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateMemory(ctx context.Context, entry core.MemoryEntry) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE memories
SET scope = ?, key = ?, content = ?, working_dir = ?, updated_at = ?
WHERE id = ?`,
		string(entry.Scope),
		entry.Key,
		entry.Content,
		entry.WorkingDir,
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
	result, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, strings.TrimSpace(id))
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

func (s *SQLiteStore) GetMemory(ctx context.Context, id string) (core.MemoryEntry, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, scope, key, content, working_dir, created_at, updated_at
FROM memories
WHERE id = ?`, strings.TrimSpace(id))
	entry, err := scanMemory(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.MemoryEntry{}, core.ErrNotFound
		}
		return core.MemoryEntry{}, err
	}
	return entry, nil
}

func (s *SQLiteStore) ListMemories(ctx context.Context, filter core.MemoryFilter) ([]core.MemoryEntry, error) {
	query := `
SELECT id, scope, key, content, working_dir, created_at, updated_at
FROM memories`
	args := []any{}
	clauses := []string{}
	if strings.TrimSpace(string(filter.Scope)) != "" {
		clauses = append(clauses, "scope = ?")
		args = append(args, strings.TrimSpace(string(filter.Scope)))
	}
	if strings.TrimSpace(filter.WorkingDir) != "" {
		clauses = append(clauses, "(working_dir = '' OR working_dir = ?)")
		args = append(args, strings.TrimSpace(filter.WorkingDir))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY updated_at DESC, created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list memories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	entries := []core.MemoryEntry{}
	for rows.Next() {
		entry, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate memories: %w", err)
	}
	return entries, nil
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemory(scanner memoryScanner) (core.MemoryEntry, error) {
	var entry core.MemoryEntry
	var scope string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&entry.ID, &scope, &entry.Key, &entry.Content, &entry.WorkingDir, &createdAt, &updatedAt); err != nil {
		return core.MemoryEntry{}, fmt.Errorf("store: scan memory: %w", err)
	}
	entry.Scope = core.MemoryScope(scope)
	entry.CreatedAt = mustParseTime(createdAt)
	entry.UpdatedAt = mustParseTime(updatedAt)
	return entry, nil
}
