package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

// Wake times are kept in Unix milliseconds so they compare as numbers.

func (s *SQLiteStore) SaveRunWakeup(ctx context.Context, wakeup core.RunWakeup) error {
	taskIDs := ""
	if len(wakeup.TaskIDs) > 0 {
		encoded, err := json.Marshal(wakeup.TaskIDs)
		if err != nil {
			return err
		}
		taskIDs = string(encoded)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO run_wakeups(run_id, session_id, wake_at, task_ids_json) VALUES(?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET session_id = excluded.session_id, wake_at = excluded.wake_at, task_ids_json = excluded.task_ids_json`,
		strings.TrimSpace(wakeup.RunID), strings.TrimSpace(wakeup.SessionID), wakeup.WakeAt.UnixMilli(), taskIDs); err != nil {
		return fmt.Errorf("store: save run wakeup: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRunWakeup(ctx context.Context, runID string) (core.RunWakeup, error) {
	wakeup, err := scanRunWakeup(s.db.QueryRowContext(ctx, `SELECT run_id, session_id, wake_at, task_ids_json FROM run_wakeups WHERE run_id = ?`, strings.TrimSpace(runID)))
	if errors.Is(err, sql.ErrNoRows) {
		return core.RunWakeup{}, core.ErrNotFound
	}
	return wakeup, err
}

func (s *SQLiteStore) DeleteRunWakeup(ctx context.Context, runID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM run_wakeups WHERE run_id = ?`, strings.TrimSpace(runID)); err != nil {
		return fmt.Errorf("store: delete run wakeup: %w", err)
	}
	return nil
}

// ListDueRunWakeups lists the wakeups due at at, earliest first.
func (s *SQLiteStore) ListDueRunWakeups(ctx context.Context, at time.Time) ([]core.RunWakeup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, session_id, wake_at, task_ids_json FROM run_wakeups WHERE wake_at <= ? ORDER BY wake_at ASC`, at.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("store: list due run wakeups: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var wakeups []core.RunWakeup
	for rows.Next() {
		wakeup, err := scanRunWakeup(rows)
		if err != nil {
			return nil, err
		}
		wakeups = append(wakeups, wakeup)
	}
	return wakeups, rows.Err()
}

type runWakeupScanner interface {
	Scan(dest ...any) error
}

func scanRunWakeup(scanner runWakeupScanner) (core.RunWakeup, error) {
	var wakeup core.RunWakeup
	var wakeAt int64
	var taskIDs string
	if err := scanner.Scan(&wakeup.RunID, &wakeup.SessionID, &wakeAt, &taskIDs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.RunWakeup{}, err
		}
		return core.RunWakeup{}, fmt.Errorf("store: scan run wakeup: %w", err)
	}
	wakeup.WakeAt = time.UnixMilli(wakeAt).UTC()
	if taskIDs != "" {
		if err := json.Unmarshal([]byte(taskIDs), &wakeup.TaskIDs); err != nil {
			return core.RunWakeup{}, fmt.Errorf("store: decode run wakeup tasks: %w", err)
		}
	}
	return wakeup, nil
}
