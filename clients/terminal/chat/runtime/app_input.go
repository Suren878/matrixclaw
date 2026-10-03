package runtime

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
)

func (m *appModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := m.input.KeyMap()

	switch {
	case key.Matches(msg, km.Sessions):
		return m, m.controlplaneCmd("/sessions")
	case key.Matches(msg, km.Chat.Todo):
		return m, m.toggleTodoPanel()
	case key.Matches(msg, km.Commands):
		m.openCommandsDialog()
		return m, nil
	}

	if m.focus == appFocusEditor {
		switch {
		case m.busy && key.Matches(msg, km.Editor.Escape):
			return m, m.openCancelRunDialog()
		case m.busy && key.Matches(msg, km.Editor.OpenEditor):
			m.err = "agent is working, please wait"
			return m, nil
		}
		return m, m.input.Update(msg)
	}

	switch {
	case key.Matches(msg, km.Chat.Cancel):
		if m.busy {
			return m, m.openCancelRunDialog()
		}
	case key.Matches(msg, km.Tab):
		return m, m.setFocus(appFocusEditor)
	case key.Matches(msg, km.Chat.Reload):
		m.loading = true
		m.err = ""
		return m, m.loadInitialCmd()
	}

	if m.chat == nil {
		return m, nil
	}

	switch {
	case key.Matches(msg, km.Chat.Down):
		m.chat.SelectNext()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.Up):
		m.chat.SelectPrev()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.HalfPageDown):
		return m, m.chat.ScrollByAndAnimate(max(1, m.chat.Height()/2))
	case key.Matches(msg, km.Chat.HalfPageUp):
		return m, m.chat.ScrollByAndAnimate(-max(1, m.chat.Height()/2))
	case key.Matches(msg, km.Chat.PageDown):
		return m, m.chat.ScrollByAndAnimate(max(1, m.chat.Height()))
	case key.Matches(msg, km.Chat.PageUp):
		return m, m.chat.ScrollByAndAnimate(-max(1, m.chat.Height()))
	case key.Matches(msg, km.Chat.Home):
		m.chat.SelectFirst()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.End):
		m.chat.SelectLast()
		return m, m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.Expand):
		m.chat.ToggleExpandedSelectedItem()
		return m, nil
	case key.Matches(msg, km.Chat.Copy):
		content := strings.TrimSpace(m.chat.CopyContent())
		if content == "" {
			return m, nil
		}
		m.err = ""
		return m, tea.SetClipboard(content)
	default:
		if handled, cmd := m.chat.HandleKeyMsg(msg); handled {
			return m, cmd
		}
	}
	return m, nil
}

func (m *appModel) handleGlobalKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := m.input.KeyMap()
	switch {
	case key.Matches(msg, km.Quit):
		return true, tea.Quit
	case key.Matches(msg, km.Help):
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
	sourceID := ""
	if top := m.dialog.DialogLast(); top != nil {
		sourceID = top.ID()
	}
	fromCommands := m.commandsDialogRoot && m.dialog.ContainsDialog(surfacedialog.CommandsID)
	action := m.dialog.Update(msg)
	if action == nil {
		return m, nil
	}
	if command, ok := action.(surfacedialog.ActionRunControlplaneCommand); ok {
		if sourceID != "" && sourceID != surfacedialog.CommandsID {
			m.dialog.CloseDialog(sourceID)
		}
		return m, m.handleRunControlplaneCommand(command, fromCommands)
	}
	next, cmd := m.Update(action)
	return next, cmd
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
