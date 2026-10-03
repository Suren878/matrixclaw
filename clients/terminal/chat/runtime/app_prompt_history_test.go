package runtime

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestUpArrowRecallsTheLatestPromptFirst(t *testing.T) {
	m, _ := renderApp(t, 120, 40, core.ClientSnapshot{SessionID: "session_1", Messages: []transcript.Message{
		{ID: "m1", SessionID: "session_1", Role: transcript.MessageRoleUser, Content: "first"},
		{ID: "m2", SessionID: "session_1", Role: transcript.MessageRoleAssistant, Content: "reply"},
		{ID: "m3", SessionID: "session_1", Role: transcript.MessageRoleUser, Content: "second"},
	}})

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	for _, want := range []string{"second", "first"} {
		m.input.Update(up)
		if got := m.input.Value(); got != want {
			t.Fatalf("editor = %q, want %q", got, want)
		}
	}
}
