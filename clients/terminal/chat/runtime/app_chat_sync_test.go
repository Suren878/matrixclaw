package runtime

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func longOutput() string {
	var lines []string
	for range 30 {
		lines = append(lines, "output line")
	}
	return strings.Join(append(lines, "the last line"), "\n")
}

func streamingSnapshot() core.ClientSnapshot {
	return core.ClientSnapshot{SessionID: "session_1", Session: renderSession(), Context: renderContext(), Messages: []transcript.Message{
		textMessage("m1", transcript.MessageRoleUser, 0, "build it"),
		toolCallMessage("m2", 1, "call_bash", "bash", map[string]any{"command": "make"}),
		toolResultMessage("m3", 2, "call_bash", "bash", longOutput(), nil),
		textMessage("m4", transcript.MessageRoleAssistant, 3, "Built"),
	}}
}

func TestLiveEventsRebuildOnlyTheRowsTheyChange(t *testing.T) {
	m, _ := renderApp(t, 120, 60, streamingSnapshot())
	bash, user := m.rows["call_bash"].item, m.rows["m1"].item

	deliver(t, m, core.EventMessageUpdated, textMessage("m4", transcript.MessageRoleAssistant, 3, "Built, and the tests pass."))

	if m.rows["call_bash"].item != bash || m.rows["m1"].item != user {
		t.Fatal("rows the event did not touch were rebuilt")
	}
	if screen := ansi.Strip(m.viewContent()); !strings.Contains(screen, "Built, and the tests pass.") {
		t.Fatalf("streamed text not shown:\n%s", screen)
	}
}

func TestExpandedOutputStaysExpandedWhenItsRowChanges(t *testing.T) {
	m, _ := renderApp(t, 120, 80, streamingSnapshot())
	_, focusChat := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(focusChat())
	for !strings.Contains(ansi.Strip(m.viewContent()), "the last line") {
		if m.chat.SelectedMessageID() == "call_bash" {
			m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		before := m.chat.SelectedMessageID()
		m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		if m.chat.SelectedMessageID() == before {
			t.Fatal("could not select the command output")
		}
	}

	deliver(t, m, core.EventToolUpdated, core.ToolUpdate{ToolCallID: "call_bash", ToolName: "bash", State: core.ToolLifecycleCompleted, RunID: "run_1", SessionID: "session_1"})

	if screen := ansi.Strip(m.viewContent()); !strings.Contains(screen, "the last line") {
		t.Fatalf("output collapsed after its tool update:\n%s", screen)
	}
}

func TestReloadedSnapshotReplacesRowsWhoseContentChanged(t *testing.T) {
	snapshot := streamingSnapshot()
	m, _ := renderApp(t, 120, 60, snapshot)

	snapshot.Messages[3] = textMessage("m4", transcript.MessageRoleAssistant, 3, "Rebuilt from scratch")
	m.stopStream()
	m.Update(connectedMsg{streamID: m.stream.id, sessionID: "session_1"})
	m.Update(snapshotMsg{streamID: m.stream.id, snapshot: snapshot})

	if screen := ansi.Strip(m.viewContent()); !strings.Contains(screen, "Rebuilt from scratch") {
		t.Fatalf("reloaded content not shown:\n%s", screen)
	}
}
