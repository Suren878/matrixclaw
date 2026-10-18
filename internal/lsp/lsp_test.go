package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestDiagnoseReportsErrorsOnlyAtMostTwentyAFile(t *testing.T) {
	m, dir := fakeProject(t)
	file := filepath.Join(dir, "a.go")
	var text strings.Builder
	text.WriteString("package m\nWARN: unused variable\n")
	for i := range 23 {
		fmt.Fprintf(&text, "ERROR: problem %d\n", i)
	}
	writeFile(t, file, text.String())

	report := m.Diagnose(context.Background(), []string{file})
	lines := report.Lines()
	if len(report.Checked) != 1 || len(lines) != 21 {
		t.Fatalf("checked %v, lines %q", report.Checked, lines)
	}
	if lines[0] != file+":3:1: problem 0" || lines[20] != "... and 3 more errors in "+file {
		t.Fatalf("lines %q", lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), "unused") {
		t.Fatalf("a warning was reported: %q", lines)
	}
}

func TestDiagnoseWaitsForThePushOfTheNewVersion(t *testing.T) {
	m, dir := fakeProject(t, "LSP_FAKE_DELAY=300ms")
	file := filepath.Join(dir, "a.go")
	writeFile(t, file, "package m\nERROR: old problem\n")
	if lines := m.Diagnose(context.Background(), []string{file}).Lines(); len(lines) != 1 {
		t.Fatalf("first check: %q", lines)
	}
	// The server first pushes the old text's error again, then the new one.
	writeFile(t, file, "package m\nERROR: new problem\n")
	lines := m.Diagnose(context.Background(), []string{file}).Lines()
	if len(lines) != 1 || !strings.HasSuffix(lines[0], "new problem") {
		t.Fatalf("lines %q, want only the new problem", lines)
	}
	writeFile(t, file, "package m\n")
	if report := m.Diagnose(context.Background(), []string{file}); len(report.Checked) != 1 || len(report.Lines()) != 0 {
		t.Fatalf("fixed file: checked %v, lines %q", report.Checked, report.Lines())
	}
}

func TestDiagnoseWithoutVersionsTakesThePushAfterTheChange(t *testing.T) {
	m, dir := fakeProject(t, "LSP_FAKE_NO_VERSION=1")
	file := filepath.Join(dir, "a.go")
	writeFile(t, file, "package m\n")
	m.Diagnose(context.Background(), []string{file})
	writeFile(t, file, "package m\nERROR: broken\n")
	if lines := m.Diagnose(context.Background(), []string{file}).Lines(); len(lines) != 1 {
		t.Fatalf("lines %q", lines)
	}
	// Unchanged text: the push already received answers it.
	start := time.Now()
	if report := m.Diagnose(context.Background(), []string{file}); len(report.Checked) != 1 || time.Since(start) > documentWait/2 {
		t.Fatalf("unchanged file: checked %v after %s", report.Checked, time.Since(start))
	}
}

func TestDiagnoseReportsErrorsInOtherFiles(t *testing.T) {
	m, dir := fakeProject(t)
	file := filepath.Join(dir, "a.go")
	names := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go"}
	for _, name := range names[1:] {
		writeFile(t, filepath.Join(dir, name), "package m\n")
	}
	writeFile(t, file, "package m\nERROR: here\nOTHERS: "+strings.Join(names[1:], " ")+" gone.go\n")

	report := m.Diagnose(context.Background(), []string{file})
	others := report.OtherLines()
	if len(report.Lines()) != 1 || len(others) != 7 || others[0] != "Errors in other files:" || others[6] != "... and 2 more files with errors" {
		t.Fatalf("lines %q, others %q", report.Lines(), others)
	}
	if strings.Contains(strings.Join(others, "\n"), "gone.go") || strings.Contains(strings.Join(others, "\n"), "/a.go") {
		t.Fatalf("a missing or the edited file is among the others: %q", others)
	}
	if !strings.HasSuffix(others[1], ":1:1: broken by a.go") {
		t.Fatalf("others %q", others)
	}
	// Errors this server published before the edit may be stale: not repeated.
	writeFile(t, file, "package m\n")
	if others := m.Diagnose(context.Background(), []string{file}).OtherLines(); others != nil {
		t.Fatalf("stale errors reported: %q", others)
	}
}

