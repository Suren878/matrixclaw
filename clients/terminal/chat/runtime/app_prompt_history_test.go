package runtime

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestUpArrowRecallsTheLatestPromptFirst(t *testing.T) {
	m := newApp(context.Background(), nil)
	m.width, m.height = 120, 40
	m.read = readmodel.New(core.ClientSnapshot{SessionID: "session_1", Messages: []transcript.Message{
		{ID: "m1", SessionID: "session_1", Role: transcript.MessageRoleUser, Content: "first"},
		{ID: "m2", SessionID: "session_1", Role: transcript.MessageRoleAssistant, Content: "reply"},
		{ID: "m3", SessionID: "session_1", Role: transcript.MessageRoleUser, Content: "second"},
	}})
	m.rebuildChat()

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	for _, want := range []string{"second", "first"} {
		m.input.Update(up)
		if got := m.input.Value(); got != want {
			t.Fatalf("editor = %q, want %q", got, want)
		}
	}
}
