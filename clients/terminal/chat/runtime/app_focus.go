package runtime

import tea "charm.land/bubbletea/v2"

func (m *appModel) setFocus(focus appFocus) tea.Cmd {
	m.focus = focus
	switch focus {
	case appFocusEditor:
		if m.chat != nil {
			m.chat.Blur()
		}
		return m.input.Focus()
	case appFocusChat:
		m.input.Blur()
		if m.chat != nil {
			m.chat.Focus()
		}
	}
	return nil
}
