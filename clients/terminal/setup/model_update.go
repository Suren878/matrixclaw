package setup

import (
	tea "charm.land/bubbletea/v2"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
)

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
	event := m.updateListSelection(keyMsg.String(), &m.cursor, 2, components.RoleBack)
	switch event.Kind {
	case components.EventSelect:
		if m.cursor == 0 {
			m.cursor = 0
			m.screen = next
			return m, nil
		}
		if m.cursor == 1 {
			m.openDraftForm(edit)
			return m, nil
		}
	case components.EventBack:
		m.screen = back
		m.cursor = 0
	}
	return m, nil
}

func (m *model) updateForm(msg tea.Msg, fieldCount int, cancel func(), save func() error, selectField func() tea.Cmd) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	state := m.formState(fieldCount)
	event := state.Update(keyMsg.String(), stateItems(fieldCount), setupFormButtons(), components.RoleCancel)
	m.applyFormState(state, fieldCount)
	switch event.Kind {
	case components.EventCancel, components.EventBack:
		cancel()
	case components.EventSubmit:
		m.submitFormAction(save, cancel)
		return m, nil
	case components.EventEdit:
		return m, selectField()
	}
	return m, nil
}

func (m *model) updateListSelection(key string, cursor *int, count int, closeRole components.Role) components.Event {
	state := components.ListState{Cursor: *cursor, NoWrap: true}
	event := state.Update(key, stateItems(count), closeRole)
	state.Clamp(count)
	*cursor = state.Cursor
	return event
}

func (m *model) formState(fieldCount int) components.FormState {
	return components.FormState{
		Focus:  formFocus(m.formFocus, fieldCount),
		Button: m.formAction,
		NoWrap: true,
	}
}

func (m *model) applyFormState(state components.FormState, fieldCount int) {
	if state.Focus.Kind == components.FormFocusButton {
		m.formFocus = fieldCount
		m.formAction = state.Button
		return
	}
	m.formFocus = state.Focus.Index
	if m.formFocus < 0 {
		m.formFocus = 0
	}
	if m.formFocus > fieldCount {
		m.formFocus = fieldCount
	}
}

func stateItems(count int) []components.Item {
	if count <= 0 {
		return nil
	}
	items := make([]components.Item, count)
	for i := range items {
		items[i] = components.Item{Title: "Item"}
	}
	return items
}

func updateConfirmSelection(key string, selected *int) components.Event {
	state := components.ConfirmState{Selected: *selected}
	event := state.Update(key)
	*selected = state.Selected
	return event
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
