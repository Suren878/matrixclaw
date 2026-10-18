package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubagentWorktreeRepoIDDistinguishesSameBasename(t *testing.T) {
	t.Parallel()
	first := subagentWorktreeRepoID(filepath.Join(string(filepath.Separator), "srv", "one", "project"))
	second := subagentWorktreeRepoID(filepath.Join(string(filepath.Separator), "srv", "two", "project"))
	if first == second {
		t.Fatalf("repo IDs collide for different roots: %q", first)
	}
	if first != subagentWorktreeRepoID(filepath.Join(string(filepath.Separator), "srv", "one", "project")) {
		t.Fatal("repo ID is not stable")
	}
}

// testGit runs git in dir with a fixed identity and returns its output.
func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

func writeTestFile(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeStartsFromTheParentsUncommittedFiles(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	testGit(t, repo, "init", "-q")
	writeTestFile(t, filepath.Join(repo, "pkg", "a.txt"), "committed\n")
	writeTestFile(t, filepath.Join(repo, ".gitignore"), "*.log\n")
	testGit(t, repo, "add", "-A")
	testGit(t, repo, "commit", "-q", "-m", "init")
	writeTestFile(t, filepath.Join(repo, "pkg", "a.txt"), "edited, not staged\n")
	writeTestFile(t, filepath.Join(repo, "pkg", "staged.txt"), "staged\n")
	testGit(t, repo, "add", "pkg/staged.txt")
	writeTestFile(t, filepath.Join(repo, "new.txt"), "untracked\n")
	writeTestFile(t, filepath.Join(repo, "debug.log"), "ignored\n")
	indexBefore, statusBefore := testGit(t, repo, "diff", "--cached", "--name-status"), testGit(t, repo, "status", "--porcelain")
	c := New(nil).WithWorktrees(t.TempDir())

	tree, err := c.prepareSubagentWorktree(context.Background(), filepath.Join(repo, "pkg"), "task_base")
	if err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]string{"pkg/a.txt": "edited, not staged\n", "pkg/staged.txt": "staged\n", "new.txt": "untracked\n"} {
		if got, err := os.ReadFile(filepath.Join(tree.root, path)); err != nil || string(got) != want {
			t.Errorf("%s in the worktree = %q, %v; want %q", path, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(tree.root, "debug.log")); !os.IsNotExist(err) {
		t.Errorf("an ignored file reached the worktree: %v", err)
	}
	if tree.dir != filepath.Join(tree.root, "pkg") || !strings.HasPrefix(tree.root, c.worktrees) {
		t.Errorf("worktree = %+v", tree)
	}
	if branch := strings.TrimSpace(testGit(t, tree.root, "branch", "--show-current")); branch != "mc/task_base" {
		t.Errorf("branch = %q", branch)
	}
	if got := testGit(t, repo, "diff", "--cached", "--name-status"); got != indexBefore {
		t.Errorf("the parent's index changed: %q, was %q", got, indexBefore)
	}
	if got := testGit(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Errorf("the parent's status changed: %q, was %q", got, statusBefore)
	}
	if stash := testGit(t, repo, "stash", "list"); stash != "" {
		t.Errorf("stash = %q", stash)
	}
}
