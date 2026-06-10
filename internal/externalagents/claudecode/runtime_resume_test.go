package claudecode

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestClaudePromptArgsInitialTurnUsesJSONOutput(t *testing.T) {
	args := strings.Join(claudePromptArgs(externalagents.ExternalSession{Model: "sonnet"}, "hello"), "\n")
	if !strings.Contains(args, "-p\nhello") {
		t.Fatalf("args missing prompt: %q", args)
	}
	if !strings.Contains(args, "--output-format\njson") {
		t.Fatalf("args missing JSON output: %q", args)
	}
	if strings.Contains(args, "--resume") {
		t.Fatalf("initial turn should not resume: %q", args)
	}
}

func TestClaudePromptArgsResumesRealSessionID(t *testing.T) {
	args := strings.Join(claudePromptArgs(externalagents.ExternalSession{
		ExternalThreadID:  "session-1",
		ExternalSessionID: "session-1",
		Model:             "sonnet",
	}, "hello"), "\n")
	if !strings.Contains(args, "--resume\nsession-1") {
		t.Fatalf("args missing resume: %q", args)
	}
}

func TestClaudePromptArgsIgnoresLegacySyntheticSessionID(t *testing.T) {
	args := strings.Join(claudePromptArgs(externalagents.ExternalSession{
		ExternalThreadID:  "claude-123",
		ExternalSessionID: "claude-123",
	}, "hello"), "\n")
	if strings.Contains(args, "--resume") {
		t.Fatalf("synthetic session should not resume: %q", args)
	}
}

func TestParseClaudePromptOutputExtractsSessionID(t *testing.T) {
	output, err := parseClaudePromptOutput([]byte(`{"result":"answer","session_id":"session-1"}`))
	if err != nil {
		t.Fatalf("parseClaudePromptOutput: %v", err)
	}
	if output.Result != "answer" || output.SessionID != "session-1" {
		t.Fatalf("output = %#v", output)
	}
}
