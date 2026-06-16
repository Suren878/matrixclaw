package skills

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type Service struct {
	db      *sql.DB
	root    string
	cfg     Config
	now     func() time.Time
	closer  bool
	enabled bool
}

func NewService(cfg Config) (*Service, error) {
	cfg = normalizeConfig(cfg)
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("skills db path is required")
	}
	if err := ensureSkillDirs(cfg.Root); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Clean(cfg.DBPath))
	if err != nil {
		return nil, err
	}
	// Skill metadata is local daemon state. A single SQLite connection keeps
	// writes serialized and matches the rest of MatrixClaw's personal-store model.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	service := &Service{db: db, root: cfg.Root, cfg: cfg, now: time.Now, closer: true, enabled: cfg.Enabled}
	if err := service.bootstrap(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return service, nil
}

func normalizeConfig(cfg Config) Config {
	cfg.DBPath = strings.TrimSpace(cfg.DBPath)
	cfg.Root = strings.TrimSpace(cfg.Root)
	if cfg.Root == "" && cfg.DBPath != "" {
		cfg.Root = filepath.Join(filepath.Dir(filepath.Clean(cfg.DBPath)), "skills")
	}
	cfg.Root = filepath.Clean(cfg.Root)
	if cfg.TrustPolicy == "" {
		cfg.TrustPolicy = DefaultTrustPolicy
	}
	if cfg.SelfImprove == "" {
		cfg.SelfImprove = DefaultSelfImprove
	}
	if !cfg.Enabled {
		cfg.Enabled = true
	}
	cfg.AutoInvoke = true
	return cfg
}

func ensureSkillDirs(root string) error {
	for _, dir := range []string{"installed", "quarantine", "archive", "plugins", "drafts"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Close() error {
	if s == nil || s.db == nil || !s.closer {
		return nil
	}
	return s.db.Close()
}

func (s *Service) bootstrap() error {
	stmts := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
		`CREATE TABLE IF NOT EXISTS skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			author TEXT NOT NULL DEFAULT '',
			authors_json TEXT NOT NULL DEFAULT '[]',
			license TEXT NOT NULL DEFAULT '',
			tags_json TEXT NOT NULL DEFAULT '[]',
			platforms_json TEXT NOT NULL DEFAULT '[]',
			category TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			provenance TEXT NOT NULL DEFAULT '',
			hash TEXT NOT NULL DEFAULT '',
			trust_state TEXT NOT NULL,
			state TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			pinned INTEGER NOT NULL DEFAULT 0,
			use_count INTEGER NOT NULL DEFAULT 0,
			view_count INTEGER NOT NULL DEFAULT 0,
			patch_count INTEGER NOT NULL DEFAULT 0,
			installed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_activity_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS skill_fts USING fts5(id UNINDEXED, name, description, tags, category, body)`,
		`CREATE TABLE IF NOT EXISTS skill_sessions (
			session_id TEXT NOT NULL,
			skill_id TEXT NOT NULL,
			activated_at TEXT NOT NULL,
			PRIMARY KEY(session_id, skill_id)
		)`,
		`CREATE TABLE IF NOT EXISTS skill_plugin_mcp_candidates (
			id TEXT PRIMARY KEY,
			plugin_path TEXT NOT NULL,
			config_json TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := s.ensureSkillSchema(); err != nil {
		return err
	}
	return nil
}

func (s *Service) ensureSkillSchema() error {
	hasEnabled := false
	rows, err := s.db.Query(`PRAGMA table_info(skills)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "enabled" {
			hasEnabled = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasEnabled {
		_, err := s.db.Exec(`ALTER TABLE skills ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`)
		return err
	}
	return nil
}

func (s *Service) InstallPath(path string, opts InstallOptions) ([]Skill, error) {
	path = strings.TrimSpace(path)
	if isGitHubURL(path) {
		return s.installGitHubURL(path, opts)
	}
	path = filepath.Clean(path)
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
		skill, err := s.InstallLocal(path, opts)
		if err != nil {
			return nil, err
		}
		return []Skill{skill}, nil
	}
	for _, manifest := range []string{filepath.Join(path, ".codex-plugin", "plugin.json"), filepath.Join(path, ".claude-plugin", "plugin.json")} {
		if _, err := os.Stat(manifest); err == nil {
			return s.installPlugin(path, manifest, opts)
		}
	}
	return s.installHermesTree(path, opts)
}

func isGitHubURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && strings.EqualFold(parsed.Host, "github.com")
}

func (s *Service) installGitHubURL(rawURL string, opts InstallOptions) ([]Skill, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("github URL must include owner and repository")
	}
	owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
	repoURL := "https://github.com/" + owner + "/" + repo + ".git"
	ref := ""
	subpath := ""
	if len(parts) >= 4 && parts[2] == "tree" {
		ref = parts[3]
		if len(parts) > 4 {
			subpath = filepath.Join(parts[4:]...)
		}
	}
	tmp, err := os.MkdirTemp(filepath.Join(s.root, "quarantine"), "github-*")
	if err != nil {
		return nil, err
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, repoURL, tmp)
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("clone github skill source: %w: %s", err, strings.TrimSpace(string(output)))
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	importRoot := tmp
	if subpath != "" {
		importRoot = filepath.Join(tmp, subpath)
	}
	if opts.Provenance == "" {
		opts.Provenance = rawURL
	}
	if opts.Source == "" {
		opts.Source = "github"
	}
	return s.InstallPath(importRoot, opts)
}

