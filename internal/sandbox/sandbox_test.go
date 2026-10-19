package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSettingsPolicyFollowsTheSessionAndTheMode(t *testing.T) {
	work := t.TempDir()
	cases := []struct {
		name     string
		settings Settings
		run      Run
		mode     Mode
		strict   bool
	}{
		{"default leaves attended sessions alone", Settings{}, Run{WorkingDir: work}, "", false},
		{"default confines full_auto", Settings{}, Run{WorkingDir: work, Unattended: true}, WorkspaceWrite, false},
		{"default makes read-only runs read_only", Settings{}, Run{WorkingDir: work, ReadOnly: true}, ReadOnly, false},
		{"off confines nothing", Settings{Mode: Off}, Run{WorkingDir: work, ReadOnly: true, Unattended: true}, "", false},
		{"a set mode confines every session", Settings{Mode: WorkspaceWrite}, Run{WorkingDir: work}, WorkspaceWrite, true},
		{"read-only runs stay read_only", Settings{Mode: WorkspaceWrite}, Run{WorkingDir: work, ReadOnly: true}, ReadOnly, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.settings.Policy(tc.run)
			if p.Mode != tc.mode || p.Strict != tc.strict {
				t.Fatalf("policy = %+v, want mode %q strict %v", p, tc.mode, tc.strict)
			}
		})
	}
}

func TestWorkspaceWriteNeverOpensTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	if roots := WorkspaceRoots(home, home); len(roots) != 0 {
		t.Fatalf("roots of home = %q", roots)
	}
	if roots := WorkspaceRoots(filepath.Dir(home), home); len(roots) != 0 {
		t.Fatalf("roots above home = %q", roots)
	}
	project := filepath.Join(home, "project")
	if roots := WorkspaceRoots(project, home); !slices.Equal(roots, []string{project}) {
		t.Fatalf("roots of a project = %q", roots)
	}
}

func TestWorkspaceRootsIncludeAWorktreesGitDirectories(t *testing.T) {
	base := t.TempDir()
	common := filepath.Join(base, "repo", ".git")
	gitDir := filepath.Join(common, "worktrees", "child")
	worktree := filepath.Join(base, "child")
	for _, dir := range []string{gitDir, filepath.Join(worktree, "pkg")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+gitDir+"\n")
	writeFile(t, filepath.Join(gitDir, "commondir"), "../..\n")

	roots := WorkspaceRoots(filepath.Join(worktree, "pkg"), "")
	want := []string{filepath.Join(worktree, "pkg"), gitDir, common}
	if !slices.Equal(roots, want) {
		t.Fatalf("roots = %q, want %q", roots, want)
	}
}

func TestValidateRejectsUnknownModesAndRelativePaths(t *testing.T) {
	relative := []string{"cache"}
	if err := (Settings{Mode: "danger"}).Validate(); err == nil {
		t.Fatal("an unknown mode passed")
	}
	if err := (Settings{Writable: []string{"build"}}).Validate(); err == nil {
		t.Fatal("a relative writable path passed")
	}
	if err := (Settings{Caches: &relative}).Validate(); err == nil {
		t.Fatal("a relative cache passed")
	}
	if err := (Settings{Mode: ReadOnly, Writable: []string{"/srv/build", "~/out"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDenialHintExplainsRefusedWritesOnlyUnderASandbox(t *testing.T) {
	confined := Policy{Mode: WorkspaceWrite, Workspace: []string{"/work"}}
	if confined.DenialHint("touch: cannot touch '/etc/x': Permission denied") == "" {
		t.Fatal("no hint for a refused write")
	}
	if confined.DenialHint("FAIL: TestX") != "" {
		t.Fatal("a hint for an unrelated failure")
	}
	if (Policy{}).DenialHint("Permission denied") != "" {
		t.Fatal("a hint without a sandbox")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
