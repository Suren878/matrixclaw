package runtime

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) handleSendMessageResult(msg sendMessageResultMsg) tea.Cmd {
	if msg.err != nil {
		m.setBusy(false)
		m.showError(msg.err.Error())
		m.restoreEditorDraft(msg.content, msg.attachments)
		return nil
	}
	switch msg.result.Status {
	case core.AcceptRunStatusQueued, core.AcceptRunStatusSteered, core.AcceptRunStatusInterrupting:
		m.showAcceptedInputStatus(msg.result.Status)
	default:
		m.setBusy(runIsActive(m.acceptedRun(msg.result.Run)))
	}
	return nil
}

// acceptedRun prefers the read model's copy of the run: its events may
// already have ended the run the accept reply still calls accepted.
func (m *appModel) acceptedRun(run core.Run) *core.Run {
	if m.read != nil {
		if known := m.read.Run(); known != nil && known.ID == run.ID {
			return known
		}
	}
	return &run
}
