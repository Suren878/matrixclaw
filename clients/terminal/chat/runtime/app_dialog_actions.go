package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func (m *appModel) handlePermissionResponse(msg surfacedialog.ActionPermissionResponse) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.PermissionsID)
	m.dialog.suppressed[msg.Permission.ID] = struct{}{}
	request := core.ApprovalResolveRequest{}
	switch msg.Action {
	case surfacedialog.PermissionDenyWithReason:
		m.dialog.OpenDialog(surfacedialog.NewPromptCommand(m.com, controlplane.DenyWithReasonPrompt(msg.Permission.ID)))
		return nil
	case surfacedialog.PermissionAllow:
		request.Approved = true
	case surfacedialog.PermissionAlwaysSession:
		request.Approved, request.Always = true, permission.ScopeSession
	case surfacedialog.PermissionAlwaysGlobal:
		request.Approved, request.Always = true, permission.ScopeGlobal
	}
	return tea.Batch(m.resolveApprovalCmd(msg.Permission, request), m.syncPermissionDialogCmd())
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
	m.dialog.menuClosed()
	if m.input.busy {
		m.showWarning("agent is working, please wait")
		return nil
	}
	return m.input.Editor().OpenExternalEditor()
}

func (m *appModel) handleRunControlplaneCommand(msg surfacedialog.ActionRunControlplaneCommand) tea.Cmd {
	command := strings.TrimSpace(msg.Command)
	m.dialog.commandStarted()
	if strings.HasPrefix(command, "/update ") {
		return m.handleUpdateCommand(command)
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
		return m.openServerRestartDialog(false)
	}
	return tea.Batch(m.dialog.StartLoading(), m.controlplaneCmd(command))
}

func (m *appModel) handleResolveApproval(msg resolveApprovalMsg) tea.Cmd {
	if msg.err != nil {
		delete(m.dialog.suppressed, msg.approvalID)
		m.showError(msg.err.Error())
		return m.syncPermissionDialogCmd()
	}
	return m.syncPermissionDialogCmd()
}

func (m *appModel) handleCancelRunResult(msg cancelRunResultMsg) tea.Cmd {
	if msg.err != nil {
		m.showError(msg.err.Error())
		return nil
	}
	m.setBusy(runIsActive(&msg.run))
	if msg.run.Status == core.RunStatusCanceled {
		m.clearNotice()
	}
	return nil
}
