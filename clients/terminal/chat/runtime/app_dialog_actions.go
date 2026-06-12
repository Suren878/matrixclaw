package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) handlePermissionResponse(msg surfacedialog.ActionPermissionResponse) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.PermissionsID)
	m.suppressedApprovals[msg.Permission.ID] = struct{}{}
	if msg.Action == surfacedialog.PermissionAllowSession && surfacepermission.CanAllowSessionApproval(msg.Permission) {
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

func (m *appModel) handleOpenDiffPreview(msg surfacedialog.ActionOpenDiffPreview) {
	m.dialog.CloseDialog(surfacedialog.DiffPreviewID)
	m.dialog.OpenDialog(surfacedialog.NewDiffPreview(m.com, msg.Data))
}

func (m *appModel) handleOpenFilePreview(msg surfacedialog.ActionOpenFilePreview) {
	m.dialog.CloseDialog(surfacedialog.FilePreviewID)
	m.dialog.OpenDialog(surfacedialog.NewFilePreview(m.com, msg.Data))
}

func (m *appModel) handleExternalEditorAction() tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.CommandsID)
	m.commandsDialogRoot = false
	if m.busy {
		m.err = "agent is working, please wait"
		return nil
	}
	return m.input.Editor().OpenExternalEditor()
}

func (m *appModel) handleRunControlplaneCommand(msg surfacedialog.ActionRunControlplaneCommand, fromCommands bool) tea.Cmd {
	command := strings.TrimSpace(msg.Command)
	m.returnToCommands = m.controlplaneCommandReturnsToCommands(fromCommands)
	if strings.HasPrefix(command, "/update ") {
		return m.handleUpdateCommand(command)
	}
	if cmd, handled := m.handlePlanPromptCommand(command); handled {
		return cmd
	}
	switch command {
	case "/plan":
		m.closeAllDialogs()
		m.returnToCommands = false
		return m.openPlanPanel()
	case "/plan run":
		m.closeAllDialogs()
		return m.startPlanRunCmd()
	case "/plan cancel":
		m.closeAllDialogs()
		m.planAutoRun = false
		m.planPanelOpen = false
		if m.focus == appFocusPlan {
			_ = m.setFocus(appFocusEditor)
		}
		return m.controlplaneCmd("/plan clear confirm")
	}
	if isContextCompactCommand(command) {
		m.closeAllDialogs()
		m.startContextCompactProgress()
		return m.controlplaneCmd(command)
	}
	if command == "/server" {
		m.dialog.CloseDialog(surfacedialog.ServerStatusInfoID)
	}
	if command == "/status" {
		return m.openServerStatusDialog()
	}
	if isDaemonRestartCommand(command) {
		return m.openServerRestartDialog()
	}
	return tea.Batch(m.dialog.StartLoading(), m.controlplaneCmd(command))
}

func (m *appModel) controlplaneCommandReturnsToCommands(fromCommands bool) bool {
	if fromCommands {
		return true
	}
	return m.returnToCommands && m.dialog.HasDialogs()
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
