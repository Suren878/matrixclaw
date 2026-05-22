package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillFileFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: deploy-helper
description: Guides safe deployments
version: 1.2.3
author: Platform
license: MIT
tags: [deploy, release]
platforms: [codex, claude]
metadata:
  hermes:
    category: software-development
---
# Deploy Helper

Use staged rollout steps.
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	doc, err := ParseSkillFile(path)
	if err != nil {
		t.Fatalf("ParseSkillFile() error = %v", err)
	}
	if doc.Name != "deploy-helper" {
		t.Fatalf("Name = %q", doc.Name)
	}
	if doc.Description != "Guides safe deployments" {
		t.Fatalf("Description = %q", doc.Description)
	}
	if doc.Version != "1.2.3" || doc.Author != "Platform" || doc.License != "MIT" {
		t.Fatalf("metadata not parsed: %+v", doc.Metadata)
	}
	if got := doc.Category; got != "software-development" {
		t.Fatalf("Category = %q", got)
	}
	if len(doc.Tags) != 2 || doc.Tags[0] != "deploy" || doc.Tags[1] != "release" {
		t.Fatalf("Tags = %#v", doc.Tags)
	}
	if doc.Body != "# Deploy Helper\n\nUse staged rollout steps." {
		t.Fatalf("Body = %q", doc.Body)
	}
}

func TestParseSkillFileRejectsMissingDescription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: incomplete
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ParseSkillFile(path); err == nil {
		t.Fatal("ParseSkillFile() error = nil, want validation error")
	}
}

func TestValidateSkillBundleRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: linked
description: Linked skill
---
Body.
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "references")); err != nil {
		t.Fatal(err)
	}

	if err := ValidateSkillBundle(dir); err == nil {
		t.Fatal("ValidateSkillBundle() error = nil, want symlink rejection")
	}
}
