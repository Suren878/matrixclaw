package checkpoint

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	s := New(t.TempDir())
	s.home = filepath.Join(t.TempDir(), "home")
	return s
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(dir, name string) bool {
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
	return err == nil
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func paths(changes []Change) []string {
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		out = append(out, string(change.Status)+" "+change.Path)
	}
	slices.Sort(out)
	return out
}

func TestRestoreBringsBackAPlainDirectory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	write(t, dir, "keep.txt", "keep\n")
	write(t, dir, "edit.txt", "one\n")
	write(t, dir, "gone.txt", "gone\n")
	tree, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	write(t, dir, "edit.txt", "two\n")
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "new/deep/file.txt", "new\n")

	diff, err := s.Diff(ctx, dir, tree, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(diff.Changes), []string{"added new/deep/file.txt", "deleted gone.txt", "modified edit.txt"}; !slices.Equal(got, want) {
		t.Fatalf("diff = %v, want %v", got, want)
	}
	if !strings.Contains(diff.Patch, "+two") {
		t.Fatalf("patch misses the change:\n%s", diff.Patch)
	}

	restored, err := s.Restore(ctx, dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(restored), []string{"added gone.txt", "deleted new/deep/file.txt", "modified edit.txt"}; !slices.Equal(got, want) {
		t.Fatalf("restore = %v, want %v", got, want)
	}
	if read(t, dir, "edit.txt") != "one\n" || read(t, dir, "gone.txt") != "gone\n" || read(t, dir, "keep.txt") != "keep\n" {
		t.Fatal("files were not restored")
	}
	if exists(dir, "new") {
		t.Fatal("added file and its empty directories should be gone")
	}
	diff, err = s.Diff(ctx, dir, tree, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Changes) != 0 {
		t.Fatalf("after restore diff = %v", paths(diff.Changes))
	}
}

func TestSnapshotKeepsTheUsersRepositoryUntouched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	write(t, dir, ".gitignore", "*.log\nbuild/\n")
	write(t, dir, "main.go", "package main\n")
	gitRun(t, dir, "add", ".gitignore", "main.go")
	gitRun(t, dir, "commit", "-qm", "init")
	write(t, dir, ".git/info/exclude", "secret.txt\n")
	write(t, dir, "debug.log", "log\n")
	write(t, dir, "build/out", "bin\n")
	write(t, dir, "secret.txt", "s\n")
	write(t, dir, "wip.go", "package main\n")
	statusBefore := gitRun(t, dir, "status", "--porcelain")
	headBefore := gitRun(t, dir, "rev-parse", "HEAD")

	tree, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "main.go", "package main // changed\n")
	write(t, dir, "debug.log", "more\n")
	if _, err := s.Restore(ctx, dir, tree); err != nil {
		t.Fatal(err)
	}

	if got := gitRun(t, dir, "status", "--porcelain"); got != statusBefore {
		t.Fatalf("user's status changed:\n%s\nwant\n%s", got, statusBefore)
	}
	if got := gitRun(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Fatal("user's history changed")
	}
	if read(t, dir, "main.go") != "package main\n" {
		t.Fatal("tracked file was not restored")
	}
	if read(t, dir, "debug.log") != "more\n" || !exists(dir, "build/out") || !exists(dir, "secret.txt") {
		t.Fatal("ignored files must be left alone")
	}
	cmd := exec.Command("git", "cat-file", "-e", tree)
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	if cmd.Run() == nil {
		t.Fatal("the user's repository must not hold the snapshot")
	}
}

func TestSubdirectoryFollowsTheRepositoryIgnoreRules(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	top := t.TempDir()
	gitRun(t, top, "init", "-q")
	write(t, top, ".gitignore", "node_modules/\n")
	write(t, top, "pkg/a.txt", "a\n")
	write(t, top, "pkg/node_modules/dep.js", "dep\n")
	write(t, top, "other/b.txt", "b\n")
	dir := filepath.Join(top, "pkg")

	tree, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "changed\n")
	write(t, dir, "node_modules/dep.js", "changed\n")
	write(t, top, "other/b.txt", "changed\n")
	diff, err := s.Diff(ctx, dir, tree, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(diff.Changes), []string{"modified a.txt"}; !slices.Equal(got, want) {
		t.Fatalf("diff = %v, want %v", got, want)
	}
	if _, err := s.Restore(ctx, dir, tree); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "a.txt") != "a\n" || read(t, top, "other/b.txt") != "changed\n" || read(t, dir, "node_modules/dep.js") != "changed\n" {
		t.Fatal("restore must stay inside the working directory and skip ignored files")
	}
}

