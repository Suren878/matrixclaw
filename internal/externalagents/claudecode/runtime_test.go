package claudecode

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

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

func collectEvents(events <-chan externalagents.Event) []externalagents.Event {
	var out []externalagents.Event
	for event := range events {
		out = append(out, event)
	}
	return out
}
