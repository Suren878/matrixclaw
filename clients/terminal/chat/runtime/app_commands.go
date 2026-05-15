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
	if isPlanOpenCommand(content) {
		m.returnToCommands = false
		return true, m.openPlanPanel()
	}
	if isDaemonRestartCommand(content) {
		m.returnToCommands = false
		return true, m.openServerRestartDialog()
	}
	if isContextCompactCommand(content) {
		m.returnToCommands = false
		m.startContextCompactProgress()
		return true, m.controlplaneCmd(content)
	}
	m.returnToCommands = false
	return true, m.controlplaneCmd(content)
}

func isPlanOpenCommand(command string) bool {
	return strings.EqualFold(strings.TrimSpace(command), "/plan")
}

func (m *appModel) controlplaneCmd(content string) tea.Cmd {
	return func() tea.Msg {
		dispatcher := controlplane.New(m.rt, m.workingDir)
		result, err := dispatcher.Handle(m.ctx, strings.TrimSpace(m.rt.config.ExternalKey), content)
		return controlplaneResultMsg{command: strings.TrimSpace(content), result: result, err: err}
	}
}
