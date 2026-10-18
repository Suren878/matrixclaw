package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoplsDiagnosticsAndNavigation(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	dir := t.TempDir()
	for name, text := range map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"a.go":   "package m\n\n// A answers.\nfunc A() int { return 1 }\n",
		"c/c.go": "package c\n\nimport \"example.com/m\"\n\nfunc C() int { return m.A() }\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, name), text)
	}
	m := NewManager(t.TempDir(), nil, func() Config { return Config{} })
	defer m.Close()
	a, c := filepath.Join(dir, "a.go"), filepath.Join(dir, "c", "c.go")
	if !m.Available(dir) {
		t.Fatal("gopls is not offered in a Go module")
	}
	if report := m.Diagnose(context.Background(), []string{a}); len(report.Checked) != 1 || len(report.Lines()) != 0 {
		t.Fatalf("clean file: %+v", report)
	}
	writeFile(t, a, "package m\n\n// A answers.\nfunc A(x int) int { return x }\n")
	if others := m.Diagnose(context.Background(), []string{a}).OtherLines(); len(others) < 2 || !strings.HasPrefix(others[1], c+":5:27: not enough arguments in call to m.A") {
		t.Fatalf("the broken caller in another package was not reported: %q", others)
	}
	writeFile(t, a, "package m\n\n// A answers.\nfunc A() int { return 1 }\n")
	if others := m.Diagnose(context.Background(), []string{a}).OtherLines(); others != nil {
		t.Fatalf("fixed callers still reported: %q", others)
	}

	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"definition": {map[string]any{"operation": "definition", "file_path": c, "line": 5, "character": 26}, a + ":4:6: func A() int { return 1 }"},
		"references": {map[string]any{"operation": "references", "file_path": a, "line": 4, "character": 6}, c + ":5:25: func C() int { return m.A() }"},
		"hover":      {map[string]any{"operation": "hover", "file_path": c, "line": 5, "character": 26}, "func m.A() int\nA answers."},
		"symbols":    {map[string]any{"operation": "document_symbols", "file_path": c}, "function C (line 5)"},
		"workspace":  {map[string]any{"operation": "workspace_symbols", "file_path": a, "query": "C"}, c + ":5:6 function example.com/m/c.C"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := runTool(t, m, dir, tc.args, nil).Content; !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
