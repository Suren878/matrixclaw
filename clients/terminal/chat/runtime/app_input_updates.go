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
	m.err = ""
	m.setBusy(true)
	if m.chat != nil {
		m.chat.ScrollToBottom()
	}
	mode := core.BusyInputMode("")
	if m.busy {
		mode = normalizeLocalBusyInputMode(m.busyInputMode)
	}
	return m.sendMessageCmd(msg.Content, msg.Attachments, mode)
}

func (m *appModel) handleNewSession() tea.Cmd {
	if m.busy {
		m.err = "agent is busy, please wait before starting a new session"
		return nil
	}
	m.loading = true
	m.err = ""
	return m.createSessionCmd()
}

func (m *appModel) handleAttachFiles() tea.Cmd {
	if err := m.attachFilesFromEditorValue(); err != nil {
		m.err = err.Error()
		return nil
	}
	m.err = ""
	return nil
}

func (m *appModel) handleEditorHeightChanged() tea.Cmd {
	m.resizeChat()
	if m.chat != nil && m.chat.Follow() {
		return m.chat.ScrollToBottomAndAnimate()
	}
	return nil
}
