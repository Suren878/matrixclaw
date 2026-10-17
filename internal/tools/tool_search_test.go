package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func runSearchTool(t *testing.T, executor Executor, dir string, args any) Result {
	t.Helper()
	return runSearchCall(t, executor, Call{WorkingDir: dir}, args)
}

func runSearchCall(t *testing.T, executor Executor, call Call, args any) Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	call.Args = raw
	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// writeTree creates files (path → content) below dir, each modified a second
// after the one before in the order given.
func writeTree(t *testing.T, dir string, files ...string) {
	t.Helper()
	base := time.Now().Add(-time.Hour)
	for i := 0; i+1 < len(files); i += 2 {
		path := filepath.Join(dir, filepath.FromSlash(files[i]))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(files[i+1]), 0o644); err != nil {
			t.Fatal(err)
		}
		stamp := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
}

// resultPaths are the lines of a result that name a path below dir, relative to it.
func resultPaths(dir string, content string) []string {
	var paths []string
	for line := range strings.SplitSeq(content, "\n") {
		if rel, ok := strings.CutPrefix(line, filepath.ToSlash(dir)+"/"); ok {
			paths = append(paths, rel)
		}
	}
	return paths
}

func TestGrepSkipsHugeAndBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	huge := append([]byte(strings.Repeat("x", MaxReadBytes)), []byte("\nNEEDLE huge\n")...)
	files := map[string][]byte{
		"huge.log":   huge,
		"blob.bin":   []byte("\x00\x01NEEDLE binary\n"),
		"source.txt": []byte("first\nNEEDLE text\n"),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "NEEDLE", Path: dir})
	if result.IsError() || !strings.Contains(result.Content, "  2: NEEDLE text") {
		t.Fatalf("result = %+v, want the text match", result)
	}
	if strings.Contains(result.Content, "huge") || strings.Contains(result.Content, "binary") {
		t.Fatalf("content = %q, want huge and binary files skipped", result.Content)
	}
}

func TestSearchToolsSkipUnreadableSubdirectories(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "open.txt"), []byte("NEEDLE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("this user reads every directory")
	}

	if result := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "NEEDLE", Path: dir}); result.IsError() || !strings.Contains(result.Content, "open.txt") {
		t.Fatalf("grep = %+v, want the readable match", result)
	}
	if result := runSearchTool(t, NewGlobExecutor(), dir, GlobParams{Pattern: "*.txt", Path: dir}); result.IsError() || !strings.Contains(result.Content, "open.txt") {
		t.Fatalf("glob = %+v, want the readable match", result)
	}
	if result := runSearchTool(t, NewLSExecutor(), dir, LSParams{Path: dir}); result.IsError() || !strings.Contains(result.Content, "open.txt") {
		t.Fatalf("ls = %+v, want the readable entries", result)
	}
}

func TestGlobMatchesDoubleStarBracesAndNamedHiddenEntries(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir,
		"main.go", "", "cmd/app/main.go", "", "src/a.ts", "", "src/x/b.tsx", "",
		".github/workflows/ci.yml", "", "README.md", "")
	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		{"**/*.go", []string{"cmd/app/main.go", "main.go"}},
		{"*.go", []string{"main.go"}},
		{"src/**/*.ts", []string{"src/a.ts"}},
		{"src/**/*.{ts,tsx}", []string{"src/x/b.tsx", "src/a.ts"}},
		{"[A-Z]*.md", []string{"README.md"}},
		{".github/**", []string{".github/workflows/ci.yml"}},
		{"**/*.yml", nil},
	} {
		result := runSearchTool(t, NewGlobExecutor(), dir, GlobParams{Pattern: tc.pattern})
		if got := resultPaths(dir, result.Content); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("glob %q = %v, want %v (%q)", tc.pattern, got, tc.want, result.Content)
		}
	}
	if result := runSearchTool(t, NewGlobExecutor(), dir, GlobParams{Pattern: "../**"}); !result.IsError() {
		t.Errorf("a pattern leaving path ran: %q", result.Content)
	}
}

func TestGlobShowsTheMostRecentFilesAndCounts(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := range globMatchLimit + 5 {
		files = append(files, fmt.Sprintf("f%03d.txt", i), "")
	}
	writeTree(t, dir, files...)
	result := runSearchTool(t, NewGlobExecutor(), dir, GlobParams{Pattern: "*.txt"})
	paths := resultPaths(dir, result.Content)
	if len(paths) != globMatchLimit || paths[0] != "f104.txt" || paths[len(paths)-1] != "f005.txt" {
		t.Fatalf("paths = %d, first %v", len(paths), paths[:3])
	}
	if !strings.Contains(result.Content, "of 105 files") {
		t.Fatalf("content = %q, want the total", result.Content)
	}
}

func TestGrepIncludeMatchesNamesInSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir,
		"root.go", "NEEDLE", "internal/deep/x.go", "NEEDLE", "internal/deep/x.txt", "NEEDLE",
		"src/a.ts", "NEEDLE", "lib/b.ts", "NEEDLE", ".hidden/c.go", "NEEDLE")
	for _, tc := range []struct {
		include string
		want    []string
	}{
		{"*.go", []string{"internal/deep/x.go", "root.go"}},
		{"src/**/*.ts", []string{"src/a.ts"}},
		{"*.{ts,txt}", []string{"lib/b.ts", "src/a.ts", "internal/deep/x.txt"}},
		{".hidden/*.go", []string{".hidden/c.go"}},
	} {
		result := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "NEEDLE", Include: tc.include, OutputMode: grepFilesWithMatches})
		if got := resultPaths(dir, result.Content); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("include %q = %v, want %v (%q)", tc.include, got, tc.want, result.Content)
		}
	}
}

func TestGrepModesContextAndPaging(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir,
		"old.txt", "one hit\n",
		"new.txt", "a\nhit 1\nb\nc\nd\nhit 2\nhit 3\ne\n")
	grep := func(params GrepParams) string {
		params.Pattern, params.Path = "hit", dir
		result := runSearchTool(t, NewGrepExecutor(), dir, params)
		if result.IsError() {
			t.Fatalf("%+v: %q", params, result.Content)
		}
		return strings.ReplaceAll(result.Content, filepath.ToSlash(dir)+"/", "")
	}

	if got, want := grep(GrepParams{}), "Found 4 matches in 2 files\nnew.txt\n  2: hit 1\n  6: hit 2\n  7: hit 3\nold.txt\n  1: one hit"; got != want {
		t.Errorf("content =\n%s\nwant\n%s", got, want)
	}
	if got, want := grep(GrepParams{Context: 1, Limit: 2}), "Found 4 matches in 2 files\nnew.txt\n  1- a\n  2: hit 1\n  3- b\n  --\n  5- d\n  6: hit 2\n  7: hit 3\n\n(Showing matches 1-2 of 4, most recently modified files first. Continue with offset 2, or narrow path or include.)"; got != want {
		t.Errorf("context =\n%s\nwant\n%s", got, want)
	}
	if got, want := grep(GrepParams{Offset: 2, Limit: 1}), "Found 4 matches in 2 files\nnew.txt\n  7: hit 3\n\n(Showing matches 3-3 of 4, most recently modified files first. Continue with offset 3, or narrow path or include.)"; got != want {
		t.Errorf("offset =\n%s\nwant\n%s", got, want)
	}
	if got, want := grep(GrepParams{OutputMode: grepCount}), "Found 4 matches in 2 files\nnew.txt: 3\nold.txt: 1"; got != want {
		t.Errorf("count =\n%s\nwant\n%s", got, want)
	}
	if got, want := grep(GrepParams{OutputMode: grepFilesWithMatches, Limit: 1}), "Found 2 files\nnew.txt\n\n(Showing files 1-1 of 2, most recently modified files first. Continue with offset 1, or narrow path or include.)"; got != want {
		t.Errorf("files =\n%s\nwant\n%s", got, want)
	}
	result := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "HIT 1", CaseInsensitive: true, Path: filepath.Join(dir, "new.txt")})
	if !strings.Contains(result.Content, "  2: hit 1") || strings.Contains(result.Content, "old.txt") {
		t.Errorf("case-insensitive search of one file = %q", result.Content)
	}
}

