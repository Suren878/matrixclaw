package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPagesAFileWithinTheOutputBudget(t *testing.T) {
	s := newSessionTools(t)
	var body strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&body, "line %04d %s\n", i+1, strings.Repeat("x", 60))
	}
	if err := os.WriteFile(filepath.Join(s.dir, "long.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	offset, pages := 0, 0
	for {
		result := s.run("read", ReadParams{FilePath: "long.txt", Offset: offset})
		meta, ok := result.Metadata.(ReadResponseMetadata)
		if result.IsError() || !ok || meta.TotalLines != 5000 || meta.StartLine != offset+1 {
			t.Fatalf("page at %d = %+v", offset, result)
		}
		if len(result.Content) > readPageBytes+300 {
			t.Fatalf("page at %d is %d bytes", offset, len(result.Content))
		}
		if !strings.Contains(result.Content, fmt.Sprintf("%6d\tline %04d ", offset+1, offset+1)) {
			t.Fatalf("page at %d starts wrong:\n%s", offset, result.Content[:200])
		}
		pages++
		if meta.EndLine == 5000 {
			break
		}
		if !strings.Contains(result.Content, fmt.Sprintf("of 5000. To read on, call read with offset %d.", meta.EndLine)) {
			t.Fatalf("page at %d has no continuation:\n%s", offset, result.Content[len(result.Content)-200:])
		}
		offset = meta.EndLine
	}
	if pages < 10 {
		t.Fatalf("pages = %d, want the budget to split the file", pages)
	}
	if result := s.run("read", ReadParams{FilePath: "long.txt", Offset: 10, Limit: 5}); !strings.Contains(result.Content, "    15\t") || strings.Contains(result.Content, "    16\t") {
		t.Fatalf("limit = %s", result.Content)
	}
}

func TestReadEdgeCases(t *testing.T) {
	s := newSessionTools(t)
	files := map[string]string{
		"crlf.txt":  "one\r\ntwo\r\n",
		"wide.txt":  strings.Repeat("ж", 3*maxReadLineRunes) + "\nshort\n",
		"blob.bin":  "\x89PNG\r\n\x1a\n\x00\x00",
		"empty.txt": "",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(s.dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if result := s.run("read", ReadParams{FilePath: "crlf.txt"}); result.Content != "<file>\n     1\tone\n     2\ttwo\n</file>" {
		t.Fatalf("crlf = %q", result.Content)
	}
	result := s.run("read", ReadParams{FilePath: "wide.txt"})
	if result.IsError() || !strings.Contains(result.Content, fmt.Sprintf("[line cut: %d of %d characters shown]", maxReadLineRunes, 3*maxReadLineRunes)) || !strings.Contains(result.Content, "     2\tshort") {
		t.Fatalf("wide = %q", result.Content[len(result.Content)-300:])
	}
	for _, tc := range []struct {
		params ReadParams
		want   string
	}{
		{ReadParams{FilePath: "blob.bin"}, "binary file (image/png"},
		{ReadParams{FilePath: "crlf.txt", Offset: 2}, "has 2 lines; offset 2 skips all of them"},
		{ReadParams{FilePath: "crlf.txt", Offset: -1}, "cannot be negative"},
		{ReadParams{FilePath: "missing.txt"}, "File not found"},
		{ReadParams{FilePath: "."}, "is a directory"},
	} {
		if result := s.run("read", tc.params); !result.IsError() || !strings.Contains(result.Content, tc.want) {
			t.Errorf("%+v: result = %q, want %q", tc.params, result.Content, tc.want)
		}
	}
	if result := s.run("read", ReadParams{FilePath: "empty.txt"}); result.IsError() || !strings.HasSuffix(result.Content, "is empty.") {
		t.Fatalf("empty = %+v", result)
	}
}