func TestDiagnoseLeavesFilesWithoutServerUnchecked(t *testing.T) {
	m, dir := fakeProject(t)
	plain := filepath.Join(dir, "notes.txt")
	outside := filepath.Join(t.TempDir(), "x.go") // no go.mod above it
	writeFile(t, plain, "ERROR: not code\n")
	writeFile(t, outside, "ERROR: no project\n")
	if report := m.Diagnose(context.Background(), []string{plain, outside}); len(report.Checked) != 0 {
		t.Fatalf("checked %v", report.Checked)
	}
	var none *Manager
	if report := none.Diagnose(context.Background(), []string{plain}); len(report.Checked) != 0 || none.Available(dir) {
		t.Fatal("a nil manager checked a file")
	}
}

func TestBrokenServerIsNotRetriedAndNotOffered(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module m\n")
	file := filepath.Join(dir, "a.go")
	writeFile(t, file, "package m\n")
	m := NewManager(t.TempDir(), nil, func() Config { return Config{Servers: map[string][]string{"go": {"/bin/false"}}} })
	m.logf = t.Logf
	defer m.Close()
	if !m.Available(dir) {
		t.Fatal("not offered before it failed")
	}
	if report := m.Diagnose(context.Background(), []string{file}); len(report.Checked) != 0 {
		t.Fatalf("checked %v", report.Checked)
	}
	if m.Available(dir) {
		t.Fatal("a server that failed to start is still offered")
	}
	if _, err := m.clientFor(file); !errors.Is(err, errNoServer) {
		t.Fatalf("clientFor = %v, want errNoServer", err)
	}
}

func TestAvailableNeedsAProjectAndAnInstalledServer(t *testing.T) {
	m, dir := fakeProject(t)
	if !m.Available(dir) || !m.Available(filepath.Join(dir, "sub")) {
		t.Fatal("the fake Go server is not offered in its project")
	}
	if m.Available(t.TempDir()) {
		t.Fatal("offered without a project file")
	}
	for name, cfg := range map[string]Config{
		"off":          {Disabled: true},
		"language off": {Servers: map[string][]string{"go": {}}},
		"missing":      {Servers: map[string][]string{"go": {"no-such-language-server"}}},
	} {
		m.config = func() Config { return cfg }
		if m.Available(dir) {
			t.Errorf("%s: offered", name)
		}
	}
}

func TestIdleServerStopsAndStartsAgain(t *testing.T) {
	m, dir := fakeProject(t)
	m.idle = time.Second
	file := filepath.Join(dir, "a.go")
	writeFile(t, file, "package m\nERROR: x\n")
	m.Diagnose(context.Background(), []string{file})
	m.mu.Lock()
	first := m.clients["go:"+dir]
	m.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		running := len(m.clients)
		m.mu.Unlock()
		if running == 0 && !first.proc.Running() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the idle server did not stop")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lines := m.Diagnose(context.Background(), []string{file}).Lines(); len(lines) != 1 {
		t.Fatalf("after restart: %q", lines)
	}
}

func TestCloseStopsServers(t *testing.T) {
	m, dir := fakeProject(t)
	file := filepath.Join(dir, "a.go")
	writeFile(t, file, "package m\n")
	m.Diagnose(context.Background(), []string{file})
	c, _ := m.clientFor(file)
	m.Close()
	if c.proc.Running() {
		t.Fatal("the server runs after Close")
	}
	if report := m.Diagnose(context.Background(), []string{file}); len(report.Checked) != 0 {
		t.Fatal("a closed manager checked a file")
	}
}

