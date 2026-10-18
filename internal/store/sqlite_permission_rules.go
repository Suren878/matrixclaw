package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

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

// GetPermissionRule returns a rule; ErrNotFound when no rule has the ID.
func (s *SQLiteStore) GetPermissionRule(ctx context.Context, ruleID string) (permission.Rule, error) {
	rule, err := scanPermissionRule(s.db.QueryRowContext(ctx, `SELECT `+permissionRuleColumns+` FROM permission_rules WHERE id = ?`, ruleID))
	if errors.Is(err, sql.ErrNoRows) {
		return permission.Rule{}, core.ErrNotFound
	}
	if err != nil {
		return permission.Rule{}, fmt.Errorf("store: get permission rule: %w", err)
	}
	return rule, nil
}

// ListPermissionRules returns the global rules and the rules of the given
// sessions, oldest first.
func (s *SQLiteStore) ListPermissionRules(ctx context.Context, sessionIDs []string) ([]permission.Rule, error) {
	query := `SELECT ` + permissionRuleColumns + ` FROM permission_rules WHERE session_id IS NULL`
	args := make([]any, 0, len(sessionIDs))
	if len(sessionIDs) > 0 {
		query += ` OR session_id IN (?` + strings.Repeat(`, ?`, len(sessionIDs)-1) + `)`
		for _, id := range sessionIDs {
			args = append(args, id)
		}
	}
	query += ` ORDER BY created_at, id`
	return queryAll(ctx, s.db, "permission rules", scanPermissionRule, query, args...)
}

const permissionRuleColumns = `id, tool, pattern, effect, scope, COALESCE(session_id, ''), created_at`

func scanPermissionRule(row rowScanner) (permission.Rule, error) {
	var rule permission.Rule
	var effect, scope, createdAt string
	if err := row.Scan(&rule.ID, &rule.Tool, &rule.Pattern, &effect, &scope, &rule.SessionID, &createdAt); err != nil {
		return permission.Rule{}, err
	}
	rule.Effect = permission.Effect(effect)
	rule.Scope = permission.Scope(scope)
	rule.CreatedAt = mustParseTime(createdAt)
	return rule, nil
}
