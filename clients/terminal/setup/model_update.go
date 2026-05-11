package setup

import tea "charm.land/bubbletea/v2"

func (m *model) updateIntro(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "enter", " ":
		m.screen = screenDaemonList
	case "esc":
		m.aborted = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) updateStepList(msg tea.Msg, back screen, next screen, edit screen) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "up", "k", "down", "j":
		m.moveIndex(keyMsg.String(), &m.cursor, 1)
	case "enter":
		if m.cursor == 0 {
			m.cursor = 0
			m.screen = next
			return m, nil
		}
		if m.cursor == 1 {
			m.openDraftForm(edit)
			return m, nil
		}
	case "esc":
		m.screen = back
		m.cursor = 0
	}
	return m, nil
}

func (m *model) moveFormCursor(key string, fieldCount int) bool {
	switch key {
	case "up", "k":
		if m.formFocus > 0 {
			m.formFocus--
		}
	case "down", "j":
		if m.formFocus < fieldCount {
			m.formFocus++
		}
	case "left", "h":
		if m.formFocus == fieldCount && m.formAction > 0 {
			m.formAction--
		}
	case "right", "l":
		if m.formFocus == fieldCount && m.formAction < 1 {
			m.formAction++
		}
	default:
		return false
	}
	return true
}

func (m *model) updateForm(msg tea.Msg, fieldCount int, cancel func(), save func() error, selectField func()) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc":
		cancel()
	case "up", "k", "down", "j", "left", "h", "right", "l":
		m.moveFormCursor(keyMsg.String(), fieldCount)
	case "enter":
		if m.formFocus == fieldCount {
			m.submitFormAction(save, cancel)
			return m, nil
		}
		selectField()
	}
	return m, nil
}

func (m *model) moveIndex(key string, index *int, maxIndex int) bool {
	if maxIndex < 0 {
		*index = 0
		return false
	}
	switch key {
	case "up", "k":
		if *index > 0 {
			*index--
		}
	case "down", "j":
		if *index < maxIndex {
			*index++
		}
	default:
		return false
	}
	return true
}

func (m *model) openDraftForm(target screen) {
	m.draftSnapshot = cloneDraft(m.draft)
	m.screen = target
	m.formFocus = 0
	m.formAction = 0
	m.formError = ""
}

func (m *model) returnToList(target screen) {
	m.formError = ""
	m.screen = target
	m.cursor = 0
}

func (m *model) cancelDraftForm(target screen) {
	m.draft = cloneDraft(m.draftSnapshot)
	m.returnToList(target)
}

func (m *model) saveDraftAndReturn(target screen) error {
	if err := m.service.SaveDraft(m.draft); err != nil {
		return err
	}
	m.draftSnapshot = cloneDraft(m.draft)
	m.returnToList(target)
	return nil
}

func (m *model) submitFormAction(save func() error, cancel func()) {
	if m.formAction != 0 {
		cancel()
		return
	}
	if err := save(); err != nil {
		m.formError = err.Error()
		return
	}
	m.formError = ""
}
