package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/commandmenu"
	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
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
		m.closeAllDialogs()
		m.err = ""
		m.skipPlanResumeOnce = true
		m.planPanelOpen = !isPlanClearCommand(msg.command)
		return m.reloadSnapshotCmd()
	}
	if dialog := m.controlplaneDialog(msg.result); dialog != nil {
		m.showControlplaneResultDialog(dialog)
		if msg.result.ReloadSnapshot {
			return m.reloadSnapshotCmd()
		}
		return nil
	}
	if m.showControlplaneTextResult(msg.result) {
		if msg.result.ReloadSnapshot {
			return m.reloadSnapshotCmd()
		}
		return nil
	}
	if m.dialog.HasDialogs() {
		m.closeAllDialogs()
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

func (m *appModel) showControlplaneTextResult(result controlplane.Result) bool {
	text := strings.TrimSpace(result.Text)
	if text == "" || (!m.returnToCommands && !m.dialog.HasDialogs()) {
		return false
	}
	var closeAction surfacedialog.Action
	if m.returnToCommands {
		closeAction = surfacedialog.ActionOpenCommands{}
	}
	m.err = ""
	m.showControlplaneDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
		Title:       resultTitle(text),
		Text:        text,
		CloseAction: closeAction,
	}))
	return true
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
	case result.TextEdit != nil:
		return surfacedialog.NewTextEditCommand(m.com, *result.TextEdit)
	case result.Confirm != nil:
		return surfacedialog.NewConfirmCommand(m.com, *result.Confirm)
	case result.Info != nil:
		return surfacedialog.NewInfo(m.com, infoData(*result.Info))
	default:
		return nil
	}
}

func (m *appModel) controlplanePickerDialog(data controlplane.PickerData) surfacedialog.Dialog {
	picker := data
	view := controlplane.PickerView(picker, controlplane.PickerViewOptions{Surface: controlplane.SurfaceTerminal})
	closeAction := m.controlplanePickerCloseAction(picker, view)
	if m.controlplanePickerIsPopup(picker) {
		entries := commandmenu.PickerRows(view)
		return surfacedialog.NewPicker(m.com, surfacedialog.PickerData{
			ID:          surfacedialog.PickerID,
			Title:       view.Title,
			Meta:        strings.TrimSpace(view.Meta),
			Legend:      popupPickerLegend(picker),
			Filter:      surfacedialog.PickerNeedsFilter(entries),
			Entries:     entries,
			CloseAction: closeAction,
		})
	}
	entries := m.controlplanePickerEntries(picker, view)
	return surfacedialog.NewCommands(m.com, surfacedialog.CommandsData{
		Title:       view.Title,
		Meta:        strings.TrimSpace(view.Meta),
		Legend:      view.Legend,
		Entries:     entries,
		CloseAction: closeAction,
	})
}

func (m *appModel) controlplanePickerEntries(picker controlplane.PickerData, view controlplane.PickerViewData) []surfacedialog.PickerEntry {
	entries := commandmenu.PickerEntries(view)
	if m.controlplanePickerReturnsToCommands(picker, view) {
		entries = append(entries, surfacedialog.PickerEntry{
			ID:     "footer_back_commands",
			Title:  "Back",
			Role:   components.RoleBack,
			Footer: true,
			Action: surfacedialog.ActionOpenCommands{},
		})
	}
	return entries
}

func (m *appModel) controlplanePickerCloseAction(picker controlplane.PickerData, view controlplane.PickerViewData) surfacedialog.Action {
	if m.controlplanePickerReturnsToCommands(picker, view) {
		return surfacedialog.ActionOpenCommands{}
	}
	action := commandmenu.PickerCloseAction(view)
	return action
}

func (m *appModel) controlplanePickerReturnsToCommands(picker controlplane.PickerData, view controlplane.PickerViewData) bool {
	return m.returnToCommands && !picker.Popup && view.Footer == nil
}

func (m *appModel) controlplanePickerIsPopup(picker controlplane.PickerData) bool {
	return picker.Popup
}

func popupPickerLegend(picker controlplane.PickerData) string {
	switch picker.Kind {
	case controlplane.PickerPermissions:
		return "enter apply · esc close"
	default:
		return "enter select · esc close"
	}
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
	return surfacedialog.ActionRunControlplaneCommand{Command: command}
}

func (m *appModel) closeControlplaneDialogs() {
	m.dialog.CloseDialog(surfacedialog.CommandsID)
	m.commandsDialogRoot = false
	m.closeControlplaneTransientDialogs()
}

func (m *appModel) closeAllDialogs() {
	m.dialog.CloseAll()
	m.commandsDialogRoot = false
	m.returnToCommands = false
}

func (m *appModel) closeControlplaneTransientDialogs() {
	m.dialog.CloseDialog(surfacedialog.PickerID)
	m.dialog.CloseDialog(surfacedialog.FormCommandID)
	m.dialog.CloseDialog(surfacedialog.PromptCommandID)
	m.dialog.CloseDialog(surfacedialog.TextEditCommandID)
	m.dialog.CloseDialog(surfacedialog.ConfirmCommandID)
	m.dialog.CloseDialog(surfacedialog.InfoID)
}

func (m *appModel) showControlplaneDialog(dialog surfacedialog.Dialog) {
	m.err = ""
	if dialog == nil {
		return
	}
	nextID := dialog.ID()
	if nextID == surfacedialog.CommandsID {
		m.commandsDialogRoot = false
		m.closeControlplaneTransientDialogs()
	} else if nextID != surfacedialog.ConfirmCommandID {
		m.dialog.CloseDialog(surfacedialog.ConfirmCommandID)
	}
	top := m.dialog.DialogLast()
	if top != nil && top.ID() == nextID {
		m.dialog.ReplaceFrontDialog(dialog)
		return
	}
	m.dialog.CloseDialog(nextID)
	m.dialog.OpenDialog(dialog)
}

func (m *appModel) showControlplaneResultDialog(dialog surfacedialog.Dialog) {
	m.showControlplaneDialog(dialog)
}

func (m *appModel) reloadSnapshotCmd() tea.Cmd {
	if !m.dialog.HasDialogs() {
		m.returnToCommands = false
	}
	m.loading = true
	return m.loadInitialCmd()
}
