package runtime

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) handleSendMessageResult(msg sendMessageResultMsg) tea.Cmd {
	if msg.err != nil {
		m.setBusy(false)
		m.err = msg.err.Error()
		m.restoreEditorDraft(msg.content, msg.attachments)
		return nil
	}
	switch msg.result.Status {
	case core.AcceptRunStatusQueued, core.AcceptRunStatusSteered, core.AcceptRunStatusInterrupting:
		m.showAcceptedInputStatus(msg.result.Status)
	default:
		m.setBusy(runIsActive(&msg.result.Run))
	}
	return nil
}
