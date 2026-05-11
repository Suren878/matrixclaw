package commandmenu

import (
	"strings"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

type State struct {
	SessionTitle            string
	ProviderID              string
	ModelID                 string
	PermissionMode          core.PermissionMode
	ExternalEditorAvailable bool
}

func Entries(state State) []surfacedialog.CommandEntry {
	entries := make([]surfacedialog.CommandEntry, 0, 12)
	var secondary []surfacedialog.CommandEntry
	for _, item := range controlplane.BuildCommandView(controlplane.MenuState{
		SessionTitle:   state.SessionTitle,
		ProviderID:     state.ProviderID,
		ModelID:        state.ModelID,
		PermissionMode: state.PermissionMode,
	}) {
		if !item.Menu {
			continue
		}
		if item.ID == string(controlplane.CommandNewSession) {
			continue
		}
		entry := surfacedialog.CommandEntry{
			ID:     item.ID,
			Title:  item.Title,
			Status: item.Status,
			Tone:   commandui.RowToneNormal,
			Action: surfacedialog.ActionRunControlplaneCommand{Command: item.Command},
		}
		if item.Group == controlplane.MenuItemGroupSecondary {
			secondary = append(secondary, entry)
			continue
		}
		entries = append(entries, entry)
	}
	entries = append(entries, secondary...)
	if state.ExternalEditorAvailable {
		entries = append(entries, surfacedialog.CommandEntry{ID: "open_external_editor", Title: "External Editor", Shortcut: "ctrl+o", Action: surfacedialog.ActionExternalEditor{}})
	}
	entries = append(entries, surfacedialog.CommandEntry{ID: "quit", Title: "Exit", Role: commandui.RoleExit, Footer: true, Action: surfacedialog.ActionQuit{}})
	return entries
}

func PickerTitle(picker controlplane.PickerData) string {
	return controlplane.PickerPresentationTitle(picker)
}

func PickerLegend(picker controlplane.PickerData) string {
	return controlplane.PickerLegend(picker)
}

func PickerEntries(picker controlplane.PickerData) []surfacedialog.PickerEntry {
	return PickerEntriesWithCloseAction(picker, nil)
}

func PickerEntriesWithCloseAction(picker controlplane.PickerData, closeAction surfacedialog.Action) []surfacedialog.PickerEntry {
	entries := make([]surfacedialog.PickerEntry, 0, len(picker.Items)+2)
	for _, presented := range controlplane.PresentPickerItems(picker) {
		footer := presented.Navigation
		if footer {
			if picker.HideBackItem {
				continue
			}
		} else if presented.SeparatorBefore && len(entries) > 0 && entries[len(entries)-1].Kind != surfacedialog.ListEntryDivider && entries[len(entries)-1].Kind != surfacedialog.ListEntryHeader {
			entries = append(entries, surfacedialog.PickerEntry{Kind: surfacedialog.ListEntryDivider, ID: "divider_destructive"})
		}
		tone := commandui.RowToneNormal
		if presented.Selected && !footer {
			tone = commandui.RowToneAccent
		}
		role := commandui.RoleNormal
		if footer {
			role = commandui.RoleBack
		}
		entries = append(entries, surfacedialog.PickerEntry{
			ID:     presented.Item.ID,
			Title:  presented.Title,
			Status: presented.Status,
			Role:   role,
			Tone:   tone,
			Footer: footer,
			Action: pickerItemAction(presented, footer, closeAction),
		})
	}
	return entries
}

func pickerItemAction(item controlplane.PickerPresentationItem, footer bool, closeAction surfacedialog.Action) surfacedialog.Action {
	if strings.TrimSpace(item.Command) == "" {
		if closeAction != nil && footer {
			return closeAction
		}
		return surfacedialog.ActionClose{}
	}
	return surfacedialog.ActionRunControlplaneCommand{Command: item.Command}
}

func PickerCloseAction(picker controlplane.PickerData) surfacedialog.Action {
	command := controlplane.PickerCloseCommand(picker)
	if strings.TrimSpace(command) == "" {
		return surfacedialog.ActionClose{}
	}
	return surfacedialog.ActionRunControlplaneCommand{Command: command}
}