func (s *Service) InstallLocal(source string, opts InstallOptions) (Skill, error) {
	if s == nil {
		return Skill{}, fmt.Errorf("skills service is not configured")
	}
	source = filepath.Clean(strings.TrimSpace(source))
	if err := ValidateSkillBundle(source); err != nil {
		return Skill{}, err
	}
	doc, err := ParseSkillFile(filepath.Join(source, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}
	hash, err := hashFiles(source)
	if err != nil {
		return Skill{}, err
	}
	trust := normalizeTrust(firstNonEmpty(opts.TrustState, s.initialTrustState()))
	id := NormalizeID(doc.Name)
	if exists, _ := s.Exists(id); exists {
		id = id + "-" + hash[:8]
	}
	bucket := trustBucket(trust)
	target := filepath.Join(s.root, bucket, id)
	if err := os.RemoveAll(target); err != nil {
		return Skill{}, err
	}
	if err := copyDir(source, target); err != nil {
		return Skill{}, err
	}
	now := s.now().UTC()
	skill := Skill{
		ID:          id,
		Name:        doc.Name,
		Description: doc.Description,
		Version:     doc.Version,
		Author:      doc.Author,
		Authors:     doc.Authors,
		License:     doc.License,
		Tags:        doc.Tags,
		Platforms:   doc.Platforms,
		Category:    doc.Category,
		Path:        target,
		Source:      firstNonEmpty(opts.Source, "local"),
		Provenance:  opts.Provenance,
		Hash:        hash,
		TrustState:  trust,
		State:       StateActive,
		Enabled:     trust == TrustTrusted,
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if err := s.upsertSkill(skill, doc.Body); err != nil {
		return Skill{}, err
	}
	return skill, nil
}

func (s *Service) initialTrustState() string {
	if strings.EqualFold(s.cfg.TrustPolicy, TrustTrusted) {
		return TrustTrusted
	}
	return TrustQuarantine
}

func normalizeTrust(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case TrustTrusted:
		return TrustTrusted
	case TrustDisabled:
		return TrustDisabled
	default:
		return TrustQuarantine
	}
}

func trustBucket(trust string) string {
	if trust == TrustTrusted {
		return "installed"
	}
	return "quarantine"
}

func (s *Service) Exists(id string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM skills WHERE id = ?`, NormalizeID(id)).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) upsertSkill(skill Skill, body string) error {
	authors, _ := json.Marshal(skill.Authors)
	tags, _ := json.Marshal(skill.Tags)
	platforms, _ := json.Marshal(skill.Platforms)
	_, err := s.db.Exec(`INSERT INTO skills (
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
		skill.Path, skill.Source, skill.Provenance, skill.Hash, skill.TrustState, skill.State, boolInt(skill.Enabled), boolInt(skill.Pinned), formatTime(skill.InstalledAt), formatTime(skill.UpdatedAt), formatTime(skill.LastActivityAt))
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM skill_fts WHERE id = ?`, skill.ID)
	_, err = s.db.Exec(`INSERT INTO skill_fts(id, name, description, tags, category, body) VALUES (?, ?, ?, ?, ?, ?)`,
		skill.ID, skill.Name, skill.Description, strings.Join(skill.Tags, " "), skill.Category, body)
	return err
}

func (s *Service) Search(query string, opts SearchOptions) ([]Skill, error) {
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
		return s.querySkills(`SELECT `+skillColumns("")+` FROM skills WHERE `+strings.Join(where, " AND ")+` ORDER BY pinned DESC, name LIMIT ?`, args...)
	}
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	args = append([]any{match}, args...)
	args = append(args, limit)
	return s.querySkills(`SELECT `+skillColumns("s")+` FROM skill_fts f JOIN skills s ON s.id = f.id WHERE skill_fts MATCH ? AND `+strings.Join(where, " AND ")+` ORDER BY rank, s.pinned DESC, s.name LIMIT ?`, args...)
}

func (s *Service) List(opts SearchOptions) ([]Skill, error) {
	return s.Search("", opts)
}

func (s *Service) Get(id string) (SkillDetail, error) {
	skill, err := s.getSkill(NormalizeID(id))
	if err != nil {
		return SkillDetail{}, err
	}
	doc, err := ParseSkillFile(filepath.Join(skill.Path, "SKILL.md"))
	if err != nil {
		return SkillDetail{}, err
	}
	return SkillDetail{Skill: skill, Body: doc.Body}, nil
}

func (s *Service) View(id string) (SkillDetail, error) {
	detail, err := s.Get(id)
	if err != nil {
		return SkillDetail{}, err
	}
	now := s.now().UTC()
	_, _ = s.db.Exec(`UPDATE skills SET view_count = view_count + 1, last_activity_at = ? WHERE id = ?`, formatTime(now), detail.Skill.ID)
	detail.Skill.ViewCount++
	detail.Skill.LastActivityAt = now
	return detail, nil
}

func (s *Service) Use(sessionID string, id string) (SkillDetail, error) {
	detail, err := s.Get(id)
	if err != nil {
		return SkillDetail{}, err
	}
	if detail.Skill.TrustState != TrustTrusted || detail.Skill.State != StateActive || !detail.Skill.Enabled {
		return SkillDetail{}, fmt.Errorf("skill %s is not trusted and active", detail.Skill.ID)
	}
	now := s.now().UTC()
	_, _ = s.db.Exec(`UPDATE skills SET use_count = use_count + 1, last_activity_at = ? WHERE id = ?`, formatTime(now), detail.Skill.ID)
	if strings.TrimSpace(sessionID) != "" {
		_, _ = s.db.Exec(`INSERT OR REPLACE INTO skill_sessions(session_id, skill_id, activated_at) VALUES (?, ?, ?)`, strings.TrimSpace(sessionID), detail.Skill.ID, formatTime(now))
	}
	detail.Skill.UseCount++
	detail.Skill.LastActivityAt = now
	return detail, nil
}

func (s *Service) Trust(id string) error {
	if err := s.setTrustState(id, TrustTrusted); err != nil {
		return err
	}
	return s.SetEnabled(id, true)
}

func (s *Service) Quarantine(id string) error {
	if err := s.setTrustState(id, TrustQuarantine); err != nil {
		return err
	}
	return s.SetEnabled(id, false)
}

func (s *Service) Disable(id string) error {
	return s.SetEnabled(id, false)
}

func (s *Service) setTrustState(id string, trust string) error {
	skill, err := s.getSkill(NormalizeID(id))
	if err != nil {
		return err
	}
	target := filepath.Join(s.root, trustBucket(trust), skill.ID)
	if filepath.Clean(skill.Path) != filepath.Clean(target) {
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.Rename(skill.Path, target); err != nil && !os.IsNotExist(err) {
			return err
		}
		skill.Path = target
	}
	_, err = s.db.Exec(`UPDATE skills SET trust_state = ?, path = ?, updated_at = ? WHERE id = ?`, trust, skill.Path, formatTime(s.now().UTC()), skill.ID)
	return err
}

func (s *Service) Archive(id string) error {
	return s.setState(id, StateArchived)
}

func (s *Service) Restore(id string) error {
	return s.setState(id, StateActive)
}

func (s *Service) setState(id string, state string) error {
	_, err := s.db.Exec(`UPDATE skills SET state = ?, updated_at = ? WHERE id = ?`, state, formatTime(s.now().UTC()), NormalizeID(id))
	return err
}

func (s *Service) Pin(id string, pinned bool) error {
	_, err := s.db.Exec(`UPDATE skills SET pinned = ?, updated_at = ? WHERE id = ?`, boolInt(pinned), formatTime(s.now().UTC()), NormalizeID(id))
	return err
}

func (s *Service) SetEnabled(id string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE skills SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), formatTime(s.now().UTC()), NormalizeID(id))
	if !enabled {
		_, _ = s.db.Exec(`DELETE FROM skill_sessions WHERE skill_id = ?`, NormalizeID(id))
	}
	return err
}

func (s *Service) SessionSkills(sessionID string) ([]Skill, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	return s.querySkills(`SELECT `+skillColumns("s")+` FROM skill_sessions ss JOIN skills s ON s.id = ss.skill_id WHERE ss.session_id = ? AND s.trust_state = ? AND s.state = ? AND s.enabled = 1 ORDER BY ss.activated_at`, sessionID, TrustTrusted, StateActive)
}

func (s *Service) Unload(sessionID string, id string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM skill_sessions WHERE session_id = ? AND skill_id = ?`, sessionID, NormalizeID(id))
	return err
}

