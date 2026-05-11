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
		m.returnToCommands = false
		return true, m.openServerStatusDialog()
	}
	if isDaemonRestartCommand(content) {
		m.returnToCommands = false
		return true, m.openServerRestartDialog()
	}
	m.returnToCommands = false
	return true, m.controlplaneCmd(content)
}

func (m *appModel) controlplaneCmd(content string) tea.Cmd {
	return func() tea.Msg {
		dispatcher := controlplane.New(m.rt, m.workingDir)
		result, err := dispatcher.Handle(m.ctx, strings.TrimSpace(m.rt.config.ExternalKey), content)
		return controlplaneResultMsg{result: result, err: err}
	}
}
