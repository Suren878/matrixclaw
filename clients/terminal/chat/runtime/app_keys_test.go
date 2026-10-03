package runtime

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestSpaceExpandsTheSelectedToolOutput(t *testing.T) {
	var output []string
	for i := range 30 {
		output = append(output, "output line "+strings.Repeat("x", i%3))
	}
	output = append(output, "the last line")
	m, _ := renderApp(t, 120, 200, core.ClientSnapshot{SessionID: "session_1", Messages: []transcript.Message{
		{ID: "m1", SessionID: "session_1", Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{
			{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "call_1", Name: "bash", Input: `{"command":"make"}`, Finished: true}},
		}},
		{ID: "m2", SessionID: "session_1", Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{
			{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: "call_1", Name: "bash", Content: strings.Join(output, "\n")}},
		}},
	}})
	_, focusChat := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(focusChat())
	if strings.Contains(ansi.Strip(m.viewContent()), "the last line") {
		t.Fatal("long output is not collapsed to begin with")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})

	if !strings.Contains(ansi.Strip(m.viewContent()), "the last line") {
		t.Fatalf("space did not expand the selected output:\n%s", ansi.Strip(m.viewContent()))
	}
}