func (s *Service) CreateDraft(name string, description string, tags []string, body string) (Skill, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	body = strings.TrimSpace(body)
	if name == "" || description == "" {
		return Skill{}, fmt.Errorf("skill draft requires name and description")
	}
	if body == "" {
		body = "Describe when to use this skill and the steps to follow."
	}
	cleanTags := cleanStringSlice(tags)
	id := NormalizeID(name)
	if id == "" {
		return Skill{}, fmt.Errorf("skill draft requires a valid name")
	}
	if exists, _ := s.Exists(id); exists {
		id = id + "-" + s.now().UTC().Format("20060102150405")
	}
	target := filepath.Join(s.root, "drafts", id)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return Skill{}, err
	}
	content := skillMarkdown(name, description, cleanTags, body)
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(content), 0o600); err != nil {
		return Skill{}, err
	}
	doc, err := ParseSkillFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}
	hash, err := hashFiles(target)
	if err != nil {
		return Skill{}, err
	}
	now := s.now().UTC()
	skill := Skill{
		ID:          id,
		Name:        doc.Name,
		Description: doc.Description,
		Tags:        doc.Tags,
		Category:    doc.Category,
		Path:        target,
		Source:      "draft",
		Provenance:  "matrixclaw-ui",
		Hash:        hash,
		TrustState:  TrustQuarantine,
		State:       StateActive,
		Enabled:     false,
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if err := s.upsertSkill(skill, doc.Body); err != nil {
		return Skill{}, err
	}
	return skill, nil
}