func TestGrepCutsLongLinesOnARuneBoundary(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "wide.txt", "x"+strings.Repeat("é", maxRenderedLineWidth))
	result := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "x"})
	if !utf8.ValidString(result.Content) || !strings.Contains(result.Content, "é...") || len(result.Content) > 2*maxRenderedLineWidth {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestSearchesFollowGitignoreAndSkipHeavyDirectories(t *testing.T) {
	check := func(t *testing.T, dir string) {
		t.Helper()
		grep := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "NEEDLE", OutputMode: grepFilesWithMatches})
		if got := resultPaths(dir, grep.Content); strings.Join(got, ",") != "kept.log,src/keep.go" {
			t.Errorf("grep = %v (%q)", got, grep.Content)
		}
		glob := runSearchTool(t, NewGlobExecutor(), dir, GlobParams{Pattern: "**/*.go"})
		if got := resultPaths(dir, glob.Content); strings.Join(got, ",") != "src/keep.go" {
			t.Errorf("glob = %v (%q)", got, glob.Content)
		}
		named := runSearchTool(t, NewGlobExecutor(), dir, GlobParams{Pattern: "node_modules/**/*.js"})
		if got := resultPaths(dir, named.Content); strings.Join(got, ",") != "node_modules/pkg/index.js" {
			t.Errorf("glob into node_modules = %v (%q)", got, named.Content)
		}
		inside := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "NEEDLE", Path: "build"})
		if !strings.Contains(inside.Content, "build/out.go") {
			t.Errorf("grep inside an ignored directory = %q", inside.Content)
		}
		ls := runSearchTool(t, NewLSExecutor(), dir, LSParams{})
		for _, hidden := range []string{"node_modules", "build", "debug.log", "secret.go"} {
			if strings.Contains(ls.Content, hidden) {
				t.Errorf("ls shows %s:\n%s", hidden, ls.Content)
			}
		}
		if !strings.Contains(ls.Content, "src/\n") || !strings.Contains(ls.Content, "keep.go") {
			t.Errorf("ls =\n%s", ls.Content)
		}
	}
	tree := []string{
		".gitignore", "build/\n*.log\n!kept.log\nsrc/secret.go\n",
		"src/keep.go", "NEEDLE", "src/secret.go", "NEEDLE", "build/out.go", "NEEDLE",
		"debug.log", "NEEDLE", "kept.log", "NEEDLE", "node_modules/pkg/index.js", "NEEDLE",
	}

	t.Run("walk", func(t *testing.T) {
		dir := t.TempDir()
		writeTree(t, dir, tree...)
		check(t, dir)
	})
	t.Run("git", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("no git")
		}
		dir := t.TempDir()
		writeTree(t, dir, append(tree, ".gitignore", "build/\n*.log\n!kept.log\nsrc/secret.go\nnode_modules/\n")...)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		check(t, dir)
	})
}

func TestSearchesLeaveOutSkippedPaths(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "notes.txt", "NEEDLE", "secret/key.txt", "NEEDLE", "app/.env", "NEEDLE")
	skip := func(path string) bool {
		return path == filepath.Join(dir, "secret") || strings.HasSuffix(path, "/.env")
	}
	call := Call{WorkingDir: dir, Skip: skip}
	grep := runSearchCall(t, NewGrepExecutor(), call, GrepParams{Pattern: "NEEDLE", Include: "{*.txt,.env}"})
	if got := resultPaths(dir, grep.Content); strings.Join(got, ",") != "notes.txt" || !strings.Contains(grep.Content, "2 entries left out") {
		t.Errorf("grep = %q", grep.Content)
	}
	glob := runSearchCall(t, NewGlobExecutor(), call, GlobParams{Pattern: "secret/*"})
	if got := resultPaths(dir, glob.Content); len(got) != 0 {
		t.Errorf("glob = %q", glob.Content)
	}
	ls := runSearchCall(t, NewLSExecutor(), call, LSParams{})
	if strings.Contains(ls.Content, "secret") || !strings.Contains(ls.Content, "notes.txt") {
		t.Errorf("ls = %q", ls.Content)
	}
}

func TestLSMarksDirectoriesAndDescribesFiles(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "a/b/c.txt", "", "a/skip.log", "", "z.txt", "12345", "testdata/x", "")
	result := runSearchTool(t, NewLSExecutor(), dir, LSParams{Ignore: []string{"*.log", "testdata"}})
	want := filepath.ToSlash(dir) + "/\n├── a/\n│   └── b/\n│       └── c.txt\n└── z.txt"
	if result.Content != want {
		t.Errorf("ls =\n%s\nwant\n%s", result.Content, want)
	}
	if shallow := runSearchTool(t, NewLSExecutor(), dir, LSParams{Depth: 1}); strings.Contains(shallow.Content, "b/") {
		t.Errorf("depth 1 =\n%s", shallow.Content)
	}
	file := runSearchTool(t, NewLSExecutor(), dir, LSParams{Path: "z.txt"})
	if file.IsError() || !strings.Contains(file.Content, "z.txt (file, 5 bytes)") {
		t.Errorf("ls of a file = %+v", file)
	}
}
