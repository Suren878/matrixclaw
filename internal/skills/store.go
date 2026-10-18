package skills

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The skills tables (skills, skill_fts, skill_sessions) are in the store's
// schema; this file holds every query on them.

func (s *Service) exists(ctx context.Context, id string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM skills WHERE id = ?`, NormalizeID(id)).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// upsertSkill writes the skill's row and its search entry in one transaction.
func (s *Service) upsertSkill(ctx context.Context, skill Skill, body string) error {
	authors, _ := json.Marshal(skill.Authors)
	tags, _ := json.Marshal(skill.Tags)
	platforms, _ := json.Marshal(skill.Platforms)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO skills (
		id, name, description, version, author, authors_json, license, tags_json, platforms_json, category,
		path, source, provenance, hash, trust_state, state, enabled, pinned, installed_at, updated_at, last_activity_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name, description=excluded.description, version=excluded.version, author=excluded.author,
		authors_json=excluded.authors_json, license=excluded.license, tags_json=excluded.tags_json,
		platforms_json=excluded.platforms_json, category=excluded.category, path=excluded.path,
		source=excluded.source, provenance=excluded.provenance, hash=excluded.hash,
		trust_state=excluded.trust_state, state=excluded.state, enabled=excluded.enabled, updated_at=excluded.updated_at`,
		skill.ID, skill.Name, skill.Description, skill.Version, skill.Author, string(authors), skill.License, string(tags), string(platforms), skill.Category,
		skill.Path, skill.Source, skill.Provenance, skill.Hash, skill.TrustState, skill.State, boolInt(skill.Enabled), boolInt(skill.Pinned), formatTime(skill.InstalledAt), formatTime(skill.UpdatedAt), formatTime(skill.LastActivityAt)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM skill_fts WHERE id = ?`, skill.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO skill_fts(id, name, description, tags, category, body) VALUES (?, ?, ?, ?, ?, ?)`,
		skill.ID, skill.Name, skill.Description, strings.Join(skill.Tags, " "), skill.Category, body); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteSkill removes the skill's row, search entry and session activations.
