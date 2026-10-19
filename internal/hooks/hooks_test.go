package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/lsp"
)

// testRunner runs hooks with the daemon's environment and keeps what it logs.
func testRunner(t *testing.T, cfg Config) (*Runner, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var logged []string
	runner := NewRunner(nil, func() Config { return cfg }, filepath.Join(t.TempDir(), "hooks-trust.json"), nil)
	runner.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	return runner, &logged
}

func testScope(t *testing.T) Scope {
	return Scope{SessionID: "session_1", RunID: "run_1", WorkingDir: t.TempDir(), PermissionMode: "default"}
}

func TestPreToolUseReadsTheCallAndDecides(t *testing.T) {
	for name, tc := range map[string]struct {
		command    string
		permission string
		reason     string
	}{
		"json deny": {
			command:    `case "$(cat)" in *'"tool_name":"bash"'*'"tool_input":{"command":"git push"}'*) echo '{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"no pushes"}}';; esac`,
			permission: Deny, reason: "no pushes",
		},
		"json ask": {
			command:    `echo '{"hookSpecificOutput":{"permissionDecision":"ask","permissionDecisionReason":"check first"}}'`,
			permission: Ask, reason: "check first",
		},
		"json allow": {command: `echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`, permission: Allow},
		"legacy block": {
			command:    `echo '{"decision":"block","reason":"not here"}'`,
			permission: Deny, reason: "not here",
		},
		"exit 2 blocks with stderr": {command: `echo "pushes are off" >&2; exit 2`, permission: Deny, reason: "pushes are off"},
		"exit 0 plain text":         {command: `echo fine`},
		"other exit is a warning":   {command: `echo broken >&2; exit 1`},
	} {
		t.Run(name, func(t *testing.T) {
			runner, _ := testRunner(t, Config{Hooks: []Hook{{Event: PreToolUse, Matcher: "bash", Command: tc.command}}})
			decision := runner.PreToolUse(context.Background(), testScope(t), ToolCall{ID: "call_1", Name: "bash", Input: json.RawMessage(`{"command": "git push"}`)})
			if decision.Permission != tc.permission || decision.Reason != tc.reason {
				t.Fatalf("decision = %+v, want %s %q", decision, tc.permission, tc.reason)
			}
		})
	}
}

func TestPreToolUseHooksRewriteInputInTurnAndAddContext(t *testing.T) {
	runner, _ := testRunner(t, Config{Hooks: []Hook{
		{Event: PreToolUse, Command: `echo '{"hookSpecificOutput":{"updatedInput":{"command":"ls -la"},"additionalContext":"listing in long form"}}'`},
		{Event: PreToolUse, Command: `case "$(cat)" in *'"command":"ls -la"'*) echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}';; esac`},
	}})
	decision := runner.PreToolUse(context.Background(), testScope(t), ToolCall{Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)})
	if decision.Permission != Allow || string(decision.Input) != `{"command":"ls -la"}` || decision.Context != "listing in long form" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMatchersSelectTheHooksThatRun(t *testing.T) {
	for _, tc := range []struct {
		matcher string
		tool    string
		runs    bool
	}{
		{"", "bash", true},
		{"*", "read", true},
		{"bash", "bash", true},
		{"bash", "bash_output", false},
		{"edit|write", "write", true},
		{"edit|write", "multiedit", false},
		{"mcp\\..*", "mcp.files.read", true},
		{".*edit", "multiedit", true},
		{".*edit", "edit_notes", false},
	} {
		runner, _ := testRunner(t, Config{Hooks: []Hook{{Event: PreToolUse, Matcher: tc.matcher, Command: `echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`}}})
		decision := runner.PreToolUse(context.Background(), testScope(t), ToolCall{Name: tc.tool, Input: json.RawMessage(`{}`)})
		if ran := decision.Permission == Allow; ran != tc.runs {
			t.Errorf("matcher %q on %s ran = %v, want %v", tc.matcher, tc.tool, ran, tc.runs)
		}
	}
}

func TestHookTimeoutIsAWarning(t *testing.T) {
	runner, logged := testRunner(t, Config{Hooks: []Hook{{Event: PreToolUse, Command: `sleep 30 & wait`, Timeout: 1}}})
	started := time.Now()
	decision := runner.PreToolUse(context.Background(), testScope(t), ToolCall{Name: "bash", Input: json.RawMessage(`{}`)})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the hook ran %s past its timeout", elapsed)
	}
	if decision.Permission != "" || len(*logged) != 1 || !strings.Contains((*logged)[0], "timed out after 1s") {
		t.Fatalf("decision = %+v, logged = %q", decision, *logged)
	}
}

