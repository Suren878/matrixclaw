package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestRuntimeSendsPromptThroughClaudeCLI(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	cwdPath := filepath.Join(dir, "cwd")
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(argsPath) + "\npwd > " + shellQuote(cwdPath) + "\nprintf 'answer from claude\\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	workdir := filepath.Join(dir, "work")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	runtime := NewRuntime(RuntimeOptions{Path: bin, Enabled: true})
	session, err := runtime.StartSession(context.Background(), externalagents.StartSessionRequest{CWD: workdir, Model: "sonnet"})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	events, err := runtime.Send(context.Background(), session, externalagents.Input{Text: "hello"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var kinds []externalagents.EventKind
	var text string
	for event := range events {
		kinds = append(kinds, event.Kind)
		text += event.Text
	}
	if !strings.Contains(text, "answer from claude") {
		t.Fatalf("stream text = %q", text)
	}
	if len(kinds) < 2 || kinds[0] != externalagents.EventMessageDelta || kinds[len(kinds)-1] != externalagents.EventTurnCompleted {
		t.Fatalf("event kinds = %#v", kinds)
	}
	argsRaw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := string(argsRaw)
	if !strings.Contains(args, "-p\nhello") || !strings.Contains(args, "--model\nsonnet") || !strings.Contains(args, "--permission-mode\nbypassPermissions") {
		t.Fatalf("claude args = %q", args)
	}
	cwdRaw, err := os.ReadFile(cwdPath)
	if err != nil {
		t.Fatalf("read cwd: %v", err)
	}
	if strings.TrimSpace(string(cwdRaw)) != workdir {
		t.Fatalf("cwd = %q, want %q", strings.TrimSpace(string(cwdRaw)), workdir)
	}
}

func TestRuntimeStartSessionDefaultsModel(t *testing.T) {
	runtime := NewRuntime(RuntimeOptions{Enabled: true})
	session, err := runtime.StartSession(context.Background(), externalagents.StartSessionRequest{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if session.Model != "sonnet" {
		t.Fatalf("model = %q, want sonnet", session.Model)
	}
	if session.ApprovalPolicy != "never" {
		t.Fatalf("approval policy = %q, want never", session.ApprovalPolicy)
	}
	if session.Sandbox != "danger-full-access" {
		t.Fatalf("sandbox = %q, want danger-full-access", session.Sandbox)
	}
}

func TestClaudePermissionModeMapsSessionPolicy(t *testing.T) {
	tests := []struct {
		name    string
		session externalagents.ExternalSession
		want    string
	}{
		{name: "full auto", session: externalagents.ExternalSession{ApprovalPolicy: "never", Sandbox: "danger-full-access"}, want: "bypassPermissions"},
		{name: "accept edits", session: externalagents.ExternalSession{ApprovalPolicy: "on-request", Sandbox: "workspace-write"}, want: "acceptEdits"},
		{name: "default", session: externalagents.ExternalSession{ApprovalPolicy: "on-request", Sandbox: "read-only"}, want: "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := claudePermissionMode(tt.session); got != tt.want {
				t.Fatalf("claudePermissionMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
