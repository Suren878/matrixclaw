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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeChat()
		return m, m.input.SetWidth(m.editorWidth())
	case surfaceinput.SubmitMsg:
		return m, m.handleSubmit(msg)
	case surfaceinput.FocusMainMsg:
		return m, m.setFocus(appFocusChat)
	case surfaceinput.NewSessionMsg:
		return m, m.handleNewSession()
	case surfaceinput.OpenPlanMsg:
		return m, m.openPlanPanel()
	case surfaceinput.OpenCommandsMsg:
		m.openCommandsDialog()
		return m, nil
	case surfaceinput.AddImageMsg, surfaceinput.PasteImageMsg:
		m.handleAttachFiles()
		return m, nil
	case surfaceinput.QuitRequestMsg:
		return m, tea.Quit
	case surfaceeditor.OpenEditorMsg:
		return m, m.input.Update(msg)
	case surfaceeditor.HeightChangedMsg:
		return m, m.handleEditorHeightChanged()
	case surfaceeditor.ExternalEditorErrorMsg:
		m.err = msg.Err.Error()
		return m, nil
	case surfaceeditor.ExternalEditorWarningMsg:
		m.err = msg.Message
		return m, nil
	case surfacedialog.ActionPermissionResponse:
		return m, m.handlePermissionResponse(msg)
	case surfacedialog.ActionConfirmRunCancel:
		return m, m.handleConfirmRunCancel(msg)
	case surfacedialog.ActionOpenDiffPreview:
		m.handleOpenDiffPreview(msg)
		return m, nil
	case surfacedialog.ActionOpenFilePreview:
		m.handleOpenFilePreview(msg)
		return m, nil
	case surfacedialog.ActionExternalEditor:
		return m, m.handleExternalEditorAction()
	case surfacedialog.ActionOpenCommands:
		m.invalidateControlplaneResults()
		m.openCommandsDialog()
		return m, nil
	case surfacedialog.ActionRunControlplaneCommand:
		return m, m.handleRunControlplaneCommand(msg, m.commandsDialogRoot && m.dialog.ContainsDialog(surfacedialog.CommandsID))
	case surfacedialog.ActionQuit:
		return m, tea.Quit
	case surfacedialog.ActionCmd:
		return m, msg.Cmd
	case surfacedialog.ActionClose:
		m.invalidateControlplaneResults()
		if top := m.dialog.DialogLast(); top != nil && top.ID() == surfacedialog.CommandsID {
			m.commandsDialogRoot = false
		}
		m.dialog.CloseFrontDialog()
		return m, nil
	case controlplaneResultMsg:
		return m, m.handleControlplaneResult(msg)
	case tea.KeyPressMsg:
		if handled, cmd := m.handleGlobalKey(msg); handled {
			return m, cmd
		}
		if m.dialog.HasDialogs() {
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
		if m.focus == appFocusEditor {
			return m, m.input.Update(msg)
		}
		return m, nil
	case surfacemodel.DelayedClickMsg:
		if m.chat == nil {
			return m, nil
		}
		m.chat.HandleDelayedClick(msg)
		return m, nil
	case surfaceanim.StepMsg:
		if m.chat == nil {
			return m, nil
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
		return m, tea.Batch(cmds...)
	case workingTickMsg:
		m.now = msg.at
		if m.busy {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(workingSpinnerFrames)
		} else {
			m.spinnerFrame = 0
		}
		return m, m.workingTickCmd()
	case serverStatusRefreshMsg:
		m.handleServerStatusRefresh(msg)
		return m, nil
	case serverStatusTickMsg:
		return m, m.handleServerStatusTick()
	case serverRestartRequestMsg:
		m.handleServerRestartRequest(msg)
		return m, nil
	case serverRestartTickMsg:
		return m, m.handleServerRestartTick()
	case serverRestartPollMsg:
		return m, m.handleServerRestartPoll(msg)
	case serverRestartAckMsg:
		m.handleServerRestartAck(msg)
		return m, nil
	case terminalRestartMsg:
		return m, m.handleTerminalRestart(msg)
	case updateCheckMsg:
		m.handleUpdateCheck(msg)
		return m, nil
	case updateInstallMsg:
		m.handleUpdateInstall(msg)
		return m, nil
	case loadInitialMsg:
		return m, m.handleLoadInitial(msg)
	case subscribeReadyMsg:
		return m, m.handleSubscribeReady(msg)
	case liveEventMsg:
		return m, m.handleLiveEvent(msg)
	case resolveApprovalMsg:
		return m, m.handleResolveApproval(msg)
	case sendMessageResultMsg:
		return m, m.handleSendMessageResult(msg)
	case cancelRunResultMsg:
		return m, m.handleCancelRunResult(msg)
	case reconnectMsg:
		m.loading = true
		m.autoEditSessions = map[string]struct{}{}
		return m, m.loadInitialCmd()
	}
	if m.dialog.HasDialogs() {
		return m.handleDialogInput(msg)
	}
	if m.focus == appFocusEditor {
		return m, m.input.Update(msg)
	}
	return m, nil
}
