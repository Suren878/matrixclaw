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
	views := make(map[string]controlplane.CommandView)
	order := make([]string, 0, 12)
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
		views[item.ID] = item
		order = append(order, item.ID)
	}

	entries := make([]surfacedialog.CommandEntry, 0, 12)
	primary := []controlplane.CommandID{
		controlplane.CommandSessions,
		controlplane.CommandContext,
		controlplane.CommandProvider,
		controlplane.CommandPermissions,
	}
	used := make(map[string]bool, len(primary))
	for _, id := range primary {
		item, ok := views[string(id)]
		if !ok {
			continue
		}
		if id == controlplane.CommandContext {
			item.Title = "Compact"
			item.Command = "/context compact"
		}
		if id == controlplane.CommandProvider {
			item.Title = "Providers"
		}
		if id == controlplane.CommandPermissions {
			item.Title = "Permissions"
		}
		entries = append(entries, commandEntry(item))
		used[item.ID] = true
	}
	if len(entries) > 0 {
		entries = append(entries, surfacedialog.CommandEntry{Kind: surfacedialog.ListEntryDivider, ID: "divider_general"})
	}
	for _, id := range order {
		if used[id] {
			continue
		}
		entries = append(entries, commandEntry(views[id]))
	}
	if state.ExternalEditorAvailable {
		entries = append(entries, surfacedialog.CommandEntry{ID: "open_external_editor", Title: "External Editor", Shortcut: "ctrl+o", Action: surfacedialog.ActionExternalEditor{}})
	}
	entries = append(entries, surfacedialog.CommandEntry{ID: "quit", Title: "Exit", Role: commandui.RoleExit, Footer: true, Action: surfacedialog.ActionQuit{}})
	return entries
}

func commandEntry(item controlplane.CommandView) surfacedialog.CommandEntry {
	return surfacedialog.CommandEntry{
		ID:     item.ID,
		Title:  item.Title,
		Status: item.Status,
		Tone:   commandui.RowToneNormal,
		Action: surfacedialog.ActionRunControlplaneCommand{Command: item.Command},
	}
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
			ID:       presented.Item.ID,
			Title:    presented.Title,
			Status:   presented.Status,
			Role:     role,
			Tone:     tone,
			Selected: presented.Selected || presented.Item.Focused,
			Footer:   footer,
			Action:   pickerItemAction(presented, footer, closeAction),
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
