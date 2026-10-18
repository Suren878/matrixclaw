package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func paths(files []InstructionFile) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, file.Path)
	}
	return out
}

func TestInstructionsRunFromTheTopDownToTheWorkingDir(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "projects", "app")
	wd := filepath.Join(repo, "cmd", "tool")
	global := filepath.Join(home, ".config", "matrixclaw", "AGENTS.md")
	writeFile(t, global, "global rules")
	writeFile(t, filepath.Join(home, "projects", "AGENTS.md"), "projects rules")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "repo agents")
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "repo claude, shadowed by AGENTS.md")
	writeFile(t, filepath.Join(repo, ".matrixclaw", "instructions.md"), "repo matrixclaw")
	writeFile(t, filepath.Join(repo, "cmd", "CLAUDE.md"), "cmd claude")
	writeFile(t, filepath.Join(wd, "AGENTS.md"), "   \n")
	writeFile(t, filepath.Join(home, "elsewhere", "AGENTS.md"), "not on the path")

	got := paths(Instructions(wd, repo, home, global))
	want := []string{
		global,
		filepath.Join(home, "projects", "AGENTS.md"),
		filepath.Join(repo, "AGENTS.md"),
		filepath.Join(repo, ".matrixclaw", "instructions.md"),
		filepath.Join(repo, "cmd", "CLAUDE.md"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestInstructionsOutsideHomeStartAtTheGitRoot(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	writeFile(t, filepath.Join(base, "AGENTS.md"), "above the repository")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "repo")
	wd := filepath.Join(repo, "sub")
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := paths(Instructions(wd, repo, "/nonexistent-home", "")); len(got) != 1 || got[0] != filepath.Join(repo, "AGENTS.md") {
		t.Fatalf("files = %v", got)
	}
	if got := paths(Instructions(wd, "", "/nonexistent-home", "")); len(got) != 0 {
		t.Fatalf("outside a repository only the working dir counts, got %v", got)
	}
}

func TestInstructionsShareOneByteBudget(t *testing.T) {
	dir := t.TempDir()
	line := strings.Repeat("x", 99) + "\n"
	writeFile(t, filepath.Join(dir, "global.md"), strings.Repeat(line, (MaxInstructionBytes-250)/len(line)))
	writeFile(t, filepath.Join(dir, "AGENTS.md"), strings.Repeat(line, 10))
	writeFile(t, filepath.Join(dir, ".matrixclaw", "instructions.md"), "left out")

	files := Instructions(dir, "", "", filepath.Join(dir, "global.md"))
	if len(files) != 3 {
		t.Fatalf("files = %v", paths(files))
	}
	total := 0
	for _, file := range files {
		total += len(file.Content)
	}
	if total > MaxInstructionBytes || files[0].Truncated || !files[1].Truncated || files[1].Content != strings.Repeat(line, 2) {
		t.Fatalf("total %d, truncated %v %v", total, files[0].Truncated, files[1].Truncated)
	}
	if !files[2].Truncated || files[2].Content != "" {
		t.Fatalf("file past the budget = %+v", files[2])
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func TestReadGitSnapshotsBranchStatusAndCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	run(t, repo, "git", "init", "-q", "-b", "main")
	if git, ok := ReadGit(context.Background(), repo); !ok || git.Branch != "branch main (no commits yet)" || len(git.Commits) != 0 {
		t.Fatalf("empty repository = %+v, %v", git, ok)
	}
	var names []string
	for i := range maxStatusLines + 4 {
		names = append(names, filepath.Join("dir", string(rune('a'+i))+".txt"))
		writeFile(t, filepath.Join(repo, names[i]), "x")
	}
	run(t, repo, append([]string{"git", "add"}, names...)...)
	run(t, repo, "git", "commit", "-q", "-m", "first commit")
	for _, name := range names {
		writeFile(t, filepath.Join(repo, name), "changed")
	}
	sub := filepath.Join(repo, "dir")

	git, ok := ReadGit(context.Background(), sub)
	if !ok {
		t.Fatal("repository not recognised")
	}
	root, _ := filepath.EvalSymlinks(repo)
	if git.Root != root || git.Branch != "branch main" || len(git.Commits) != 1 || !strings.HasSuffix(git.Commits[0], "first commit") {
		t.Fatalf("snapshot = %+v", git)
	}
	if len(git.Status) != maxStatusLines || git.MoreStatus != 4 || git.Status[0] != " M dir/a.txt" {
		t.Fatalf("status = %q, %d more", git.Status, git.MoreStatus)
	}
	if _, ok := ReadGit(context.Background(), t.TempDir()); ok {
		t.Fatal("a plain directory read as a repository")
	}
}

func TestDescribeBranch(t *testing.T) {
	for header, want := range map[string]string{
		"main":                                   "branch main",
		"main...origin/main":                     "branch main (upstream origin/main)",
		"feat...origin/feat [ahead 1, behind 2]": "branch feat (upstream origin/feat, ahead 1, behind 2)",
		"HEAD (no branch)":                       "detached HEAD",
		"No commits yet on main":                 "branch main (no commits yet)",
	} {
		if got := describeBranch(header); got != want {
			t.Errorf("describeBranch(%q) = %q, want %q", header, got, want)
		}
	}
}