func (s *Service) UpdateMetadata(id string, update MetadataUpdate) (Skill, error) {
	detail, err := s.Get(id)
	if err != nil {
		return Skill{}, err
	}
	doc, err := ParseSkillFile(filepath.Join(detail.Skill.Path, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}
	metadata := doc.Metadata
	if strings.TrimSpace(update.Name) != "" {
		metadata["name"] = strings.TrimSpace(update.Name)
	}
	if strings.TrimSpace(update.Description) != "" {
		metadata["description"] = strings.TrimSpace(update.Description)
	}
	if update.Tags != nil {
		metadata["tags"] = cleanStringSlice(update.Tags)
	}
	if strings.TrimSpace(update.Category) != "" {
		metadata["category"] = strings.TrimSpace(update.Category)
	}
	rawMetadata, err := yaml.Marshal(metadata)
	if err != nil {
		return Skill{}, err
	}
	content := "---\n" + strings.TrimSpace(string(rawMetadata)) + "\n---\n" + strings.TrimSpace(doc.Body) + "\n"
	if err := os.WriteFile(filepath.Join(detail.Skill.Path, "SKILL.md"), []byte(content), 0o600); err != nil {
		return Skill{}, err
	}
	parsed, err := ParseSkillFile(filepath.Join(detail.Skill.Path, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}
	hash, err := hashFiles(detail.Skill.Path)
	if err != nil {
		return Skill{}, err
	}
	skill := detail.Skill
	skill.Name = parsed.Name
	skill.Description = parsed.Description
	skill.Tags = parsed.Tags
	skill.Category = parsed.Category
	skill.Hash = hash
	skill.UpdatedAt = s.now().UTC()
	if err := s.upsertSkill(skill, parsed.Body); err != nil {
		return Skill{}, err
	}
	return skill, nil
}

func (s *Service) UpdateBody(id string, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("skill body must not be empty")
	}
	detail, err := s.Get(id)
	if err != nil {
		return err
	}
	doc, err := ParseSkillFile(filepath.Join(detail.Skill.Path, "SKILL.md"))
	if err != nil {
		return err
	}
	rawMetadata, err := yaml.Marshal(doc.Metadata)
	if err != nil {
		return err
	}
	content := "---\n" + strings.TrimSpace(string(rawMetadata)) + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(detail.Skill.Path, "SKILL.md"), []byte(content), 0o600); err != nil {
		return err
	}
	hash, err := hashFiles(detail.Skill.Path)
	if err != nil {
		return err
	}
	skill := detail.Skill
	skill.Hash = hash
	skill.UpdatedAt = s.now().UTC()
	return s.upsertSkill(skill, body)
}

