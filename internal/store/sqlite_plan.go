package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *SQLiteStore) GetSessionPlan(ctx context.Context, sessionID string) (core.SessionPlan, error) {
	sessionID = strings.TrimSpace(sessionID)
	plan := core.SessionPlan{SessionID: sessionID}
	var goal string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
SELECT goal, updated_at
FROM session_goals
WHERE session_id = ?`, sessionID).Scan(&goal, &updatedAt)
	if err != nil && err != sql.ErrNoRows {
		return core.SessionPlan{}, fmt.Errorf("store: get session goal: %w", err)
	}
	if err == nil {
		plan.Goal = goal
		plan.UpdatedAt = mustParseTime(updatedAt)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, parent_id, text, status, position, created_at, updated_at
FROM session_plan_items
WHERE session_id = ?
ORDER BY parent_id, position, created_at`, sessionID)
	if err != nil {
		return core.SessionPlan{}, fmt.Errorf("store: list plan items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item, err := scanPlanItem(rows)
		if err != nil {
			return core.SessionPlan{}, err
		}
		plan.Items = append(plan.Items, item)
		if item.UpdatedAt.After(plan.UpdatedAt) {
			plan.UpdatedAt = item.UpdatedAt
		}
	}
	if err := rows.Err(); err != nil {
		return core.SessionPlan{}, fmt.Errorf("store: iterate plan items: %w", err)
	}
	plan.Items = orderPlanItems(plan.Items)
	return plan, nil
}

func (s *SQLiteStore) SetSessionGoal(ctx context.Context, sessionID string, goal string, updatedAt time.Time) error {
	if strings.TrimSpace(sessionID) == "" {
		return core.ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_goals(session_id, goal, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    goal = excluded.goal,
    updated_at = excluded.updated_at`,
		sessionID,
		strings.TrimSpace(goal),
		formatTime(updatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: set session goal: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ClearSessionPlan(ctx context.Context, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin clear session plan: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_plan_items WHERE session_id = ?`, sessionID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: clear session plan items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_goals WHERE session_id = ?`, sessionID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: clear session goal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit clear session plan: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AddPlanItem(ctx context.Context, item core.PlanItem) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.SessionID) == "" || strings.TrimSpace(item.Text) == "" {
		return core.ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_plan_items(id, session_id, parent_id, text, status, position, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		item.SessionID,
		strings.TrimSpace(item.ParentID),
		item.Text,
		string(item.Status),
		item.Position,
		formatTime(item.CreatedAt),
		formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: add plan item: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdatePlanItem(ctx context.Context, item core.PlanItem) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE session_plan_items
SET parent_id = ?, text = ?, status = ?, position = ?, updated_at = ?
WHERE id = ?`,
		strings.TrimSpace(item.ParentID),
		item.Text,
		string(item.Status),
		item.Position,
		formatTime(item.UpdatedAt),
		item.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update plan item: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update plan item rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetPlanItem(ctx context.Context, itemID string) (core.PlanItem, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, parent_id, text, status, position, created_at, updated_at
FROM session_plan_items
WHERE id = ?`, itemID)
	item, err := scanPlanItem(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return core.PlanItem{}, core.ErrNotFound
		}
		return core.PlanItem{}, err
	}
	return item, nil
}

func (s *SQLiteStore) NextPlanItemPosition(ctx context.Context, sessionID string, parentID string) (int, error) {
	var position sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
SELECT MAX(position)
FROM session_plan_items
WHERE session_id = ? AND parent_id = ?`, sessionID, strings.TrimSpace(parentID)).Scan(&position); err != nil {
		return 0, fmt.Errorf("store: next plan item position: %w", err)
	}
	if !position.Valid {
		return 1, nil
	}
	return int(position.Int64) + 1, nil
}

func (s *SQLiteStore) GetPlanRun(ctx context.Context, sessionID string) (core.PlanRun, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT session_id, status, current_item_id, last_run_id, last_error, step_no, attempt, created_at, updated_at
FROM plan_runs
WHERE session_id = ?`, strings.TrimSpace(sessionID))
	run, err := scanPlanRun(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return core.PlanRun{SessionID: strings.TrimSpace(sessionID), Status: core.PlanRunIdle}, nil
		}
		return core.PlanRun{}, fmt.Errorf("store: get plan run: %w", err)
	}
	return run, nil
}

func (s *SQLiteStore) SavePlanRun(ctx context.Context, run core.PlanRun) error {
	if strings.TrimSpace(run.SessionID) == "" {
		return core.ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO plan_runs(session_id, status, current_item_id, last_run_id, last_error, step_no, attempt, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    status = excluded.status,
    current_item_id = excluded.current_item_id,
    last_run_id = excluded.last_run_id,
    last_error = excluded.last_error,
    step_no = excluded.step_no,
    attempt = excluded.attempt,
    updated_at = excluded.updated_at`,
		strings.TrimSpace(run.SessionID),
		string(run.Status),
		strings.TrimSpace(run.CurrentItemID),
		strings.TrimSpace(run.LastRunID),
		strings.TrimSpace(run.LastError),
		run.StepNo,
		run.Attempt,
		formatTime(run.CreatedAt),
		formatTime(run.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save plan run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ClearPlanRun(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM plan_runs WHERE session_id = ?`, strings.TrimSpace(sessionID)); err != nil {
		return fmt.Errorf("store: clear plan run: %w", err)
	}
	return nil
}

func scanPlanRun(scanner interface{ Scan(dest ...any) error }) (core.PlanRun, error) {
	var run core.PlanRun
	var status string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&run.SessionID, &status, &run.CurrentItemID, &run.LastRunID, &run.LastError, &run.StepNo, &run.Attempt, &createdAt, &updatedAt); err != nil {
		return core.PlanRun{}, err
	}
	run.Status = core.PlanRunStatus(status)
	run.CreatedAt = mustParseTime(createdAt)
	run.UpdatedAt = mustParseTime(updatedAt)
	return run, nil
}

func scanPlanItem(scanner interface{ Scan(dest ...any) error }) (core.PlanItem, error) {
	var item core.PlanItem
	var status string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.ID, &item.SessionID, &item.ParentID, &item.Text, &status, &item.Position, &createdAt, &updatedAt); err != nil {
		return core.PlanItem{}, err
	}
	item.Status = core.PlanItemStatus(status)
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return item, nil
}

func orderPlanItems(items []core.PlanItem) []core.PlanItem {
	if len(items) < 2 {
		return items
	}
	children := make(map[string][]core.PlanItem, len(items))
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		ids[item.ID] = struct{}{}
	}
	for _, item := range items {
		parentID := strings.TrimSpace(item.ParentID)
		if _, ok := ids[parentID]; parentID != "" && !ok {
			parentID = ""
			item.ParentID = ""
		}
		children[parentID] = append(children[parentID], item)
	}
	ordered := make([]core.PlanItem, 0, len(items))
	var walk func(parentID string)
	walk = func(parentID string) {
		for _, item := range children[parentID] {
			ordered = append(ordered, item)
			walk(item.ID)
		}
	}
	walk("")
	if len(ordered) != len(items) {
		seen := make(map[string]struct{}, len(ordered))
		for _, item := range ordered {
			seen[item.ID] = struct{}{}
		}
		for _, item := range items {
			if _, ok := seen[item.ID]; !ok {
				ordered = append(ordered, item)
			}
		}
	}
	return ordered
}
