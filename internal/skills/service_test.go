package skills_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/store"
)

func newLibrary(t *testing.T) (*skills.Service, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.NewSQLite(filepath.Join(dir, "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root := filepath.Join(dir, "skills")
	service, err := skills.NewService(db.DB(), root)
	if err != nil {
		t.Fatal(err)
	}
	return service, root
}

func writeBundle(t *testing.T, dir string, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"SKILL.md":       "---\nname: " + name + "\ndescription: Use when " + name + " is asked.\n---\nFollow the steps.\n",
		"scripts/run.sh": "echo run\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDraftLifecycle(t *testing.T) {
	service, _ := newLibrary(t)
	ctx := context.Background()

	draft, err := service.CreateDraft(ctx, "Release Notes", "Write release notes", []string{"docs", "docs"}, "Read the changelog.")
	if err != nil {
		t.Fatal(err)
	}
	if draft.ID != "release-notes" || draft.TrustState != skills.TrustQuarantine || draft.Enabled {
		t.Fatalf("draft = %+v", draft)
	}
	if _, err := service.UpdateMetadata(ctx, draft.ID, skills.MetadataUpdate{Description: "Draft the notes of a release", Category: "writing"}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateBody(ctx, draft.ID, "List every user-visible change."); err != nil {
		t.Fatal(err)
	}
	detail, err := service.Get(ctx, draft.ID)
	if err != nil || detail.Body != "List every user-visible change." || detail.Skill.Description != "Draft the notes of a release" || detail.Skill.Category != "writing" || len(detail.Skill.Tags) != 1 {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	found, err := service.Search(ctx, "user-visible", skills.SearchOptions{IncludeQuarantined: true, IncludeDisabled: true})
	if err != nil || len(found) != 1 || found[0].ID != draft.ID {
		t.Fatalf("search = %+v, %v", found, err)
	}
	for _, action := range []skills.Action{skills.ActionPin, skills.ActionArchive} {
		if err := service.Apply(ctx, draft.ID, action); err != nil {
			t.Fatal(action, err)
		}
	}
	if got, _ := service.Get(ctx, draft.ID); !got.Skill.Pinned || got.Skill.State != skills.StateArchived {
		t.Fatalf("after pin and archive = %+v", got.Skill)
	}

	if err := service.Apply(ctx, draft.ID, skills.ActionRemove); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, draft.ID); !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("get after remove = %v", err)
	}
	if _, err := os.Stat(draft.Path); !os.IsNotExist(err) {
		t.Fatalf("draft directory after remove: %v", err)
	}
	if err := service.Apply(ctx, "missing", skills.ActionPin); !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("action on an unknown skill = %v", err)
	}
}

func TestInstalledSkillsStayQuarantinedUntilTrusted(t *testing.T) {
	service, root := newLibrary(t)
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "review")
	writeBundle(t, source, "review")

	installed, err := service.InstallPath(ctx, source)
	if err != nil || len(installed) != 1 {
		t.Fatalf("installed = %+v, %v", installed, err)
	}
	skill := installed[0]
	if skill.TrustState != skills.TrustQuarantine || skill.Enabled || skill.Path != filepath.Join(root, "quarantine", "review") {
		t.Fatalf("installed skill = %+v", skill)
	}
	if _, err := service.Use(ctx, "s1", skill.ID); err == nil {
		t.Fatal("a quarantined skill was activated")
	}

	if err := service.Apply(ctx, skill.ID, skills.ActionTrust); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Use(ctx, "s1", skill.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "installed", "review", "scripts", "run.sh")); err != nil {
		t.Fatalf("trusted skill files: %v", err)
	}

	if err := service.Apply(ctx, skill.ID, skills.ActionQuarantine); err != nil {
		t.Fatal(err)
	}
	detail, err := service.Get(ctx, skill.ID)
	if err != nil || detail.Skill.Enabled || detail.Skill.Path != filepath.Join(root, "quarantine", "review") {
		t.Fatalf("quarantined again = %+v, %v", detail.Skill, err)
	}
	if active, err := service.SessionSkills(ctx, "s1"); err != nil || len(active) != 0 {
		t.Fatalf("session skills after quarantine = %+v, %v", active, err)
	}

	service.Configure(skills.Settings{TrustPolicy: skills.TrustTrusted})
	other := filepath.Join(t.TempDir(), "deploy")
	writeBundle(t, other, "deploy")
	trusted, err := service.InstallPath(ctx, other)
	if err != nil || len(trusted) != 1 || trusted[0].TrustState != skills.TrustTrusted || !trusted[0].Enabled {
		t.Fatalf("install under the trusted policy = %+v, %v", trusted, err)
	}
}

