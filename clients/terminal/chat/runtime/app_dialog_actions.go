package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) handlePermissionResponse(msg surfacedialog.ActionPermissionResponse) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.PermissionsID)
	m.suppressedApprovals[msg.Permission.ID] = struct{}{}
	if msg.Action == surfacedialog.PermissionAllowSession {
		sessionID := strings.TrimSpace(msg.Permission.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(m.session)
		}
		if sessionID != "" {
			m.autoEditSessions[sessionID] = struct{}{}
		}
	}
	return tea.Batch(
		m.resolveApprovalCmd(
			msg.Permission,
			msg.Action != surfacedialog.PermissionDeny,
		),
		m.syncPermissionDialogCmd(),
	)
}

func (m *appModel) handleConfirmRunCancel(msg surfacedialog.ActionConfirmRunCancel) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.ConfirmRunCancelID)
	if !msg.Confirmed || strings.TrimSpace(msg.RunID) == "" {
		return nil
	}
	return m.cancelRunCmd(msg.RunID)
}

func (m *appModel) handleOpenDiffPreview(msg surfacedialog.ActionOpenDiffPreview) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.DiffPreviewID)
	m.dialog.OpenDialog(surfacedialog.NewDiffPreview(m.com, msg.Data))
	return nil
}

func (m *appModel) handleOpenFilePreview(msg surfacedialog.ActionOpenFilePreview) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.FilePreviewID)
	m.dialog.OpenDialog(surfacedialog.NewFilePreview(m.com, msg.Data))
	return nil
}

func (m *appModel) handleExternalEditorAction() tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.CommandsID)
	if m.busy {
		m.err = "agent is working, please wait"
		return nil
	}
	return m.input.Editor().OpenExternalEditor()
}

func (m *appModel) handleRunControlplaneCommand(msg surfacedialog.ActionRunControlplaneCommand) tea.Cmd {
	fromCommands := m.dialog.ContainsDialog(surfacedialog.CommandsID)
	if isContextCompactCommand(msg.Command) {
		m.dialog.CloseAll()
		m.err = ""
		m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
			Title: "Compact",
			Text:  compactProgressText,
		}))
		return m.controlplaneCmd(msg.Command)
	}
	if strings.TrimSpace(msg.Command) == "/server" {
		m.dialog.CloseDialog(surfacedialog.ServerStatusInfoID)
	}
	if strings.TrimSpace(msg.Command) == "/status" {
		return m.openServerStatusDialog()
	}
	if isDaemonRestartCommand(msg.Command) {
		return m.openServerRestartDialog()
	}
	if fromCommands {
		m.returnToCommands = true
	}
	m.closeControlplaneDialogs()
	return m.controlplaneCmd(msg.Command)
}

func (m *appModel) handleResolveApproval(msg resolveApprovalMsg) tea.Cmd {
	if msg.err != nil {
		delete(m.suppressedApprovals, msg.approvalID)
		m.err = msg.err.Error()
		return m.syncPermissionDialogCmd()
	}
	m.applyResolvedApproval(msg)
	return tea.Batch(m.syncPermissionDialogCmd(), m.loadInitialCmd())
}

func (m *appModel) handleCancelRunResult(msg cancelRunResultMsg) tea.Cmd {
	if msg.err != nil {
		m.err = msg.err.Error()
		return nil
	}
	m.setBusy(runIsActive(&msg.run))
	if msg.run.Status == core.RunStatusCanceled {
		m.err = ""
	}
	return m.loadInitialCmd()
}
