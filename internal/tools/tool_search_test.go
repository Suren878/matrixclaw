package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSearchTool(t *testing.T, executor Executor, dir string, args any) Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), Call{WorkingDir: dir, Args: raw})
	if err != nil {
		t.Fatal(err)
	}
	return result
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
	if result.IsError() || !strings.Contains(result.Content, "Line 2, Char 1: NEEDLE text") {
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
