package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/commandmenu"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (m *appModel) handleControlplaneResult(msg controlplaneResultMsg) tea.Cmd {
	if msg.err != nil {
		m.err = msg.err.Error()
		return nil
	}
	if dialog := m.controlplaneDialog(msg.result); dialog != nil {
		m.showControlplaneDialog(dialog)
		if msg.result.ReloadSnapshot {
			m.returnToCommands = false
			m.loading = true
			return m.loadInitialCmd()
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
		m.returnToCommands = false
		m.loading = true
		return m.loadInitialCmd()
	}
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
	return surfacedialog.ActionRunControlplaneCommand{Command: command}
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

func (m *appModel) replaceControlplaneDialog(dialog surfacedialog.Dialog) {
	m.err = ""
	m.closeControlplaneDialogs()
	m.dialog.OpenDialog(dialog)
}

func (m *appModel) showControlplaneDialog(dialog surfacedialog.Dialog) {
	m.err = ""
	if dialog == nil {
		return
	}
	top := m.dialog.DialogLast()
	if top == nil {
		m.dialog.OpenDialog(dialog)
		return
	}
	topID := top.ID()
	nextID := dialog.ID()
	switch {
	case topID == nextID:
		m.dialog.CloseFrontDialog()
	case topID == surfacedialog.PromptCommandID && nextID == surfacedialog.FormCommandID:
		m.dialog.CloseFrontDialog()
		m.dialog.CloseDialog(surfacedialog.FormCommandID)
	case topID == surfacedialog.PromptCommandID || topID == surfacedialog.ConfirmCommandID:
		m.dialog.CloseFrontDialog()
	}
	m.dialog.OpenDialog(dialog)
}
