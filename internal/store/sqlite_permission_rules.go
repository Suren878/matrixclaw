package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func migratePermissionRules(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS permission_rules (
    id TEXT PRIMARY KEY,
    tool TEXT NOT NULL,
    pattern TEXT NOT NULL DEFAULT '',
    effect TEXT NOT NULL,
    scope TEXT NOT NULL,
    session_id TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create permission rules table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_permission_rules_session ON permission_rules(session_id)`); err != nil {
		return fmt.Errorf("store: create permission rules session index: %w", err)
	}
	return nil
}

// CreatePermissionRule stores a rule; a global rule belongs to no session.
func (s *SQLiteStore) CreatePermissionRule(ctx context.Context, rule permission.Rule) error {
	var sessionID any
	if rule.Scope == permission.ScopeSession {
		sessionID = rule.SessionID
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO permission_rules(id, tool, pattern, effect, scope, session_id, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.Tool, rule.Pattern, string(rule.Effect), string(rule.Scope), sessionID, formatTime(rule.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create permission rule: %w", err)
	}
	return nil
}

// DeletePermissionRule removes a rule; ErrNotFound when no rule has the ID.
func (s *SQLiteStore) DeletePermissionRule(ctx context.Context, ruleID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM permission_rules WHERE id = ?`, ruleID)
	if err != nil {
		return fmt.Errorf("store: delete permission rule: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete permission rule rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

// ListPermissionRules returns the global rules and the rules of the given
// sessions, oldest first.
func (s *SQLiteStore) ListPermissionRules(ctx context.Context, sessionIDs []string) ([]permission.Rule, error) {
	query := `
SELECT id, tool, pattern, effect, scope, COALESCE(session_id, ''), created_at
FROM permission_rules
WHERE session_id IS NULL`
	args := make([]any, 0, len(sessionIDs))
	if len(sessionIDs) > 0 {
		query += ` OR session_id IN (?` + strings.Repeat(`, ?`, len(sessionIDs)-1) + `)`
		for _, id := range sessionIDs {
			args = append(args, id)
		}
	}
	query += ` ORDER BY created_at, id`
	return queryAll(ctx, s.db, "permission rules", func(row rowScanner) (permission.Rule, error) {
		var rule permission.Rule
		var effect, scope, createdAt string
		if err := row.Scan(&rule.ID, &rule.Tool, &rule.Pattern, &effect, &scope, &rule.SessionID, &createdAt); err != nil {
			return permission.Rule{}, err
		}
		rule.Effect = permission.Effect(effect)
		rule.Scope = permission.Scope(scope)
		rule.CreatedAt = mustParseTime(createdAt)
		return rule, nil
	}, query, args...)
}
