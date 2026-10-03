package runtime

import (
	tea "charm.land/bubbletea/v2"

	surfaceanim "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
	surfacemodel "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/model"
)

func (m *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.relayout()
	return m, cmd
}

func (m *appModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.input.SetWidth(m.editorWidth())
	case surfaceinput.SubmitMsg:
		return m.handleSubmit(msg)
	case surfaceinput.FocusMainMsg:
		return m.setFocus(appFocusChat)
	case surfaceinput.OpenCommandsMsg:
		m.openCommandsDialog()
		return nil
	case surfaceinput.AttachFilesMsg:
		m.handleAttachFiles()
		return nil
	case surfaceinput.QuitRequestMsg:
		return tea.Quit
	case surfaceeditor.OpenEditorMsg:
		return m.input.Update(msg)
	case surfaceeditor.HeightChangedMsg:
		return m.handleEditorHeightChanged()
	case surfaceeditor.ExternalEditorErrorMsg:
		m.showError(msg.Err.Error())
		return nil
	case surfaceeditor.ExternalEditorWarningMsg:
		m.showWarning(msg.Message)
		return nil
	case surfacedialog.ActionPermissionResponse:
		return m.handlePermissionResponse(msg)
	case surfacedialog.ActionConfirmRunCancel:
		return m.handleConfirmRunCancel(msg)
	case surfacedialog.ActionOpenDiffPreview:
		m.handleOpenDiffPreview(msg)
		return nil
	case surfacedialog.ActionOpenFilePreview:
		m.handleOpenFilePreview(msg)
		return nil
	case surfacedialog.ActionExternalEditor:
		return m.handleExternalEditorAction()
	case surfacedialog.ActionOpenCommands:
		m.invalidateControlplaneResults()
		m.openCommandsDialog()
		return nil
	case surfacedialog.ActionRunControlplaneCommand:
		return m.handleRunControlplaneCommand(msg)
	case surfacedialog.ActionQuit:
		return tea.Quit
	case surfacedialog.ActionCmd:
		return msg.Cmd
	case surfacedialog.ActionClose:
		m.invalidateControlplaneResults()
		top := m.dialog.DialogLast()
		if top != nil && top.ID() == surfacedialog.CommandsID {
			m.dialog.menuClosed()
		}
		denying := m.denyingApproval()
		m.dialog.CloseFrontDialog()
		if denying != "" {
			// The reason prompt was closed without denying: ask again.
			delete(m.dialog.suppressed, denying)
			return m.syncPermissionDialogCmd()
		}
		return nil
	case controlplaneResultMsg:
		return m.handleControlplaneResult(msg)
	case tea.KeyPressMsg:
		if handled, cmd := m.handleGlobalKey(msg); handled {
			return cmd
		}
		if m.dialog.HasDialogs() {
			if m.dialog.keyGuarded() {
				return nil
			}
			return m.handleDialogInput(msg)
		}
		return m.handleKey(msg)
	case tea.MouseMsg:
		if m.dialog.HasDialogs() {
			return m.handleDialogInput(msg)
		}
		return m.handleMouse(msg)
	case tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
		if m.dialog.HasDialogs() {
			return m.handleDialogInput(msg)
		}
		if m.input.focus == appFocusEditor {
			return m.input.Update(msg)
		}
		return nil
	case surfacemodel.DelayedClickMsg:
		if m.chat == nil {
			return nil
		}
		m.chat.HandleDelayedClick(msg)
		return nil
	case surfaceanim.StepMsg:
		if m.chat == nil {
			return nil
		}
		cmds := make([]tea.Cmd, 0, 2)
		if cmd := m.chat.Animate(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if m.chat.Follow() {
			if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return tea.Batch(cmds...)
	case workingTickMsg:
		m.now = msg.at
		if m.input.busy {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(workingSpinnerFrames)
		} else {
			m.spinnerFrame = 0
		}
		return m.workingTickCmd()
	case serverStatusRefreshMsg:
		m.handleServerStatusRefresh(msg)
		return nil
	case serverStatusTickMsg:
		return m.handleServerStatusTick()
	case serverRestartRequestMsg:
		m.handleServerRestartRequest(msg)
		return nil
	case serverRestartTickMsg:
		return m.handleServerRestartTick()
	case serverRestartPollMsg:
		return m.handleServerRestartPoll(msg)
	case serverRestartAckMsg:
		m.handleServerRestartAck(msg)
		return nil
	case terminalRestartMsg:
		return m.handleTerminalRestart(msg)
	case updateCheckMsg:
		m.handleUpdateCheck(msg)
		return nil
	case updateInstallMsg:
		m.handleUpdateInstall(msg)
		return nil
	case connectedMsg:
		return m.handleConnected(msg)
	case snapshotMsg:
		return m.handleSnapshot(msg)
	case liveEventMsg:
		return m.handleLiveEvent(msg)
	case resolveApprovalMsg:
		return m.handleResolveApproval(msg)
	case sendMessageResultMsg:
		return m.handleSendMessageResult(msg)
	case cancelRunResultMsg:
		return m.handleCancelRunResult(msg)
	case reconnectMsg:
		return m.reload()
	}
	if m.dialog.HasDialogs() {
		return m.handleDialogInput(msg)
	}
	if m.input.focus == appFocusEditor {
		return m.input.Update(msg)
	}
	return nil
}
