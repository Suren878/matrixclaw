package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestRunPromptStreamsPartialClaudeOutput(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "fake-claude")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"session-1"}'
printf '%s\n' '{"type":"stream_event","session_id":"session-1","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}'
printf '%s\n' '{"type":"result","subtype":"success","session_id":"session-1","is_error":false,"result":"hello"}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake Claude executable: %v", err)
	}
	runtime := NewRuntime(RuntimeOptions{Enabled: true})
	out := make(chan externalagents.Event, 8)
	runtime.runPrompt(context.Background(), out, scriptPath, externalagents.ExternalSession{}, "hello")
	events := collectEvents(out)

	messageDeltas := 0
	completed := false
	for _, event := range events {
		if event.Kind == externalagents.EventMessageDelta {
			messageDeltas++
			if event.Text != "hello" {
				t.Fatalf("message delta = %q, want hello", event.Text)
			}
		}
		if event.Kind == externalagents.EventTurnCompleted {
			completed = true
		}
	}
	if messageDeltas != 1 {
		t.Fatalf("message delta count = %d, want exactly one (no final duplication)", messageDeltas)
	}
	if !completed {
		t.Fatalf("events = %#v, want completed turn", events)
	}
}

func TestRunPromptPanicEmitsTurnFailedAndCloses(t *testing.T) {
	runtime := NewRuntime(RuntimeOptions{Enabled: true})
	out := make(chan externalagents.Event, 4)
	session := externalagents.ExternalSession{ExternalThreadID: "thread-1"}

	runtime.runPrompt(nil, out, "/bin/true", session, "hello") //nolint:staticcheck // Exercises panic recovery for a nil context passed into exec.CommandContext.

	events := collectEvents(out)
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one turn failure", events)
	}
	event := events[0]
	if event.Kind != externalagents.EventTurnFailed {
		t.Fatalf("event kind = %q, want %q", event.Kind, externalagents.EventTurnFailed)
	}
	if event.ExternalThreadID != "thread-1" {
		t.Fatalf("thread id = %q, want thread-1", event.ExternalThreadID)
	}
	if !strings.Contains(event.Error, "claudecode prompt worker panicked") {
		t.Fatalf("event error = %q, want panic failure", event.Error)
	}
}

func TestClaudePromptArgsUsesStreamingAndRootCompatibleAutoMode(t *testing.T) {
	session := externalagents.ExternalSession{
		Model:          "sonnet",
		ApprovalPolicy: "never",
		Sandbox:        "danger-full-access",
	}
	args := claudePromptArgs(session, "hello")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--output-format stream-json", "--include-partial-messages", "--permission-mode auto"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args = %q, want %q", joined, want)
		}
	}
	if strings.Contains(joined, "bypassPermissions") || strings.Contains(joined, "dangerously-skip-permissions") {
		t.Fatalf("args = %q, must not use root-forbidden bypass mode", joined)
	}
}

func TestReadOnlyClaudeSessionIsDeniedEveryChangeWithoutAsking(t *testing.T) {
	session := externalagents.ExternalSession{ApprovalPolicy: "never", Sandbox: "read-only"}
	if joined := strings.Join(claudePromptArgs(session, "hello"), " "); !strings.Contains(joined, "--permission-mode dontAsk") {
		t.Fatalf("args = %q, want dontAsk", joined)
	}
}

func TestClaudeStreamDeltaParsesTextAndThinking(t *testing.T) {
	textEvent := json.RawMessage(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`)
	kind, delta := claudeStreamDelta(textEvent)
	if kind != externalagents.EventMessageDelta || delta != "hello" {
		t.Fatalf("text delta = (%q, %q)", kind, delta)
	}
	thinkingEvent := json.RawMessage(`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"work"}}`)
	kind, delta = claudeStreamDelta(thinkingEvent)
	if kind != externalagents.EventReasoningDelta || delta != "work" {
		t.Fatalf("thinking delta = (%q, %q)", kind, delta)
	}
}

func TestClaudeAgentAdvertisesStreaming(t *testing.T) {
	if !((Agent{}).Capabilities().StreamingEvents) {
		t.Fatal("Claude Code agent must advertise streaming events")
	}
}

func TestCappedBufferBoundsClaudeStderr(t *testing.T) {
	buffer := &cappedBuffer{limit: 16}
	input := strings.Repeat("x", 128)
	if written, err := buffer.Write([]byte(input)); err != nil || written != len(input) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", written, err, len(input))
	}
	if !strings.HasPrefix(buffer.String(), strings.Repeat("x", 16)) || !strings.Contains(buffer.String(), "stderr truncated") {
		t.Fatalf("buffer = %q, want bounded content and truncation marker", buffer.String())
	}
}

func collectEvents(events <-chan externalagents.Event) []externalagents.Event {
	var out []externalagents.Event
	for event := range events {
		out = append(out, event)
	}
	return out
}
