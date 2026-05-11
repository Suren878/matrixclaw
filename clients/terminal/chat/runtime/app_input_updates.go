package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
)

func (m *appModel) handleSubmit(msg surfaceinput.SubmitMsg) tea.Cmd {
	if handled, cmd := m.handleControlplaneSubmit(msg.Content, msg.Attachments); handled {
		return cmd
	}
	if m.busy {
		m.err = "agent is busy, please wait"
		m.restoreEditorDraft(msg.Content, msg.Attachments)
		return nil
	}
	if strings.TrimSpace(m.session) == "" {
		m.err = "no active session"
		m.restoreEditorDraft(msg.Content, msg.Attachments)
		m.setBusy(false)
		return nil
	}
	m.err = ""
	m.setBusy(true)
	return m.sendMessageCmd(msg.Content, msg.Attachments)
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
