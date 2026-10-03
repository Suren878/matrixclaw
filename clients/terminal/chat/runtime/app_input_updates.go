package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) handleSubmit(msg surfaceinput.SubmitMsg) tea.Cmd {
	if handled, cmd := m.handleBusySubmitCommand(msg.Content); handled {
		return cmd
	}
	if handled, cmd := m.handleControlplaneSubmit(msg.Content, msg.Attachments); handled {
		return cmd
	}
	if strings.TrimSpace(m.session) == "" {
		m.err = "no active session"
		m.restoreEditorDraft(msg.Content, msg.Attachments)
		m.setBusy(false)
		return nil
	}
	mode := core.BusyInputMode("")
	if m.busy {
		mode = m.busyInputMode
	}
	m.err = ""
	m.setBusy(true)
	if m.chat != nil {
		m.chat.ScrollToBottom()
	}
	return m.sendMessageCmd(msg.Content, msg.Attachments, mode)
}

func (m *appModel) handleAttachFiles() {
	if err := m.attachFilesFromEditorValue(); err != nil {
		m.err = err.Error()
		return
	}
	m.err = ""
}

func (m *appModel) handleEditorHeightChanged() tea.Cmd {
	if m.chat != nil && m.chat.Follow() {
		return m.chat.ScrollToBottomAndAnimate()
	}
	return nil
}