func TestConfigValidate(t *testing.T) {
	for cfg, want := range map[*Config]string{
		{Servers: map[string][]string{"cobol": {"x"}}}: `"cobol" is not one of`,
		{Servers: map[string][]string{"go": {" "}}}:    "servers.go: the program name is empty",
	} {
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate(%v) = %v, want %q", cfg.Servers, err, want)
		}
	}
	if err := (Config{Servers: map[string][]string{"python": {"pylsp"}, "rust": nil}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func runTool(t *testing.T, m *Manager, dir string, args map[string]any, recheck func(context.Context, permission.Subject) error) tools.Result {
	t.Helper()
	raw, _ := json.Marshal(args)
	result, err := NewTool(m).Execute(context.Background(), tools.Call{WorkingDir: dir, Args: raw, Recheck: recheck})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestToolOperations(t *testing.T) {
	m, dir := fakeProject(t)
	writeFile(t, filepath.Join(dir, "a.go"), "package m\n\ntype T struct{}\nfunc (T) M() { _ = \"é\" }\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package m\n")
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"definition":        {map[string]any{"operation": "definition", "file_path": "a.go", "line": 4, "character": 7}, filepath.Join(dir, "a.go") + ":3:6: type T struct{}"},
		"references":        {map[string]any{"operation": "references", "file_path": "a.go", "line": 3, "character": 6}, "2 references:\n" + filepath.Join(dir, "a.go") + ":3:6: type T struct{}\n" + filepath.Join(dir, "b.go") + ":1:4: package m"},
		"hover":             {map[string]any{"operation": "hover", "file_path": "a.go", "line": 4, "character": 23}, "hover at 3:22"},
		"document symbols":  {map[string]any{"operation": "document_symbols", "file_path": "a.go"}, "struct T (line 3)\n  method M (line 4) func()"},
		"workspace symbols": {map[string]any{"operation": "workspace_symbols", "file_path": "b.go", "query": "Thing"}, filepath.Join(dir, "a.go") + ":3:1 function m.Thing"},
		"no position":       {map[string]any{"operation": "hover", "file_path": "a.go"}, "hover needs line and character, both 1-based"},
		"no query":          {map[string]any{"operation": "workspace_symbols", "file_path": "a.go"}, "workspace_symbols needs a query"},
		"bad operation":     {map[string]any{"operation": "rename", "file_path": "a.go"}, "operation must be one of"},
		"unsupported":       {map[string]any{"operation": "implementation", "file_path": "a.go", "line": 1, "character": 1}, "does not support implementation"},
		"no server":         {map[string]any{"operation": "document_symbols", "file_path": "go.mod"}, "No language server serves"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := runTool(t, m, dir, tc.args, nil).Content; !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestToolLeavesOutFilesTheCallMayNotRead(t *testing.T) {
	m, dir := fakeProject(t)
	writeFile(t, filepath.Join(dir, "a.go"), "package m\n\ntype T struct{}\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package m\n")
	deny := func(_ context.Context, subject permission.Subject) error {
		if filepath.Base(subject.Value) == "b.go" {
			return errors.New("blocked")
		}
		return nil
	}
	got := runTool(t, m, dir, map[string]any{"operation": "references", "file_path": "a.go", "line": 3, "character": 6}, deny).Content
	if strings.Contains(got, "b.go") || !strings.Contains(got, "(1 results in files you may not read were left out.)") {
		t.Fatalf("got %q", got)
	}
}

func TestToolSyncsFilesChangedOnDisk(t *testing.T) {
	m, dir := fakeProject(t)
	a, b := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")
	writeFile(t, a, "package m\n")
	writeFile(t, b, "package m\n")
	m.Diagnose(context.Background(), []string{a})
	// A shell command rewrites the open a.go; checking b.go sends it again.
	writeFile(t, a, "package m\nERROR: from the shell\n")
	m.Diagnose(context.Background(), []string{b})
	c, _ := m.clientFor(a)
	c.mu.Lock()
	defer c.mu.Unlock()
	if doc := c.docs[fileURI(a)]; doc == nil || doc.version != 2 || !strings.Contains(string(doc.text), "from the shell") {
		t.Fatalf("a.go was not synced: %+v", doc)
	}
}

func TestColumnsCountCharactersNotUTF16Units(t *testing.T) {
	text := []byte("first\nx := \"😀é\" + y\n")
	// y is the 13th character; before it the emoji takes two UTF-16 units.
	if got := utf16Character(text, 1, 13); got != 13 {
		t.Fatalf("utf16Character = %d, want 13", got)
	}
	if got := column(text, Position{Line: 1, Character: 13}); got != 13 {
		t.Fatalf("column = %d, want 13", got)
	}
	if got := column(text, Position{Line: 9, Character: 4}); got != 5 {
		t.Fatalf("column past the end = %d, want 5", got)
	}
}

func TestServerCommandIsFoundOnTheEnvironmentPath(t *testing.T) {
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "pylsp"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bin, "pylsp"), 0o755); err != nil {
		t.Fatal(err)
	}
	python := languageOf("python")
	if got := python.command(Config{}, bin); len(got) != 1 || got[0] != filepath.Join(bin, "pylsp") {
		t.Fatalf("command = %q, want pylsp as the fallback", got)
	}
	if got := python.command(Config{Servers: map[string][]string{"python": {}}}, bin); got != nil {
		t.Fatalf("a language turned off has command %q", got)
	}
}