func TestNestedRepositoriesAreLeftOut(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	write(t, dir, "top.txt", "top\n")
	empty := filepath.Join(dir, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, empty, "init", "-q")
	write(t, empty, "f.txt", "f\n")
	vendored := filepath.Join(dir, "vendored")
	if err := os.MkdirAll(vendored, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, vendored, "init", "-q")
	write(t, vendored, "g.txt", "g\n")
	gitRun(t, vendored, "add", "g.txt")
	gitRun(t, vendored, "commit", "-qm", "g")

	tree, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, empty, "f.txt", "changed\n")
	write(t, dir, "top.txt", "changed\n")
	restored, err := s.Restore(ctx, dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(restored), []string{"modified top.txt"}; !slices.Equal(got, want) {
		t.Fatalf("restore = %v, want %v", got, want)
	}
	if read(t, empty, "f.txt") != "changed\n" {
		t.Fatal("a nested repository's files are its own")
	}
}

func TestDiffLimitsToPaths(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	write(t, dir, "a.txt", "a\n")
	write(t, dir, "b.txt", "b\n")
	tree, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "a2\n")
	write(t, dir, "b.txt", "b2\n")
	diff, err := s.Diff(ctx, dir, tree, "", filepath.Join(dir, "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(diff.Changes), []string{"modified b.txt"}; !slices.Equal(got, want) {
		t.Fatalf("diff = %v, want %v", got, want)
	}
	if diff.Changes[0].Added != 1 || diff.Changes[0].Deleted != 1 {
		t.Fatalf("stat = %+v", diff.Changes[0])
	}
	if _, err := s.Diff(ctx, dir, tree, "", "../elsewhere"); err == nil {
		t.Fatal("a path outside the working directory must be refused")
	}
}

func TestUnsupportedDirectories(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(s.home, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/", s.home, filepath.Dir(s.home), filepath.Join(s.home, "Downloads")} {
		if _, err := s.Snapshot(ctx, dir); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Snapshot(%s) = %v, want ErrUnsupported", dir, err)
		}
	}
	project := filepath.Join(s.home, "project")
	write(t, project, "a.txt", "a\n")
	if _, err := s.Snapshot(ctx, project); err != nil {
		t.Fatalf("a project inside home is supported: %v", err)
	}

	s.maxFiles = 2
	big := t.TempDir()
	for _, name := range []string{"1", "2", "3"} {
		write(t, big, name, name)
	}
	if _, err := s.Snapshot(ctx, big); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Snapshot of a directory over the file cap = %v, want ErrUnsupported", err)
	}
}

func TestPruneDropsUnkeptSnapshots(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	write(t, dir, "a.txt", "1\n")
	now := time.Now().Add(-2 * refGrace)
	s.now = func() time.Time { return now }
	first, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "2\n")
	second, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * refGrace)
	if err := s.Prune(ctx, dir, []string{second}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Diff(ctx, dir, first, ""); !errors.Is(err, ErrMissing) {
		t.Fatalf("Diff from a pruned snapshot = %v, want ErrMissing", err)
	}
	if _, err := s.Diff(ctx, dir, second, ""); err != nil {
		t.Fatalf("a kept snapshot must survive: %v", err)
	}
}

func TestRestoreSwapsAFileAndADirectory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	write(t, dir, "x", "file\n")
	tree, err := s.Snapshot(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "x")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "x/y", "nested\n")
	if _, err := s.Restore(ctx, dir, tree); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "x") != "file\n" {
		t.Fatal("x should be a file again")
	}
}
