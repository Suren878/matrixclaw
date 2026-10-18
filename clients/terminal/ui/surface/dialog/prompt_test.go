package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func TestPromptsHandTheAnswerApartFromTheCommand(t *testing.T) {
	data := controlplane.PromptData{Submit: "/modules set web_search tavily_key", Value: "old", Sensitive: true}
	line := NewPromptCommand(nil, data)
	text := NewTextEditCommand(nil, controlplane.PromptData{Submit: "/modules skills save-body library review", Value: "Step one", Multiline: true})

	for _, tc := range []struct {
		dialog Dialog
		want   ActionAnswerControlplanePrompt
	}{
		{line, ActionAnswerControlplanePrompt{Submit: data.Submit, Answer: "old"}},
		{text, ActionAnswerControlplanePrompt{Submit: "/modules skills save-body library review", Answer: "Step one"}},
	} {
		got := tc.dialog.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
		if got != tc.want {
			t.Errorf("%s submits %#v, want %#v", tc.dialog.ID(), got, tc.want)
		}
	}
}