func TestHooksGetTheSessionEnvironment(t *testing.T) {
	scope := testScope(t)
	runner, _ := testRunner(t, Config{Hooks: []Hook{{Event: PostToolUse, Command: `echo "$MATRIXCLAW_SESSION_ID $MATRIXCLAW_TOOL $MATRIXCLAW_FILE $CLAUDE_PROJECT_DIR $(pwd)"`}}})
	told := runner.PostToolUse(context.Background(), scope, ToolCall{Name: "read", Input: json.RawMessage(`{}`), Files: []string{"/src/a.txt"}}, ToolResponse{Content: "text", Status: "success"})
	want := fmt.Sprintf("session_1 read /src/a.txt %s %s", scope.WorkingDir, scope.WorkingDir)
	if !strings.Contains(told, want) {
		t.Fatalf("told = %q, want it to hold %q", told, want)
	}
}

func TestPostToolUseTellsTheModel(t *testing.T) {
	for name, tc := range map[string]struct{ command, want string }{
		"plain stdout": {`echo formatted`, "formatted"},
		"json":         {`echo '{"decision":"block","reason":"lint failed","hookSpecificOutput":{"additionalContext":"see make lint"}}'`, "lint failed"},
		"exit 2":       {`echo "vet failed" >&2; exit 2`, "vet failed"},
		"other exit":   {`echo quiet; exit 1`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			runner, _ := testRunner(t, Config{Hooks: []Hook{{Event: PostToolUse, Command: tc.command}}})
			told := runner.PostToolUse(context.Background(), testScope(t), ToolCall{Name: "bash", Input: json.RawMessage(`{}`)}, ToolResponse{Content: "ok", Status: "success"})
			if tc.want == "" && told != "" || !strings.Contains(told, tc.want) {
				t.Fatalf("told = %q, want %q", told, tc.want)
			}
		})
	}
}

func TestStopHooksBlockWithExit2OrDecision(t *testing.T) {
	for name, tc := range map[string]struct {
		commands []string
		want     string
	}{
		"exit 2":         {[]string{`echo "tests fail" >&2; exit 2`}, "tests fail"},
		"decision block": {[]string{`grep -q '"stop_hook_active":false' && echo '{"decision":"block","reason":"run the linter"}'`}, "run the linter"},
		"exit 0":         {[]string{`exit 0`}, ""},
		"continue false": {[]string{`echo "tests fail" >&2; exit 2`, `echo '{"continue":false}'`}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var hooks []Hook
			for _, command := range tc.commands {
				hooks = append(hooks, Hook{Event: Stop, Command: command})
			}
			runner, _ := testRunner(t, Config{Hooks: hooks})
			if got := runner.Stop(context.Background(), testScope(t), Stop, false, "Done."); got != tc.want {
				t.Fatalf("Stop = %q, want %q", got, tc.want)
			}
			if got := runner.Stop(context.Background(), testScope(t), SubagentStop, false, "Done."); got != "" {
				t.Fatalf("a Stop hook ran for SubagentStop: %q", got)
			}
		})
	}
}

