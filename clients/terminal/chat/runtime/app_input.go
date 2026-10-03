package runtime

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
)

func (m *appModel) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	km := m.input.KeyMap()

	switch {
	case key.Matches(msg, km.Sessions):
		return m.controlplaneCmd("/sessions")
	case key.Matches(msg, km.Chat.Todo):
		return m.toggleTodoPanel()
	case key.Matches(msg, km.Commands):
		m.openCommandsDialog()
		return nil
	}

	if m.focus == appFocusEditor {
		switch {
		case m.busy && key.Matches(msg, km.Editor.Escape):
			return m.openCancelRunDialog()
		case m.busy && key.Matches(msg, km.Editor.OpenEditor):
			m.err = "agent is working, please wait"
			return nil
		}
		return m.input.Update(msg)
	}

	switch {
	case key.Matches(msg, km.Chat.Cancel):
		if m.busy {
			return m.openCancelRunDialog()
		}
	case key.Matches(msg, km.Tab):
		return m.setFocus(appFocusEditor)
	case key.Matches(msg, km.Chat.Reload):
		m.err = ""
		return m.reload()
	}

	if m.chat == nil {
		return nil
	}

	switch {
	case key.Matches(msg, km.Chat.Down):
		m.chat.SelectNext()
		return m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.Up):
		m.chat.SelectPrev()
		return m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.HalfPageDown):
		return m.chat.ScrollByAndAnimate(max(1, m.chat.Height()/2))
	case key.Matches(msg, km.Chat.HalfPageUp):
		return m.chat.ScrollByAndAnimate(-max(1, m.chat.Height()/2))
	case key.Matches(msg, km.Chat.PageDown):
		return m.chat.ScrollByAndAnimate(max(1, m.chat.Height()))
	case key.Matches(msg, km.Chat.PageUp):
		return m.chat.ScrollByAndAnimate(-max(1, m.chat.Height()))
	case key.Matches(msg, km.Chat.Home):
		m.chat.SelectFirst()
		return m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.End):
		m.chat.SelectLast()
		return m.chat.ScrollToSelectedAndAnimate()
	case key.Matches(msg, km.Chat.Expand):
		m.chat.ToggleExpandedSelectedItem()
		return nil
	case key.Matches(msg, km.Chat.Copy):
		content := strings.TrimSpace(m.chat.CopyContent())
		if content == "" {
			return nil
		}
		m.err = ""
		return tea.SetClipboard(content)
	default:
		if handled, cmd := m.chat.HandleKeyMsg(msg); handled {
			return cmd
		}
	}
	return nil
}

func (m *appModel) handleGlobalKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := m.input.KeyMap()
	switch {
	case key.Matches(msg, km.Quit):
		return true, tea.Quit
	case key.Matches(msg, km.Help):
		m.help.ShowAll = !m.help.ShowAll
		return true, nil
	default:
		return false, nil
	}
}

func (m *appModel) handleDialogInput(msg tea.Msg) tea.Cmd {
	if m.dialog == nil || !m.dialog.HasDialogs() {
		return nil
	}
	sourceID := ""
	if top := m.dialog.DialogLast(); top != nil {
		sourceID = top.ID()
	}
	fromCommands := m.commandsDialogRoot && m.dialog.ContainsDialog(surfacedialog.CommandsID)
	action := m.dialog.Update(msg)
	if action == nil {
		return nil
	}
	if command, ok := action.(surfacedialog.ActionRunControlplaneCommand); ok {
		if sourceID != "" && sourceID != surfacedialog.CommandsID {
			m.dialog.CloseDialog(sourceID)
		}
		return m.handleRunControlplaneCommand(command, fromCommands)
	}
	return m.update(action)
}

func (m *appModel) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	mouse := msg.Mouse()

	editorTop, editorBottom := m.frame.editorTop, m.frame.editorBottom
	if _, ok := msg.(tea.MouseClickMsg); ok && mouse.Y >= editorTop && mouse.Y < editorBottom {
		return m.setFocus(appFocusEditor)
	}

	if m.chat == nil {
		return nil
	}

	bodyTop, bodyBottom := m.frame.bodyTop, m.frame.bodyBottom
	if bodyBottom <= bodyTop {
		return nil
	}

	if !isMouseRelease(msg) && !isMouseMotion(msg) && (mouse.Y < bodyTop || mouse.Y >= bodyBottom) {
		return nil
	}

	handled, cmd := m.chat.HandleViewportMouse(msg, mouse.X, mouse.Y-bodyTop)
	if !handled {
		return nil
	}
	focusCmd := m.setFocus(appFocusChat)
	return tea.Batch(focusCmd, cmd)
}

func isMouseMotion(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseMotionMsg)
	return ok
}

func isMouseRelease(msg tea.MouseMsg) bool {
	_, ok := msg.(tea.MouseReleaseMsg)
	return ok
}
