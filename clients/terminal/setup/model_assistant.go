package setup

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
)

func (m *model) renderAssistantForm() string {
	items := []listItem{
		{Title: "Continue"},
		{Title: "Name", Status: nonEmpty(m.cfg.Assistant.Name, "matrixclaw")},
		{Title: "User prompt", Status: assistantPromptStatus(m.cfg.Assistant.CustomInstructions)},
	}
	extraLines := []string{"", setupFooterStyle.Render("System prompt is managed by matrixclaw.")}
	card := components.RenderListCard(m.commandFrame(), components.ListData{
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
	itemCount := 3
	event := m.updateListSelection(keyMsg.String(), &m.formFocus, itemCount)
	switch event.Kind {
	case components.EventBack:
		m.cancelForm(screenProviderList)
	case components.EventSelect:
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
			m.openTextEditor(textEditAssistantName, "Assistant Name", "matrixclaw", m.cfg.Assistant.Name, false)
		case 2:
			m.openTextEditor(textEditAssistantCustomPrompt, "User Prompt", "User instructions for every run", m.cfg.Assistant.CustomInstructions, false)
		}
	}
	return m, nil
}

func (m *model) handleAssistantFormSave() error {
	m.cfg.Assistant.Name = strings.TrimSpace(m.cfg.Assistant.Name)
	m.cfg.Assistant.CustomInstructions = strings.TrimSpace(m.cfg.Assistant.CustomInstructions)
	return m.commitFormAndReturn(screenChannelsList)
}

func assistantPromptStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "Custom"
}