func TestProjectHooksRunOnlyOnceTrusted(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeProject := func(text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(project, ".matrixclaw"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, ProjectFile), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeProject(`{"hooks": [{"event": "SessionStart", "command": "echo project context"}]}`)
	sub := filepath.Join(project, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	runner, logged := testRunner(t, Config{})
	scope := Scope{SessionID: "session_1", WorkingDir: sub}

	listing := runner.List(sub)
	if listing.Project == nil || listing.Project.Trusted || listing.Project.Dir != project || len(listing.Project.Hooks) != 1 {
		t.Fatalf("listing = %+v", listing.Project)
	}
	if got := runner.SessionStart(context.Background(), scope, "startup"); got != "" || len(*logged) != 1 {
		t.Fatalf("untrusted hooks ran: %q, logged %q", got, *logged)
	}
	if _, err := runner.Trust(sub, "0000"); !errors.Is(err, ErrProjectChanged) {
		t.Fatalf("trust with a stale sum: %v", err)
	}
	if _, err := runner.Trust(sub, listing.Project.SHA256); err != nil {
		t.Fatal(err)
	}
	if got := runner.SessionStart(context.Background(), scope, "startup"); got != "project context" {
		t.Fatalf("trusted hooks said %q", got)
	}
	writeProject(`{"hooks": [{"event": "SessionStart", "command": "echo changed"}]}`)
	if got := runner.SessionStart(context.Background(), scope, "startup"); got != "" || runner.List(sub).Project.Trusted {
		t.Fatalf("a changed file kept its trust: %q", got)
	}
	writeProject(`{"hooks": [{"event": "SessionStart", "command": "echo x", "when": "always"}]}`)
	if listing := runner.List(sub); listing.Project.Error == "" {
		t.Fatalf("an unknown key was accepted: %+v", listing.Project)
	}
}

func TestHookValidation(t *testing.T) {
	for _, tc := range []struct {
		hook Hook
		want string
	}{
		{Hook{Event: "PostToolUSE", Command: "x"}, `event is "PostToolUSE"`},
		{Hook{Event: PreToolUse}, "command is empty"},
		{Hook{Event: Stop, Matcher: "bash", Command: "x"}, "Stop hooks take no matcher"},
		{Hook{Event: PreToolUse, Matcher: "(", Command: "x"}, "matcher"},
		{Hook{Event: PreToolUse, Command: "x", Timeout: -1}, "timeout is negative"},
	} {
		if err := tc.hook.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Validate(%+v) = %v, want %q", tc.hook, err, tc.want)
		}
	}
	if err := (Hook{Event: PreToolUse, Matcher: "edit|write", Command: "x", Timeout: 30}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Diagnostics{Commands: map[string]string{"go": "x"}}).Validate(); err == nil {
		t.Fatal("an extension without its dot was accepted")
	}
}

// goModule writes a Go module with a.go and b.go and returns a.go's path.
func goModule(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	dir := t.TempDir()
	for name, text := range map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.22\n",
		"a.go":   "package m\n\nfunc A() int { return 1 }\n",
		"b.go":   "package m\n\nfunc B() int { return A() }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "a.go")
}

func editCall(file string) ToolCall {
	return ToolCall{Name: "edit", Input: json.RawMessage(`{}`), Files: []string{file}}
}

func TestDiagnosticsReportErrorsInTheEditedGoFile(t *testing.T) {
	file := goModule(t)
	if err := os.WriteFile(file, []byte("package m\n\nfunc A() int { return 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, _ := testRunner(t, Config{})
	scope := testScope(t)

	told := runner.PostToolUse(context.Background(), scope, editCall(file), ToolResponse{Status: "success"})
	if !strings.HasPrefix(told, "Diagnostics after edit:\n") || !strings.Contains(told, file+":3:") {
		t.Fatalf("told = %q", told)
	}
	if told := runner.PostToolUse(context.Background(), scope, editCall(filepath.Join(filepath.Dir(file), "b.go")), ToolResponse{Status: "success"}); strings.Contains(told, "b.go") {
		t.Fatalf("errors of another file were reported for b.go: %q", told)
	}
	if told := runner.PostToolUse(context.Background(), scope, editCall(file), ToolResponse{Status: "neutral"}); told != "" {
		t.Fatalf("an edit that changed nothing was checked: %q", told)
	}
}

func TestDiagnosticsReportTypeErrorsAndStayQuietOnCleanFiles(t *testing.T) {
	file := goModule(t)
	runner, _ := testRunner(t, Config{})
	if told := runner.PostToolUse(context.Background(), testScope(t), editCall(file), ToolResponse{Status: "success"}); told != "" {
		t.Fatalf("a clean file got %q", told)
	}
	if err := os.WriteFile(file, []byte("package m\n\nfunc A() int { return \"one\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	told := runner.PostToolUse(context.Background(), testScope(t), editCall(file), ToolResponse{Status: "success"})
	if !strings.Contains(told, "a.go:3:") {
		t.Fatalf("told = %q", told)
	}
}

func TestDiagnosticsCanBeOverriddenAndTurnedOff(t *testing.T) {
	file := goModule(t)
	if err := os.WriteFile(file, []byte("package m\n\nfunc A( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		cfg  Diagnostics
		want string
	}{
		"all off":       {Diagnostics{Disabled: true}, ""},
		"extension off": {Diagnostics{Commands: map[string]string{".go": ""}}, ""},
		"own command":   {Diagnostics{Commands: map[string]string{".go": `echo "$MATRIXCLAW_FILE:1:1: custom check"; echo "other.go:1:1: elsewhere"`}}, "Diagnostics after edit:\n" + file + ":1:1: custom check"},
	} {
		t.Run(name, func(t *testing.T) {
			runner, _ := testRunner(t, Config{Diagnostics: tc.cfg})
			if told := runner.PostToolUse(context.Background(), testScope(t), editCall(file), ToolResponse{Status: "success"}); told != tc.want {
				t.Fatalf("told = %q, want %q", told, tc.want)
			}
		})
	}
}

func TestDiagnosticsParseJSON(t *testing.T) {
	file := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(file, []byte("{\n  \"a\": 1,\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, _ := testRunner(t, Config{})
	told := runner.PostToolUse(context.Background(), testScope(t), ToolCall{Name: "write", Files: []string{file}}, ToolResponse{Status: "success"})
	if !strings.Contains(told, file+":3:1: invalid character '}'") {
		t.Fatalf("told = %q", told)
	}
}

func TestDiagnosticsCheckEveryFileAPatchChanged(t *testing.T) {
	dir := t.TempDir()
	good, bad := filepath.Join(dir, "good.json"), filepath.Join(dir, "bad.json")
	if err := os.WriteFile(good, []byte(`{"a": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(`{"a": }`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner, _ := testRunner(t, Config{})
	told := runner.PostToolUse(context.Background(), testScope(t), ToolCall{Name: "apply_patch", Files: []string{good, bad}}, ToolResponse{Status: "success"})
	if !strings.Contains(told, bad+":1:7:") || strings.Contains(told, good) {
		t.Fatalf("told = %q", told)
	}
}

func TestMentioningKeepsLinesNamingTheFile(t *testing.T) {
	output := strings.Join([]string{
		"# example.com/m",
		"./a.go:3:9: cannot use \"one\"",
		"\thave string",
		"./ba.go:1:1: other file",
		"pkg/a.go:2:1: same name, other directory",
		"src/a.go(4,2): error TS2304",
		"vet: m/a.go:5:1: nested",
	}, "\n")
	got := mentioning(output, "/work/m/a.go")
	want := []string{"./a.go:3:9: cannot use \"one\"", "\thave string", "vet: m/a.go:5:1: nested"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("kept %q, want %q", got, want)
	}
	if got := mentioning("src/a.ts(4,2): error TS2304: Cannot find name 'x'.", "/app/src/a.ts"); len(got) != 1 {
		t.Fatalf("tsc line not kept: %q", got)
	}
}

func TestDiagnosticsUseTheLanguageServerAndReportBrokenCallers(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	file := goModule(t)
	dir := filepath.Dir(file)
	if err := os.MkdirAll(filepath.Join(dir, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	caller := filepath.Join(dir, "c", "c.go")
	if err := os.WriteFile(caller, []byte("package c\n\nimport \"example.com/m\"\n\nfunc C() int { return m.A() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{}
	servers := lsp.NewManager(t.TempDir(), nil, func() lsp.Config { return cfg.Diagnostics.LSP })
	defer servers.Close()
	runner := NewRunner(nil, func() Config { return cfg }, filepath.Join(t.TempDir(), "hooks-trust.json"), servers)
	scope := testScope(t)
	if told := runner.PostToolUse(context.Background(), scope, editCall(file), ToolResponse{Status: "success"}); told != "" {
		t.Fatalf("a clean file got %q", told)
	}
	if err := os.WriteFile(file, []byte("package m\n\nfunc  A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if told := runner.PostToolUse(context.Background(), scope, editCall(file), ToolResponse{Status: "success"}); told != "Diagnostics after edit:\n"+file+":1:1: the file is not gofmt-formatted" {
		t.Fatalf("an unformatted file the server checked got %q", told)
	}

	data := filepath.Join(dir, "data.json")
	if err := os.WriteFile(data, []byte(`{"a": }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("package m\n\nfunc A(x int) int { return \"x\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	told := runner.PostToolUse(context.Background(), scope, ToolCall{Name: "apply_patch", Files: []string{file, data}}, ToolResponse{Status: "success"})
	t.Logf("checked in %s:\n%s", time.Since(start), told)
	for _, want := range []string{
		"Diagnostics after edit:\n" + file + ":3:28: cannot use \"x\"",
		"\n" + data + ":1:7: invalid character",
		"\nErrors in other files:\n",
		"\n" + filepath.Join(dir, "b.go") + ":3:25: not enough arguments in call to A",
		"\n" + caller + ":5:27: not enough arguments in call to m.A",
	} {
		if !strings.Contains(told, want) {
			t.Errorf("missing %q", want)
		}
	}

	cfg.Diagnostics.Commands = map[string]string{".go": `echo "$MATRIXCLAW_FILE:1:1: own check"`}
	if told := runner.PostToolUse(context.Background(), scope, editCall(file), ToolResponse{Status: "success"}); told != "Diagnostics after edit:\n"+file+":1:1: own check" {
		t.Fatalf("an extension's own command did not replace the server: %q", told)
	}
}
