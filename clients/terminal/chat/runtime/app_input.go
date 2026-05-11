package runtime

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m *appModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if handled, cmd := m.handleGlobalKey(msg); handled {
		return m, cmd
	}

	km := m.input.KeyMap()

	switch msg.String() {
	case "ctrl+s":
		return m, m.controlplaneCmd("/sessions")
	}

	if key.Matches(msg, km.Commands) {
		return m, m.openCommandsDialogCmd()
	}

	if m.focus == appFocusEditor {
		switch {
		case m.busy && key.Matches(msg, km.Editor.Escape):
			return m, m.openCancelRunDialog()
		case m.busy && key.Matches(msg, km.Chat.NewSession):
			m.err = "agent is busy, please wait before starting a new session"
			return m, nil
		case m.busy && key.Matches(msg, km.Editor.OpenEditor):
			m.err = "agent is working, please wait"
			return m, nil
		}
		return m, m.input.Update(msg)
	}

	switch msg.String() {
	case "esc":
		if m.busy {
			return m, m.openCancelRunDialog()
		}
	case "tab":
		return m, m.setFocus(appFocusEditor)
	case "r":
		m.loading = true
		m.err = ""
		return m, m.loadInitialCmd()
	}

	if m.chat == nil {
		return m, nil
	}

	switch msg.String() {
	case "down", "j":
		m.chat.SelectNext()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case "up", "k":
		m.chat.SelectPrev()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case "d":
		return m, m.chat.ScrollByAndAnimate(max(1, m.chat.Height()/2))
	case "u", "ctrl+u":
		return m, m.chat.ScrollByAndAnimate(-max(1, m.chat.Height()/2))
	case "pgdown", "f":
		return m, m.chat.ScrollByAndAnimate(max(1, m.chat.Height()))
	case "pgup", "b":
		return m, m.chat.ScrollByAndAnimate(-max(1, m.chat.Height()))
	case "home", "g":
		m.chat.SelectFirst()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case "end", "G":
		m.chat.SelectLast()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case " ":
		m.chat.ToggleExpandedSelectedItem()
		return m, nil
	default:
		if handled, cmd := m.chat.HandleKeyMsg(msg); handled {
			return m, cmd
		}
	}
	return m, nil
}

func (m *appModel) handleGlobalKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return true, tea.Quit
	case "ctrl+g":
		m.help.ShowAll = !m.help.ShowAll
		m.resizeChat()
		return true, nil
	default:
		return false, nil
	}
}

func (m *appModel) handleDialogInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.dialog == nil || !m.dialog.HasDialogs() {
		return m, nil
	}
	action := m.dialog.Update(msg)
	if action == nil {
		return m, nil
	}
	return m.Update(action)
}

func (m *appModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.width <= 0 || m.height <= 0 {
		return m, nil
	}
	mouse := msg.Mouse()

	editorTop, editorBottom := m.editorBounds()
	if _, ok := msg.(tea.MouseClickMsg); ok && mouse.Y >= editorTop && mouse.Y < editorBottom {
		return m, m.setFocus(appFocusEditor)
	}

	if m.chat == nil {
		return m, nil
	}

	bodyTop, bodyBottom := m.bodyBounds()
	if bodyBottom <= bodyTop {
		return m, nil
	}

	if !isMouseRelease(msg) && !isMouseMotion(msg) && (mouse.Y < bodyTop || mouse.Y >= bodyBottom) {
		return m, nil
	}

	handled, cmd := m.chat.HandleViewportMouse(msg, mouse.X, mouse.Y-bodyTop)
	if !handled {
		return m, nil
	}
	focusCmd := m.setFocus(appFocusChat)
	return m, tea.Batch(focusCmd, cmd)
}

func isMouseMotion(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseMotionMsg)
	return ok
}

func isMouseRelease(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseReleaseMsg)
	return ok
}
