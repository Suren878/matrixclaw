package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// cloneTimeout bounds the git clone of a GitHub skill source.
const cloneTimeout = 2 * time.Minute

// installOptions says where installed skills came from; confine, set for a
// cloned source, is the directory every bundle must resolve inside.
type installOptions struct {
	provenance string
	source     string
	confine    string
}

// InstallPath installs the skills at a local path or a GitHub URL: one
// SKILL.md bundle, a Codex or Claude plugin, or a Hermes skills tree. New
// skills are quarantined unless the trust policy trusts them.
func (s *Service) InstallPath(ctx context.Context, source string) ([]Skill, error) {
	source = strings.TrimSpace(source)
	if isGitHubURL(source) {
		return s.installGitHub(ctx, source)
	}
	return s.installDir(ctx, filepath.Clean(source), installOptions{provenance: source, source: "local"})
}

func (s *Service) installDir(ctx context.Context, dir string, opts installOptions) ([]Skill, error) {
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
		skill, err := s.installBundle(ctx, dir, opts)
		if err != nil {
			return nil, err
		}
		return []Skill{skill}, nil
	}
	for _, manifest := range []string{filepath.Join(dir, ".codex-plugin", "plugin.json"), filepath.Join(dir, ".claude-plugin", "plugin.json")} {
		if _, err := os.Stat(manifest); err == nil {
			return s.installPlugin(ctx, dir, manifest, opts)
		}
	}
	return s.installHermesTree(ctx, dir, opts)
}

func isGitHubURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && strings.EqualFold(parsed.Host, "github.com")
}

// githubSource is a GitHub URL read as a repository, an optional ref and a
// directory inside it.
type githubSource struct {
	repoURL string
	ref     string
	subpath string
}

var githubName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func parseGitHubSource(rawURL string) (githubSource, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return githubSource{}, err
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return githubSource{}, errors.New("github URL must include owner and repository")
	}
	owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
	for _, name := range []string{owner, repo} {
		if !githubName.MatchString(name) || name == "." || name == ".." {
			return githubSource{}, fmt.Errorf("invalid github owner or repository %q", name)
		}
	}
	source := githubSource{repoURL: "https://github.com/" + owner + "/" + repo + ".git"}
	if len(parts) >= 4 && parts[2] == "tree" {
		source.ref = parts[3]
		if len(parts) > 4 {
			source.subpath = filepath.FromSlash(path.Join(parts[4:]...))
			if !filepath.IsLocal(source.subpath) {
				return githubSource{}, fmt.Errorf("github URL path %q leaves the repository", path.Join(parts[4:]...))
			}
		}
	}
	return source, nil
}

func (s *Service) installGitHub(ctx context.Context, rawURL string) ([]Skill, error) {
	source, err := parseGitHubSource(rawURL)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Join(s.root, "quarantine"), "github-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := s.clone(ctx, source, tmp); err != nil {
		return nil, err
	}
	confine, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		return nil, err
	}
	return s.installDir(ctx, filepath.Join(tmp, source.subpath), installOptions{provenance: rawURL, source: "github", confine: confine})
}

// clone makes a shallow clone without prompts and without the daemon's keys
// in git's environment.
func (s *Service) clone(ctx context.Context, source githubSource, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()
	args := []string{"clone", "--depth", "1"}
	if source.ref != "" {
		args = append(args, "--branch="+source.ref)
	}
	args = append(args, "--", source.repoURL, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(slices.Clone(s.currentSettings().GitEnv), "GIT_TERMINAL_PROMPT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone github skill source: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// installBundle copies one SKILL.md bundle into the library.
func (s *Service) installBundle(ctx context.Context, dir string, opts installOptions) (Skill, error) {
	if opts.confine != "" {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return Skill{}, err
		}
		if rel, err := filepath.Rel(opts.confine, resolved); err != nil || !filepath.IsLocal(rel) {
			return Skill{}, fmt.Errorf("skill path leaves the cloned repository: %s", dir)
		}
	}
	if err := ValidateSkillBundle(dir); err != nil {
		return Skill{}, err
	}
	doc, err := ParseSkillFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}
	hash, err := hashFiles(dir)
	if err != nil {
		return Skill{}, err
	}
	trust := TrustQuarantine
	if strings.EqualFold(s.currentSettings().TrustPolicy, TrustTrusted) {
		trust = TrustTrusted
	}
	id := NormalizeID(doc.Name)
	if exists, err := s.exists(ctx, id); err != nil {
		return Skill{}, err
	} else if exists {
		id = id + "-" + hash[:8]
	}
	target := filepath.Join(s.root, trustBucket(trust), id)
	if err := os.RemoveAll(target); err != nil {
		return Skill{}, err
	}
	if err := copyDir(dir, target); err != nil {
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
		Source:      opts.source,
		Provenance:  opts.provenance,
		Hash:        hash,
		TrustState:  trust,
		State:       StateActive,
		Enabled:     trust == TrustTrusted,
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if err := s.upsertSkill(ctx, skill, doc.Body); err != nil {
		return Skill{}, err
	}
	return skill, nil
}

type pluginManifest struct {
	Skills []string `json:"skills"`
}

func (s *Service) installPlugin(ctx context.Context, root string, manifestPath string, opts installOptions) ([]Skill, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse plugin manifest: %w", err)
	}
	opts.source = "plugin"
	var installed []Skill
	for _, rel := range manifest.Skills {
		rel = filepath.Clean(strings.TrimSpace(rel))
		if rel == "." {
			continue
		}
		if !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("plugin skill path escapes plugin root: %s", rel)
		}
		skill, err := s.installBundle(ctx, filepath.Join(root, rel), opts)
		if err != nil {
			return nil, err
		}
		installed = append(installed, skill)
	}
	if len(installed) == 0 {
		return nil, errors.New("plugin has no importable skills")
	}
	return installed, nil
}

// installHermesTree installs every valid bundle under skills/ and
// optional-skills/, skipping the invalid ones.
func (s *Service) installHermesTree(ctx context.Context, root string, opts installOptions) ([]Skill, error) {
	opts.source = "hermes"
	var installed []Skill
	for _, base := range []string{filepath.Join(root, "skills"), filepath.Join(root, "optional-skills")} {
		_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || entry.Name() != "SKILL.md" {
				return nil
			}
			if skill, err := s.installBundle(ctx, filepath.Dir(path), opts); err == nil {
				installed = append(installed, skill)
			}
			return nil
		})
	}
	if len(installed) == 0 {
		return nil, fmt.Errorf("path does not contain SKILL.md or a supported plugin/Hermes skill tree: %s", root)
	}
	return installed, nil
}

// copyDir copies a validated bundle, without its hidden directories (.git).
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
			if rel != "." && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return os.MkdirAll(dest, 0o700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0o600)
	})
}
