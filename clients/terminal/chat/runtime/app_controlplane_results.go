package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/commandmenu"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (m *appModel) handleControlplaneResult(msg controlplaneResultMsg) tea.Cmd {
	if msg.seq != 0 && msg.seq != m.controlplaneSeq {
		return nil
	}
	m.dialog.StopLoading()
	if isContextCompactCommand(msg.command) {
		return m.handleContextCompactResult(msg)
	}
	if msg.err != nil {
		m.showControlplaneError(msg.err)
		return nil
	}
	if isPlanSnapshotCommand(msg.command) && msg.result.ReloadSnapshot {
		m.dialog.CloseAll()
		m.err = ""
		m.skipPlanResumeOnce = true
		m.planPanelOpen = !isPlanClearCommand(msg.command)
		return m.reloadSnapshotCmd()
	}
	if dialog := m.controlplaneDialog(msg.result); dialog != nil {
		m.showControlplaneResultDialog(dialog, msg.result)
		if msg.result.ReloadSnapshot {
			return m.reloadSnapshotCmd()
		}
		return nil
	}
	if m.dialog.HasDialogs() {
		m.dialog.CloseAll()
	}
	m.returnToCommands = false
	m.err = strings.TrimSpace(msg.result.Text)
	if m.err != "" && !msg.result.ReloadSnapshot {
		m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
			Title: resultTitle(msg.result.Text),
			Text:  m.err,
		}))
		m.err = ""
	}
	if msg.result.ReloadSnapshot {
		return m.reloadSnapshotCmd()
	}
	return nil
}

func (m *appModel) showControlplaneError(err error) {
	if err == nil {
		return
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return
	}
	if m.dialog.ContainsDialog(surfacedialog.ConfirmCommandID) {
		for m.dialog.ContainsDialog(surfacedialog.ConfirmCommandID) {
			m.dialog.CloseDialog(surfacedialog.ConfirmCommandID)
		}
		m.err = ""
		m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
			Title: "Command Failed",
			Text:  text,
		}))
		return
	}
	m.err = text
}

func isPlanSnapshotCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	return strings.HasPrefix(command, "/plan goal ") ||
		strings.HasPrefix(command, "/plan add ") ||
		strings.HasPrefix(command, "/plan subtask ") ||
		strings.HasPrefix(command, "/plan edit ") ||
		strings.HasPrefix(command, "/plan done ") ||
		strings.HasPrefix(command, "/plan active ") ||
		strings.HasPrefix(command, "/plan skip ") ||
		isPlanClearCommand(command)
}

func isPlanClearCommand(command string) bool {
	return strings.EqualFold(strings.TrimSpace(command), "/plan clear confirm")
}

func (m *appModel) detachCommandsForControlplaneResult(result controlplane.Result) {
	if !m.returnToCommands || !m.dialog.ContainsDialog(surfacedialog.CommandsID) {
		return
	}
	if !controlplaneResultLeavesCommandRoot(result) {
		return
	}
	m.dialog.CloseDialog(surfacedialog.CommandsID)
	m.returnToCommands = false
}

func controlplaneResultLeavesCommandRoot(result controlplane.Result) bool {
	if picker := result.Picker; picker != nil {
		return picker.HasBack || picker.HasClose
	}
	return result.Form != nil || result.Prompt != nil || result.Confirm != nil || result.Info != nil
}

func (m *appModel) handleContextCompactResult(msg controlplaneResultMsg) tea.Cmd {
	if msg.err != nil {
		m.failContextCompactProgress(msg.err)
		return nil
	}
	text := strings.TrimSpace(msg.result.Text)
	if msg.result.ReloadSnapshot {
		m.completeContextCompactProgress(compactCompleteText)
		return m.reloadSnapshotCmd()
	}
	if text == "" {
		text = compactCompleteText
	}
	m.completeContextCompactProgress(text)
	return nil
}

func (m *appModel) controlplaneDialog(result controlplane.Result) surfacedialog.Dialog {
	switch {
	case result.Picker != nil:
		return m.controlplanePickerDialog(*result.Picker)
	case result.Form != nil:
		return surfacedialog.NewFormCommand(m.com, *result.Form)
	case result.Prompt != nil:
		return surfacedialog.NewPromptCommand(m.com, *result.Prompt)
	case result.Confirm != nil:
		return surfacedialog.NewConfirmCommand(m.com, *result.Confirm)
	case result.Info != nil:
		return surfacedialog.NewInfo(m.com, infoData(*result.Info))
	default:
		return nil
	}
}

