package store

import (
	"context"
	"database/sql"
	"fmt"
)

// rowScanner is a *sql.Row or *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// sqlExecer is satisfied by *sql.DB and *sql.Tx.
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// sqlQueryer is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type sqlQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryAll runs query and scans every row into a non-nil slice; what names
// the rows in errors.
func queryAll[T any](ctx context.Context, db sqlQueryer, what string, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	out := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan %s: %w", what, err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate %s: %w", what, err)
	}
	return out, nil
}
