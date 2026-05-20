package setup

import (
	tea "charm.land/bubbletea/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
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
	event := m.updateListSelection(keyMsg.String(), &m.cursor, 2, commandui.RoleBack)
	switch event.Kind {
	case commandui.EventSelect:
		if m.cursor == 0 {
			m.cursor = 0
			m.screen = next
			return m, nil
		}
		if m.cursor == 1 {
			m.openDraftForm(edit)
			return m, nil
		}
	case commandui.EventBack:
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
	event := state.Update(keyMsg.String(), stateItems(fieldCount), setupFormButtons(), commandui.RoleCancel)
	m.applyFormState(state, fieldCount)
	switch event.Kind {
	case commandui.EventCancel, commandui.EventBack:
		cancel()
	case commandui.EventSubmit:
		m.submitFormAction(save, cancel)
		return m, nil
	case commandui.EventEdit:
		return m, selectField()
	}
	return m, nil
}

func (m *model) moveIndex(key string, index *int, maxIndex int) bool {
	if maxIndex < 0 {
		*index = 0
		return false
	}
	before := *index
	_ = m.updateListSelection(key, index, maxIndex+1, commandui.RoleBack)
	return before != *index
}

func (m *model) updateListSelection(key string, cursor *int, count int, closeRole commandui.Role) commandui.Event {
	state := commandui.ListState{Cursor: *cursor, NoWrap: true}
	event := state.Update(key, stateItems(count), closeRole)
	state.Clamp(count)
	*cursor = state.Cursor
	return event
}

func (m *model) formState(fieldCount int) commandui.FormState {
	return commandui.FormState{
		Focus:  formFocus(m.formFocus, fieldCount),
		Button: m.formAction,
		NoWrap: true,
	}
}

func (m *model) applyFormState(state commandui.FormState, fieldCount int) {
	if state.Focus.Kind == commandui.FormFocusButton {
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

func stateItems(count int) []commandui.Item {
	if count <= 0 {
		return nil
	}
	items := make([]commandui.Item, count)
	for i := range items {
		items[i] = commandui.Item{Title: "Item"}
	}
	return items
}

func updateConfirmSelection(key string, selected *int) commandui.Event {
	state := commandui.ConfirmState{Selected: *selected}
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