func (m *appModel) controlplanePickerDialog(data controlplane.PickerData) surfacedialog.Dialog {
	picker := m.preparePicker(data)
	closeAction := m.pickerCloseAction(picker)
	entries := commandmenu.PickerEntriesWithCloseAction(picker, closeAction)
	return surfacedialog.NewPicker(m.com, surfacedialog.PickerData{
		ID:          surfacedialog.PickerID,
		Title:       commandmenu.PickerTitle(picker),
		Meta:        strings.TrimSpace(picker.Meta),
		Legend:      commandmenu.PickerLegend(picker),
		Filter:      surfacedialog.PickerNeedsFilter(entries),
		Entries:     entries,
		CloseAction: closeAction,
	})
}

func infoData(info controlplane.InfoData) surfacedialog.InfoData {
	return surfacedialog.InfoData{
		Title:       info.Title,
		Text:        info.Text,
		Rows:        info.Rows,
		CloseAction: controlplaneCloseAction(info.CloseCommand),
	}
}

func controlplaneCloseAction(command string) surfacedialog.Action {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return surfacedialog.ActionRunControlplaneCommand{Command: command, CloseSource: true}
}

func (m *appModel) preparePicker(picker controlplane.PickerData) controlplane.PickerData {
	if m.returnToCommands {
		picker.HideBackItem = false
	}
	return picker
}

func (m *appModel) pickerCloseAction(picker controlplane.PickerData) surfacedialog.Action {
	action := commandmenu.PickerCloseAction(picker)
	if _, closes := action.(surfacedialog.ActionClose); closes && m.returnToCommands && !m.dialog.ContainsDialog(surfacedialog.CommandsID) {
		return surfacedialog.ActionOpenCommands{}
	}
	return action
}

func (m *appModel) closeControlplaneDialogs() {
	m.dialog.CloseDialog(surfacedialog.CommandsID)
	m.dialog.CloseDialog(surfacedialog.PickerID)
	m.dialog.CloseDialog(surfacedialog.FormCommandID)
	m.dialog.CloseDialog(surfacedialog.PromptCommandID)
	m.dialog.CloseDialog(surfacedialog.ConfirmCommandID)
	m.dialog.CloseDialog(surfacedialog.InfoID)
}

func (m *appModel) showControlplaneDialog(dialog surfacedialog.Dialog) {
	m.err = ""
	if dialog == nil {
		return
	}
	nextID := dialog.ID()
	if nextID == surfacedialog.ConfirmCommandID {
		for m.dialog.ContainsDialog(surfacedialog.ConfirmCommandID) {
			m.dialog.CloseDialog(surfacedialog.ConfirmCommandID)
		}
	}
	if nextID != surfacedialog.ConfirmCommandID {
		for m.dialog.ContainsDialog(surfacedialog.ConfirmCommandID) {
			m.dialog.CloseDialog(surfacedialog.ConfirmCommandID)
		}
	}
	top := m.dialog.DialogLast()
	if top == nil {
		m.dialog.OpenDialog(dialog)
		return
	}
	topID := top.ID()
	switch {
	case topID == nextID:
		m.dialog.ReplaceFrontDialog(dialog)
		return
	case topID == surfacedialog.PickerID && nextID == surfacedialog.FormCommandID:
		if !m.dialog.ContainsDialog(surfacedialog.FormCommandID) {
			m.dialog.OpenDialog(dialog)
			return
		}
		m.dialog.CloseFrontDialog()
		m.dialog.CloseDialog(surfacedialog.FormCommandID)
	case topID == surfacedialog.PromptCommandID && nextID == surfacedialog.FormCommandID:
		m.dialog.CloseFrontDialog()
		m.dialog.CloseDialog(surfacedialog.FormCommandID)
	case topID == surfacedialog.PromptCommandID || topID == surfacedialog.ConfirmCommandID:
		m.dialog.CloseFrontDialog()
	}
	m.dialog.OpenDialog(dialog)
}

func (m *appModel) showControlplaneResultDialog(dialog surfacedialog.Dialog, result controlplane.Result) {
	if m.returnToCommands && controlplaneResultLeavesCommandRoot(result) {
		top := m.dialog.DialogLast()
		if top != nil && top.ID() == surfacedialog.CommandsID {
			m.err = ""
			m.dialog.ReplaceFrontDialog(dialog)
			m.returnToCommands = false
			return
		}
		m.detachCommandsForControlplaneResult(result)
	}
	m.showControlplaneDialog(dialog)
}

func (m *appModel) reloadSnapshotCmd() tea.Cmd {
	m.returnToCommands = false
	m.loading = true
	return m.loadInitialCmd()
}
