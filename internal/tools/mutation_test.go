package tools

import (
	"context"
	"encoding/json"
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
	result, err := NewRegistry(CoreExecutors()...).Execute(context.Background(), tool, Call{WorkingDir: dir, Approved: true, Args: raw})
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
		{EditParams{FilePath: path, OldString: "1", NewString: "2"}, "appears multiple times"},
		{EditParams{FilePath: path, OldString: "c :=", NewString: "d :="}, "old_string not found"},
		{EditParams{FilePath: filepath.Join(dir, "missing.go"), OldString: "a", NewString: "b"}, "file not found"},
		{EditParams{OldString: "a", NewString: "b"}, "file_path is required"},
	} {
		if result := runMutation(t, dir, "edit", tc.params); !result.IsError() || !strings.Contains(result.Content, tc.want) {
			t.Fatalf("%+v: result = %+v, want %q", tc.params, result, tc.want)
		}
	}
	result := runMutation(t, dir, "edit", EditParams{FilePath: "a.go", OldString: "a := 1", NewString: "a := 2"})
	if result.IsError() || result.Content != "File edited: "+path || readFile(t, path) != "a := 2\nb := 1\n" {
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
	if result.IsError() || result.Content != "Applied 2 edits to "+path || readFile(t, path) != "delta\n" {
		t.Fatalf("result = %+v, file = %q", result, readFile(t, path))
	}
	if result := runMutation(t, dir, "multiedit", MultiEditParams{FilePath: path}); !result.IsError() || result.Content != "edits is required" {
		t.Fatalf("no edits: %+v", result)
	}
}

func TestMutationRejectsMalformedArguments(t *testing.T) {
	result, err := NewRegistry(CoreExecutors()...).Execute(context.Background(), "edit", Call{WorkingDir: t.TempDir(), Approved: true, Args: json.RawMessage(`{"file_path":3}`)})
	if err != nil || !result.IsError() || !strings.Contains(result.Content, "Invalid edit arguments") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestCutPreviewKeepsTheLimitAndValidUTF8(t *testing.T) {
	if got := cutPreview("short"); got != "short" {
		t.Fatalf("short = %q", got)
	}
	long := strings.Repeat("ж", approvalPreviewMaxBytes)
	got := cutPreview(long)
	if len(got) > approvalPreviewMaxBytes || !strings.Contains(got, "approval preview truncated") || !strings.HasPrefix(got, "жж") {
		t.Fatalf("cut = %d bytes, tail %q", len(got), got[len(got)-80:])
	}
	if !utf8.ValidString(got) {
		t.Fatal("cut splits a rune")
	}
}
