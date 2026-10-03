package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (m *appModel) handleControlplaneSubmit(content string, attachments []surfaceeditor.Attachment) (bool, tea.Cmd) {
	if strings.TrimSpace(content) == "" || len(attachments) > 0 || m.rt == nil {
		return false, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(content), "/") {
		return false, nil
	}
	if strings.TrimSpace(content) == "/status" {
		m.dialog.menu = menuNone
		return true, m.openServerStatusDialog()
	}
	if isDaemonRestartCommand(content) {
		m.dialog.menu = menuNone
		return true, m.openServerRestartDialog(false)
	}
	if isContextCompactCommand(content) {
		m.dialog.menu = menuNone
		m.startContextCompactProgress()
		return true, m.controlplaneCmd(content)
	}
	m.dialog.menu = menuNone
	return true, m.controlplaneCmd(content)
}

func (m *appModel) controlplaneCmd(content string) tea.Cmd {
	seq := m.nextControlplaneSeq()
	return func() tea.Msg {
		result, err := controlplane.New(m.rt.client, m.workingDir).Handle(m.ctx, content)
		return controlplaneResultMsg{command: strings.TrimSpace(content), seq: seq, result: result, err: err}
	}
}

func (m *appModel) nextControlplaneSeq() uint64 {
	m.dialog.seq++
	return m.dialog.seq
}

func (m *appModel) invalidateControlplaneResults() {
	m.dialog.seq++
	m.dialog.StopLoading()
}
