package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/textutil"
	"gopkg.in/yaml.v3"
)

// ErrNotFound reports a skill the library does not hold.
var ErrNotFound = errors.New("skill not found")

// Service is the skills library: skill directories under root, their rows in
// the store's database.
type Service struct {
	db       *sql.DB
	root     string
	now      func() time.Time
	settings atomic.Pointer[Settings]
}

// NewService opens the library at root on the store's database.
func NewService(db *sql.DB, root string) (*Service, error) {
	if db == nil || strings.TrimSpace(root) == "" {
		return nil, errors.New("skills: database and root are required")
	}
	root = filepath.Clean(root)
	for _, dir := range []string{"installed", "quarantine", "drafts"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			return nil, err
		}
	}
	service := &Service{db: db, root: root, now: time.Now}
	service.Configure(Settings{})
	return service, nil
}

// Configure applies the settings from setup.json; it is safe at any time.
func (s *Service) Configure(settings Settings) {
	s.settings.Store(&settings)
}

func (s *Service) currentSettings() Settings {
	return *s.settings.Load()
}

// List lists skills without a query.
func (s *Service) List(ctx context.Context, opts SearchOptions) ([]Skill, error) {
	return s.Search(ctx, "", opts)
}

// Get returns a skill and its SKILL.md body.
func (s *Service) Get(ctx context.Context, id string) (SkillDetail, error) {
	skill, err := s.getSkill(ctx, NormalizeID(id))
	if err != nil {
		return SkillDetail{}, err
	}
	doc, err := ParseSkillFile(filepath.Join(skill.Path, "SKILL.md"))
	if err != nil {
		return SkillDetail{}, err
	}
	return SkillDetail{Skill: skill, Body: doc.Body}, nil
}

// View is Get counted as a view.
func (s *Service) View(ctx context.Context, id string) (SkillDetail, error) {
	detail, err := s.Get(ctx, id)
	if err != nil {
		return SkillDetail{}, err
	}
	now := s.now().UTC()
	s.countActivity(ctx, detail.Skill.ID, "view_count", now)
	detail.Skill.ViewCount++
	detail.Skill.LastActivityAt = now
	return detail, nil
}

// Use activates a trusted, enabled, active skill for the session.
func (s *Service) Use(ctx context.Context, sessionID string, id string) (SkillDetail, error) {
	detail, err := s.Get(ctx, id)
	if err != nil {
		return SkillDetail{}, err
	}
	if !usable(detail.Skill) {
		return SkillDetail{}, fmt.Errorf("skill %s is not trusted and active", detail.Skill.ID)
	}
	now := s.now().UTC()
	s.countActivity(ctx, detail.Skill.ID, "use_count", now)
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		if err := s.activate(ctx, sessionID, detail.Skill.ID, now); err != nil {
			return SkillDetail{}, err
		}
	}
	detail.Skill.UseCount++
	detail.Skill.LastActivityAt = now
	return detail, nil
}

// Unload deactivates a skill for the session.
func (s *Service) Unload(ctx context.Context, sessionID string, id string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return s.deactivate(ctx, sessionID, NormalizeID(id))
}

// Apply runs a library action on the skill id.
func (s *Service) Apply(ctx context.Context, id string, action Action) error {
	id = NormalizeID(id)
	switch action {
	case ActionTrust:
		return s.setTrust(ctx, id, TrustTrusted, true)
	case ActionQuarantine:
		return s.setTrust(ctx, id, TrustQuarantine, false)
	case ActionEnable:
		return s.updateSkill(ctx, id, `enabled = 1`)
	case ActionDisable:
		if err := s.updateSkill(ctx, id, `enabled = 0`); err != nil {
			return err
		}
		return s.deactivate(ctx, "", id)
	case ActionArchive:
		return s.updateSkill(ctx, id, `state = ?`, StateArchived)
	case ActionRestore:
		return s.updateSkill(ctx, id, `state = ?`, StateActive)
	case ActionPin:
		return s.updateSkill(ctx, id, `pinned = 1`)
	case ActionUnpin:
		return s.updateSkill(ctx, id, `pinned = 0`)
	case ActionRemove:
		return s.remove(ctx, id)
	default:
		return fmt.Errorf("unknown skill action %q", action)
	}
}

