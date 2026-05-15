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
		return m, m.openCommandsDialogCmd()
	case surfaceinput.AddImageMsg, surfaceinput.PasteImageMsg:
		return m, m.handleAttachFiles()
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
		return m, m.handleOpenDiffPreview(msg)
	case surfacedialog.ActionOpenFilePreview:
		return m, m.handleOpenFilePreview(msg)
	case surfacedialog.ActionExternalEditor:
		return m, m.handleExternalEditorAction()
	case surfacedialog.ActionOpenCommands:
		return m, m.openCommandsDialogCmd()
	case surfacedialog.ActionRunControlplaneCommand:
		return m, m.handleRunControlplaneCommand(msg)
	case surfacedialog.ActionQuit:
		return m, tea.Quit
	case surfacedialog.ActionCmd:
		return m, msg.Cmd
	case surfacedialog.ActionClose:
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
		return m, m.handleServerStatusRefresh(msg)
	case serverStatusTickMsg:
		return m, m.handleServerStatusTick()
	case serverRestartRequestMsg:
		return m, m.handleServerRestartRequest(msg)
	case serverRestartTickMsg:
		return m, m.handleServerRestartTick()
	case serverRestartPollMsg:
		return m, m.handleServerRestartPoll(msg)
	case serverRestartAckMsg:
		return m, m.handleServerRestartAck(msg)
	case updateCheckMsg:
		return m, m.handleUpdateCheck(msg)
	case updateInstallMsg:
		return m, m.handleUpdateInstall(msg)
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