func (s *Service) deleteSkill(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, query := range []string{`DELETE FROM skill_fts WHERE id = ?`, `DELETE FROM skill_sessions WHERE skill_id = ?`, `DELETE FROM skills WHERE id = ?`} {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// updateSkill sets columns of one skill and stamps updated_at; it reports
// ErrNotFound for an unknown id.
func (s *Service) updateSkill(ctx context.Context, id string, set string, args ...any) error {
	args = append(args, formatTime(s.now().UTC()), id)
	result, err := s.db.ExecContext(ctx, `UPDATE skills SET `+set+`, updated_at = ? WHERE id = ?`, args...)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return err
}

// countActivity bumps a usage counter (use_count, view_count or patch_count).
func (s *Service) countActivity(ctx context.Context, id string, counter string, at time.Time) {
	_, _ = s.db.ExecContext(ctx, `UPDATE skills SET `+counter+` = `+counter+` + 1, last_activity_at = ? WHERE id = ?`, formatTime(at), id)
}

func (s *Service) activate(ctx context.Context, sessionID string, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO skill_sessions(session_id, skill_id, activated_at) VALUES (?, ?, ?)`, sessionID, id, formatTime(at))
	return err
}

func (s *Service) deactivate(ctx context.Context, sessionID string, id string) error {
	if sessionID == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM skill_sessions WHERE skill_id = ?`, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM skill_sessions WHERE session_id = ? AND skill_id = ?`, sessionID, id)
	return err
}

// Search finds skills by name, description, tags, category and body; an empty
// query lists them pinned first.
func (s *Service) Search(ctx context.Context, query string, opts SearchOptions) ([]Skill, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if !opts.IncludeQuarantined {
		where = append(where, "trust_state = ?")
		args = append(args, TrustTrusted)
	}
	if !opts.IncludeArchived {
		where = append(where, "state = ?")
		args = append(args, StateActive)
	}
	if !opts.IncludeDisabled {
		where = append(where, "enabled = 1")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		args = append(args, limit)
		return s.querySkills(ctx, `SELECT `+skillColumns("")+` FROM skills WHERE `+strings.Join(where, " AND ")+` ORDER BY pinned DESC, name LIMIT ?`, args...)
	}
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	args = append([]any{match}, args...)
	args = append(args, limit)
	return s.querySkills(ctx, `SELECT `+skillColumns("s")+` FROM skill_fts f JOIN skills s ON s.id = f.id WHERE skill_fts MATCH ? AND `+strings.Join(where, " AND ")+` ORDER BY rank, s.pinned DESC, s.name LIMIT ?`, args...)
}

// Usage lists every skill, most used first.
func (s *Service) Usage(ctx context.Context) ([]Skill, error) {
	return s.querySkills(ctx, `SELECT `+skillColumns("")+` FROM skills ORDER BY use_count DESC, view_count DESC, name`)
}

// SessionSkills lists the trusted, active, enabled skills the session activated.
func (s *Service) SessionSkills(ctx context.Context, sessionID string) ([]Skill, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	return s.querySkills(ctx, `SELECT `+skillColumns("s")+` FROM skill_sessions ss JOIN skills s ON s.id = ss.skill_id WHERE ss.session_id = ? AND s.trust_state = ? AND s.state = ? AND s.enabled = 1 ORDER BY ss.activated_at`, sessionID, TrustTrusted, StateActive)
}

func (s *Service) getSkill(ctx context.Context, id string) (Skill, error) {
	skills, err := s.querySkills(ctx, `SELECT `+skillColumns("")+` FROM skills WHERE id = ?`, id)
	if err != nil {
		return Skill{}, err
	}
	if len(skills) == 0 {
		return Skill{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return skills[0], nil
}

func (s *Service) querySkills(ctx context.Context, query string, args ...any) ([]Skill, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var skills []Skill
	for rows.Next() {
		skill, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, rows.Err()
}

func scanSkill(rows *sql.Rows) (Skill, error) {
	var skill Skill
	var authorsJSON, tagsJSON, platformsJSON string
	var enabled, pinned int
	var installed, updated, activity string
	err := rows.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Version, &skill.Author, &authorsJSON, &skill.License, &tagsJSON, &platformsJSON, &skill.Category, &skill.Path, &skill.Source, &skill.Provenance, &skill.Hash, &skill.TrustState, &skill.State, &enabled, &pinned, &skill.UseCount, &skill.ViewCount, &skill.PatchCount, &installed, &updated, &activity)
	if err != nil {
		return Skill{}, err
	}
	_ = json.Unmarshal([]byte(authorsJSON), &skill.Authors)
	_ = json.Unmarshal([]byte(tagsJSON), &skill.Tags)
	_ = json.Unmarshal([]byte(platformsJSON), &skill.Platforms)
	skill.Enabled = enabled != 0
	skill.Pinned = pinned != 0
	skill.InstalledAt = parseTime(installed)
	skill.UpdatedAt = parseTime(updated)
	skill.LastActivityAt = parseTime(activity)
	return skill, nil
}

func skillColumns(prefix string) string {
	columns := []string{
		"id", "name", "description", "version", "author", "authors_json", "license", "tags_json", "platforms_json", "category",
		"path", "source", "provenance", "hash", "trust_state", "state", "enabled", "pinned",
		"use_count", "view_count", "patch_count", "installed_at", "updated_at", "last_activity_at",
	}
	if prefix == "" {
		return strings.Join(columns, ", ")
	}
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, prefix+"."+column)
	}
	return strings.Join(out, ", ")
}

func ftsQuery(query string) string {
	parts := strings.Fields(strings.ToLower(query))
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = cleanFTSTerm(part); part != "" {
			cleaned = append(cleaned, `"`+part+`"*`)
		}
	}
	sort.Strings(cleaned)
	return strings.Join(cleaned, " ")
}

func cleanFTSTerm(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return t
}
