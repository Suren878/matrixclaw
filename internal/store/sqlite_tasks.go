package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MarkTasksDelivered records that runID told the tasks' sessions they finished;
// an empty runID means nothing is to be told.
func (s *SQLiteStore) MarkTasksDelivered(ctx context.Context, taskIDs []string, runID string, at time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	args := []any{formatTime(at), strings.TrimSpace(runID), formatTime(at)}
	placeholders := make([]string, 0, len(taskIDs))
	for _, id := range taskIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE tasks SET delivered_at = ?, delivered_run_id = ?, updated_at = ?
WHERE delivered_at IS NULL AND id IN (`+strings.Join(placeholders, ", ")+`)`, args...); err != nil {
		return fmt.Errorf("store: mark tasks delivered: %w", err)
	}
	return nil
}
