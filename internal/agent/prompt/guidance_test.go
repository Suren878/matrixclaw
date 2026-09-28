package prompt

import (
	"strings"
	"testing"
)

func TestToolUseDisciplineDoesNotAskToMinimiseToolCalls(t *testing.T) {
	if strings.Contains(ToolUseDiscipline(), "fewest tool calls") {
		t.Fatalf("tool-use discipline still asks to minimise tool calls:\n%s", ToolUseDiscipline())
	}
}

func TestToolUseDisciplineAsksForIndependentCallsInOneReply(t *testing.T) {
	text := ToolUseDiscipline()
	for _, want := range []string{"run at the same time", "independent reads, searches and inspections into one reply", "goes in a later reply"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool-use discipline lacks %q:\n%s", want, text)
		}
	}
}

func TestToolUseDisciplineTracksWorkWithTodo(t *testing.T) {
	text := ToolUseDiscipline()
	if !strings.Contains(text, "todo_write") || strings.Contains(text, "session plan") {
		t.Fatalf("tool-use discipline does not ask for todo_write:\n%s", text)
	}
}
