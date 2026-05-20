package setup

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) renderAssistantForm() string {
	items := []listItem{
		{Title: "Continue"},
		{Title: "Name", Status: nonEmpty(m.draft.AssistantName, "matrixclaw")},
		{Title: "User prompt", Status: assistantPromptStatus(m.draft.AssistantCustomPrompt)},
		{Title: "Refresh project context"},
	}
	extraLines := []string{"", setupFooterStyle.Render("System prompt is managed by matrixclaw.")}
	card := commandui.RenderListCard(m.commandFrame(), commandui.ListData{
		Title:      "Assistant Profile",
		Meta:       "Step 3/5",
		Items:      commandItems(items),
		Selected:   m.formFocus,
		ExtraLines: extraLines,
		Help:       "enter select · ↑/↓ move · esc back",
		Error:      m.formError,
	})
	return m.renderCommandCard(card)
}

func (m *model) updateAssistantForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	itemCount := 4
	event := m.updateListSelection(keyMsg.String(), &m.formFocus, itemCount, commandui.RoleBack)
	switch event.Kind {
	case commandui.EventBack:
		m.cancelDraftForm(screenProviderList)
	case commandui.EventSelect:
		if m.formFocus == 0 {
			if err := m.handleAssistantFormSave(); err != nil {
				m.formError = err.Error()
				return m, nil
			}
			m.formError = ""
			return m, nil
		}
		switch m.formFocus {
		case 1:
			m.openTextEditor(textEditAssistantName, "Assistant Name", "matrixclaw", m.draft.AssistantName, false)
		case 2:
			m.openTextEditor(textEditAssistantCustomPrompt, "User Prompt", "User instructions for every run", m.draft.AssistantCustomPrompt, false)
		case 3:
			m.draft.AssistantSystemPrompt = setup.InitializeAssistantSystemPromptForDraft(m.draft.AssistantSystemPrompt, m.draft)
			m.formError = "project context refreshed"
		}
	}
	return m, nil
}

func (m *model) handleAssistantFormSave() error {
	m.draft.AssistantName = strings.TrimSpace(m.draft.AssistantName)
	m.draft.AssistantSystemPrompt = strings.TrimSpace(m.draft.AssistantSystemPrompt)
	m.draft.AssistantCustomPrompt = strings.TrimSpace(m.draft.AssistantCustomPrompt)
	if m.draft.AssistantName == "" {
		m.draft.AssistantName = "matrixclaw"
	}
	m.draft.AssistantSystemPrompt = setup.InitializeAssistantSystemPromptForDraft(m.draft.AssistantSystemPrompt, m.draft)
	return m.saveDraftAndReturn(screenChannelsList)
}

func assistantPromptStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "Custom"
}