// gitFixture makes a repository at <dir>/acme/kit.git and the environment
// that sends https://github.com/ clones to dir.
func gitFixture(t *testing.T) (repo string, env []string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	repo = filepath.Join(dir, "acme", "kit.git")
	writeBundle(t, filepath.Join(repo, "review"), "review")
	env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=url.file://" + dir + "/.insteadOf",
		"GIT_CONFIG_VALUE_0=https://github.com/",
	}
	return repo, env
}

func commitAll(t *testing.T, repo string, env []string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "skills"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
}

func TestInstallFromAGitHubTreeURL(t *testing.T) {
	repo, env := gitFixture(t)
	commitAll(t, repo, env)
	service, root := newLibrary(t)
	service.Configure(skills.Settings{GitEnv: env})

	installed, err := service.InstallPath(context.Background(), "https://github.com/acme/kit/tree/main/review")
	if err != nil || len(installed) != 1 {
		t.Fatalf("installed = %+v, %v", installed, err)
	}
	if skill := installed[0]; skill.Source != "github" || skill.TrustState != skills.TrustQuarantine || skill.Provenance != "https://github.com/acme/kit/tree/main/review" {
		t.Fatalf("installed skill = %+v", skill)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "quarantine"))
	if len(entries) != 1 {
		t.Fatalf("quarantine holds %d entries, want the skill only (the clone is removed)", len(entries))
	}
}

func TestGitHubSourcesStayInsideTheClone(t *testing.T) {
	repo, env := gitFixture(t)
	outside := filepath.Join(t.TempDir(), "outside")
	writeBundle(t, outside, "outside")
	if err := os.Symlink(outside, filepath.Join(repo, "linked")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repo, env)
	service, _ := newLibrary(t)
	service.Configure(skills.Settings{GitEnv: env})
	ctx := context.Background()

	for _, url := range []string{
		"https://github.com/acme/kit/tree/main/linked",
		"https://github.com/acme/kit/tree/main/review/..%2F..%2F..",
		"https://github.com/../kit",
	} {
		if installed, err := service.InstallPath(ctx, url); err == nil {
			t.Errorf("%s installed %+v", url, installed)
		}
	}
	if listed, _ := service.List(ctx, skills.SearchOptions{IncludeQuarantined: true, IncludeDisabled: true}); len(listed) != 0 {
		t.Fatalf("library holds %+v", listed)
	}
}

func TestPluginSkillPathsStayInsideThePlugin(t *testing.T) {
	service, _ := newLibrary(t)
	plugin := t.TempDir()
	writeBundle(t, filepath.Join(filepath.Dir(plugin), "sibling"), "sibling")
	if err := os.MkdirAll(filepath.Join(plugin, ".claude-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"), []byte(`{"skills":["../sibling"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InstallPath(context.Background(), plugin); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("install of an escaping plugin path = %v", err)
	}
}

func TestSkillFilesStayInsideTheSkill(t *testing.T) {
	service, _ := newLibrary(t)
	ctx := context.Background()
	draft, err := service.CreateDraft(ctx, "Ops", "Run ops tasks", nil, "Check the dashboards.")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.WriteFile(ctx, draft.ID, "scripts/check.sh", "uptime\n", false); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(draft.Path, "scripts", "check.sh")); err != nil || string(raw) != "uptime\n" {
		t.Fatalf("written file = %q, %v", raw, err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(draft.Path, "assets")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../escape.sh", "/etc/passwd", "notes.txt", "scripts", "scripts/../../escape.sh", "assets/through-link.txt"} {
		if err := service.WriteFile(ctx, draft.ID, rel, "x", false); err == nil {
			t.Errorf("wrote %q", rel)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("a write left the skill: %v", entries)
	}
}
