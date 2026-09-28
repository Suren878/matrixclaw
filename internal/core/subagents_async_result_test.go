package core_test

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestAsyncSubagentCompletionKeepsTheSpawnResult(t *testing.T) {
	t.Parallel()
	scenario := runAsyncSubagentScenario(t)

	var spawnResult string
	for _, message := range sessionMessages(t, scenario.db, scenario.session.ID) {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == "call-spawn" {
				spawnResult = message.Content
			}
		}
	}
	if !strings.Contains(spawnResult, " started") || strings.Contains(spawnResult, " finished") {
		t.Fatalf("spawn result was rewritten: %q", spawnResult)
	}
	finished := false
	for _, event := range scenario.events {
		if update, ok := event.Payload.(core.ToolUpdate); ok && update.ToolCallID == "call-spawn" && update.State == core.ToolLifecycleCompleted {
			finished = true
		}
	}
	if !finished {
		t.Fatal("no completed tool update for the spawn call")
	}
}