func (s *Service) Remove(id string) error {
	skill, err := s.getSkill(NormalizeID(id))
	if err != nil {
		return err
	}
	if err := os.RemoveAll(skill.Path); err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM skill_fts WHERE id = ?`, skill.ID)
	_, _ = s.db.Exec(`DELETE FROM skill_sessions WHERE skill_id = ?`, skill.ID)
	_, err = s.db.Exec(`DELETE FROM skills WHERE id = ?`, skill.ID)
	return err
}

func (s *Service) Usage() (UsageSummary, error) {
	skills, err := s.querySkills(`SELECT ` + skillColumns("") + ` FROM skills ORDER BY use_count DESC, view_count DESC, name`)
	return UsageSummary{Skills: skills}, err
}

func (s *Service) Curator() (CuratorResult, error) {
	// Conservative v1: report candidates only; automatic archival is intentionally disabled
	// unless a future agent-created marker is added.
	return CuratorResult{}, nil
}

func (s *Service) getSkill(id string) (Skill, error) {
	skills, err := s.querySkills(`SELECT `+skillColumns("")+` FROM skills WHERE id = ?`, id)
	if err != nil {
		return Skill{}, err
	}
	if len(skills) == 0 {
		return Skill{}, fmt.Errorf("skill not found: %s", id)
	}
	return skills[0], nil
}

func (s *Service) querySkills(query string, args ...any) ([]Skill, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

type skillScanner interface {
	Scan(dest ...any) error
}

func scanSkill(row skillScanner) (Skill, error) {
	var skill Skill
	var authorsJSON, tagsJSON, platformsJSON string
	var enabled, pinned int
	var installed, updated, activity string
	err := row.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Version, &skill.Author, &authorsJSON, &skill.License, &tagsJSON, &platformsJSON, &skill.Category, &skill.Path, &skill.Source, &skill.Provenance, &skill.Hash, &skill.TrustState, &skill.State, &enabled, &pinned, &skill.UseCount, &skill.ViewCount, &skill.PatchCount, &installed, &updated, &activity)
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
	if strings.TrimSpace(prefix) == "" {
		return strings.Join(columns, ", ")
	}
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, prefix+"."+column)
	}
	return strings.Join(out, ", ")
}

func skillMarkdown(name string, description string, tags []string, body string) string {
	metadata := map[string]any{
		"name":        strings.TrimSpace(name),
		"description": strings.TrimSpace(description),
	}
	if len(tags) > 0 {
		metadata["tags"] = tags
	}
	raw, _ := yaml.Marshal(metadata)
	return "---\n" + strings.TrimSpace(string(raw)) + "\n---\n" + strings.TrimSpace(body) + "\n"
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func copyDir(source string, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0o600)
	})
}

func ftsQuery(query string) string {
	parts := strings.Fields(strings.ToLower(query))
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, `"':;,.!?()[]{}<>`)
		part = cleanFTSTerm(part)
		if part != "" {
			cleaned = append(cleaned, part+"*")
		}
	}
	sort.Strings(cleaned)
	return strings.Join(cleaned, " ")
}

func cleanFTSTerm(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
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

func (s *Service) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func (s *Service) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}
