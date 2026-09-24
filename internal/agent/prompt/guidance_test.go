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
