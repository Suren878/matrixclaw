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
	command := strings.TrimSpace(msg.Command)
	if strings.HasPrefix(command, "/update ") {
		return m.handleUpdateCommand(command)
	}
	if cmd, handled := m.handlePlanPromptCommand(command); handled {
		return cmd
	}
	switch command {
	case "/plan":
		m.dialog.CloseAll()
		m.returnToCommands = false
		return m.openPlanPanel()
	case "/plan run":
		m.dialog.CloseAll()
		return m.startPlanRunCmd()
	case "/plan cancel":
		m.dialog.CloseAll()
		m.planAutoRun = false
		m.planPanelOpen = false
		if m.focus == appFocusPlan {
			_ = m.setFocus(appFocusEditor)
		}
		return m.controlplaneCmd("/plan clear confirm")
	}
	if isContextCompactCommand(command) {
		m.dialog.CloseAll()
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
	if fromCommands {
		m.returnToCommands = true
	}
	return tea.Batch(m.dialog.StartLoading(), m.controlplaneCmd(command))
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
