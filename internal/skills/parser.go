package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const MaxSkillFileBytes int64 = 512 * 1024

var validSupportDirs = []string{"scripts", "references", "assets"}

func ParseSkillFile(path string) (Document, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Document{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Document{}, fmt.Errorf("skill file must not be a symlink: %s", path)
	}
	if info.Size() <= 0 || info.Size() > MaxSkillFileBytes {
		return Document{}, fmt.Errorf("skill file size must be between 1 and %d bytes", MaxSkillFileBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	frontmatter, body, ok := splitFrontmatter(string(raw))
	if !ok {
		return Document{}, fmt.Errorf("SKILL.md must start with YAML frontmatter")
	}
	var metadata map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
		return Document{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	doc := Document{
		Name:        cleanString(metadata["name"]),
		Description: cleanString(metadata["description"]),
		Version:     cleanString(metadata["version"]),
		Author:      cleanString(metadata["author"]),
		Authors:     stringSlice(metadata["authors"]),
		License:     cleanString(metadata["license"]),
		Tags:        stringSlice(metadata["tags"]),
		Platforms:   stringSlice(metadata["platforms"]),
		Category:    firstNonEmpty(cleanString(metadata["category"]), cleanString(nested(metadata, "metadata", "hermes", "category"))),
		Body:        strings.TrimSpace(body),
		Metadata:    metadata,
	}
	if len(doc.Authors) == 0 && doc.Author != "" {
		doc.Authors = []string{doc.Author}
	}
	if doc.Name == "" {
		return Document{}, fmt.Errorf("SKILL.md frontmatter requires name")
	}
	if doc.Description == "" {
		return Document{}, fmt.Errorf("SKILL.md frontmatter requires description")
	}
	if doc.Body == "" {
		return Document{}, fmt.Errorf("SKILL.md body must not be empty")
	}
	return doc, nil
}

func ValidateSkillBundle(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return fmt.Errorf("skill root is required")
	}
	if _, err := ParseSkillFile(filepath.Join(root, "SKILL.md")); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill bundle must not contain symlink: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("invalid skill path: %s", rel)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 1 && parts[0] == "SKILL.md" {
			return nil
		}
		if len(parts) > 0 && slices.Contains(validSupportDirs, parts[0]) {
			if !entry.IsDir() && info.Size() > MaxSkillFileBytes {
				return fmt.Errorf("skill support file is too large: %s", rel)
			}
			return nil
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		return fmt.Errorf("unsupported file in skill bundle: %s", rel)
	})
}

func splitFrontmatter(raw string) (string, string, bool) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return "", "", false
	}
	rest := raw[len("---\n"):]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", "", false
	}
	frontmatter := rest[:idx]
	body := strings.TrimPrefix(rest[idx+len("\n---"):], "\n")
	return frontmatter, body, true
}

func cleanString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	case nil:
		return ""
	}
}

func stringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return cleanStrings(v)
	case []any:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if s := cleanString(item); s != "" {
				values = append(values, s)
			}
		}
		return values
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func nested(values map[string]any, path ...string) any {
	var current any = values
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[key]
	}
	return current
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var skillIDCleanup = regexp.MustCompile(`[^a-z0-9_-]+`)

func NormalizeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = skillIDCleanup.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-_")
	if value == "" {
		return "skill"
	}
	return value
}

func hashFiles(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write(raw)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
