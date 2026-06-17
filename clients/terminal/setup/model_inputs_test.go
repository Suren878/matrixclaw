package setup

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLargeTextEditorEnterKeepsEditingCustomPrompt(t *testing.T) {
	m := &model{screen: screenAssistantForm}
	m.draft.AssistantCustomPrompt = "old prompt"
	m.openTextEditor(textEditAssistantCustomPrompt, "Custom prompt", "", m.draft.AssistantCustomPrompt, false)
	m.textAreaInput.SetValue("first line")

	_, _ = m.updateLargeTextEditor(keyPress(tea.KeyEnter, 0))

	if m.screen != screenTextEditor {
		t.Fatalf("screen = %v, want screenTextEditor", m.screen)
	}
	if got := m.draft.AssistantCustomPrompt; got != "old prompt" {
		t.Fatalf("AssistantCustomPrompt = %q, want unchanged draft", got)
	}
}

func TestLargeTextEditorCtrlSSavesCustomPrompt(t *testing.T) {
	m := &model{screen: screenAssistantForm}
	m.openTextEditor(textEditAssistantCustomPrompt, "Custom prompt", "", "", false)
	m.textAreaInput.SetValue("first line\nsecond line")

	_, _ = m.updateLargeTextEditor(keyPress('s', tea.ModCtrl))

	if m.screen != screenAssistantForm {
		t.Fatalf("screen = %v, want screenAssistantForm", m.screen)
	}
	if got := m.draft.AssistantCustomPrompt; got != "first line\nsecond line" {
		t.Fatalf("AssistantCustomPrompt = %q", got)
	}
}

func keyPress(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
}