// setTrust moves the skill's directory to the trust state's bucket; a skill
// leaving trust is disabled and unloaded everywhere.
func (s *Service) setTrust(ctx context.Context, id string, trust string, enabled bool) error {
	skill, err := s.getSkill(ctx, id)
	if err != nil {
		return err
	}
	target := filepath.Join(s.root, trustBucket(trust), skill.ID)
	if filepath.Clean(skill.Path) != target {
		if err := os.Rename(skill.Path, target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := s.updateSkill(ctx, skill.ID, `trust_state = ?, path = ?, enabled = ?`, trust, target, boolInt(enabled)); err != nil {
		return err
	}
	if enabled {
		return nil
	}
	return s.deactivate(ctx, "", skill.ID)
}

func (s *Service) remove(ctx context.Context, id string) error {
	skill, err := s.getSkill(ctx, id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(skill.Path); err != nil {
		return err
	}
	return s.deleteSkill(ctx, skill.ID)
}

// CreateDraft writes a quarantined, disabled skill under drafts/.
func (s *Service) CreateDraft(ctx context.Context, name string, description string, tags []string, body string) (Skill, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	body = strings.TrimSpace(body)
	if name == "" || description == "" {
		return Skill{}, errors.New("skill draft requires name and description")
	}
	if body == "" {
		body = "Describe when to use this skill and the steps to follow."
	}
	id := NormalizeID(name)
	if exists, err := s.exists(ctx, id); err != nil {
		return Skill{}, err
	} else if exists {
		id = id + "-" + s.now().UTC().Format("20060102150405")
	}
	target := filepath.Join(s.root, "drafts", id)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return Skill{}, err
	}
	metadata := map[string]any{"name": name, "description": description}
	if tags = textutil.UniqueNonBlank(tags...); len(tags) > 0 {
		metadata["tags"] = tags
	}
	doc, hash, err := writeSkillFile(target, metadata, body)
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
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if err := s.upsertSkill(ctx, skill, doc.Body); err != nil {
		return Skill{}, err
	}
	return skill, nil
}

// UpdateMetadata rewrites the SKILL.md frontmatter fields that are set.
func (s *Service) UpdateMetadata(ctx context.Context, id string, update MetadataUpdate) (Skill, error) {
	detail, doc, err := s.skillFile(ctx, id)
	if err != nil {
		return Skill{}, err
	}
	metadata := doc.Metadata
	if value := strings.TrimSpace(update.Name); value != "" {
		metadata["name"] = value
	}
	if value := strings.TrimSpace(update.Description); value != "" {
		metadata["description"] = value
	}
	if update.Tags != nil {
		metadata["tags"] = textutil.UniqueNonBlank(update.Tags...)
	}
	if value := strings.TrimSpace(update.Category); value != "" {
		metadata["category"] = value
	}
	parsed, hash, err := writeSkillFile(detail.Path, metadata, doc.Body)
	if err != nil {
		return Skill{}, err
	}
	skill := detail
	skill.Name = parsed.Name
	skill.Description = parsed.Description
	skill.Tags = parsed.Tags
	skill.Category = parsed.Category
	skill.Hash = hash
	skill.UpdatedAt = s.now().UTC()
	if err := s.upsertSkill(ctx, skill, parsed.Body); err != nil {
		return Skill{}, err
	}
	return skill, nil
}

// UpdateBody replaces the instructions of SKILL.md.
func (s *Service) UpdateBody(ctx context.Context, id string, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("skill body must not be empty")
	}
	skill, doc, err := s.skillFile(ctx, id)
	if err != nil {
		return err
	}
	parsed, hash, err := writeSkillFile(skill.Path, doc.Metadata, body)
	if err != nil {
		return err
	}
	skill.Hash = hash
	skill.UpdatedAt = s.now().UTC()
	return s.upsertSkill(ctx, skill, parsed.Body)
}

// WriteFile writes (or with remove deletes) a file of the skill: SKILL.md or
// one under scripts/, references/ or assets/.
func (s *Service) WriteFile(ctx context.Context, id string, rel string, content string, remove bool) error {
	skill, err := s.getSkill(ctx, NormalizeID(id))
	if err != nil {
		return err
	}
	rel, err = supportFilePath(rel)
	if err != nil {
		return err
	}
	dir, err := os.OpenRoot(skill.Path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	if remove {
		err = dir.Remove(rel)
	} else if err = dir.MkdirAll(filepath.Dir(rel), 0o700); err == nil {
		err = dir.WriteFile(rel, []byte(content), 0o600)
	}
	if err != nil {
		return err
	}
	s.countActivity(ctx, skill.ID, "patch_count", s.now().UTC())
	return nil
}

// supportFilePath cleans a path inside a skill and refuses one outside
// SKILL.md and the support directories.
func supportFilePath(rel string) (string, error) {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "SKILL.md" {
		return rel, nil
	}
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("invalid skill file path %q", rel)
	}
	top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	if !strings.Contains(filepath.ToSlash(rel), "/") || !slices.Contains(validSupportDirs, top) {
		return "", errors.New("skill files may only be SKILL.md or under scripts/, references/ or assets/")
	}
	return rel, nil
}

func (s *Service) skillFile(ctx context.Context, id string) (Skill, Document, error) {
	skill, err := s.getSkill(ctx, NormalizeID(id))
	if err != nil {
		return Skill{}, Document{}, err
	}
	doc, err := ParseSkillFile(filepath.Join(skill.Path, "SKILL.md"))
	return skill, doc, err
}

// writeSkillFile writes SKILL.md in dir and returns it parsed back and the
// hash of the skill's files.
func writeSkillFile(dir string, metadata map[string]any, body string) (Document, string, error) {
	raw, err := yaml.Marshal(metadata)
	if err != nil {
		return Document{}, "", err
	}
	content := "---\n" + strings.TrimSpace(string(raw)) + "\n---\n" + strings.TrimSpace(body) + "\n"
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return Document{}, "", err
	}
	doc, err := ParseSkillFile(path)
	if err != nil {
		return Document{}, "", err
	}
	hash, err := hashFiles(dir)
	return doc, hash, err
}

func usable(skill Skill) bool {
	return skill.TrustState == TrustTrusted && skill.State == StateActive && skill.Enabled
}

func trustBucket(trust string) string {
	if trust == TrustTrusted {
		return "installed"
	}
	return "quarantine"
}
