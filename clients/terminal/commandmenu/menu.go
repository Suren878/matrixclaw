package commandmenu

import (
	"strings"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

type State struct {
	SessionTitle            string
	ProviderID              string
	ModelID                 string
	PermissionMode          core.PermissionMode
	Capabilities            core.SessionCapabilities
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
		Capabilities:   state.Capabilities,
	}) {
		if !item.Menu {
			continue
		}
		if item.ID == string(controlplane.CommandNewSession) || item.ID == string(controlplane.CommandMemory) {
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
	entries = append(entries, surfacedialog.CommandEntry{ID: "quit", Title: "Exit", Role: components.RoleExit, Footer: true, Action: surfacedialog.ActionQuit{}})
	return entries
}

func commandEntry(item controlplane.CommandView) surfacedialog.CommandEntry {
	return surfacedialog.CommandEntry{
		ID:       item.ID,
		Title:    item.Title,
		Status:   item.Status,
		Tone:     components.RowToneNormal,
		Disabled: item.Disabled,
		Action:   surfacedialog.ActionRunControlplaneCommand{Command: item.Command},
	}
}

func PickerTitle(picker controlplane.PickerData) string {
	return controlplane.PickerPresentationTitle(picker)
}

func PickerLegend(picker controlplane.PickerData) string {
	return controlplane.PickerLegend(picker)
}

func PickerEntriesWithCloseAction(picker controlplane.PickerData, closeAction surfacedialog.Action) []surfacedialog.PickerEntry {
	return pickerEntries(picker, closeAction, true, false)
}

func PickerRows(picker controlplane.PickerData) []surfacedialog.PickerEntry {
	return pickerEntries(picker, nil, false, true)
}

func pickerEntries(picker controlplane.PickerData, closeAction surfacedialog.Action, includeFooter bool, closeOnSelect bool) []surfacedialog.PickerEntry {
	entries := make([]surfacedialog.PickerEntry, 0, len(picker.Items)+2)
	for _, presented := range controlplane.PresentPickerItems(picker) {
		if presented.SeparatorBefore && len(entries) > 0 && entries[len(entries)-1].Kind != surfacedialog.ListEntryDivider && entries[len(entries)-1].Kind != surfacedialog.ListEntryHeader {
			entries = append(entries, surfacedialog.PickerEntry{Kind: surfacedialog.ListEntryDivider, ID: "divider_destructive"})
		}
		entries = append(entries, surfacedialog.PickerEntry{
			ID:       presented.Item.ID,
			Title:    presented.Title,
			Status:   presented.Status,
			Search:   presented.Search,
			Role:     components.RoleNormal,
			Tone:     pickerEntryTone(presented),
			Selected: presented.Selected || presented.Item.Focused,
			Disabled: presented.Disabled,
			Action:   pickerItemAction(presented, closeOnSelect),
		})
	}
	if includeFooter {
		if footer := pickerFooterEntry(picker, closeAction); footer != nil {
			entries = append(entries, *footer)
		}
	}
	return entries
}

func pickerEntryTone(presented controlplane.PickerPresentationItem) components.RowTone {
	if presented.Selected {
		return components.RowToneAccent
	}
	return components.RowToneNormal
}

func pickerFooterEntry(picker controlplane.PickerData, closeAction surfacedialog.Action) *surfacedialog.PickerEntry {
	action := closeAction
	if action == nil {
		action = PickerCloseAction(picker)
	}
	label := "Back"
	if picker.HasClose && !picker.HasBack {
		label = "Close"
	}
	if action == nil {
		action = surfacedialog.ActionClose{}
	}
	if _, closes := action.(surfacedialog.ActionClose); closes && !picker.HasBack && !picker.HasClose {
		return nil
	}
	if _, opensCommands := action.(surfacedialog.ActionOpenCommands); opensCommands {
		label = "Back"
	}
	return &surfacedialog.PickerEntry{
		ID:     "footer_back",
		Title:  label,
		Role:   components.RoleBack,
		Footer: true,
		Action: action,
	}
}

func pickerItemAction(item controlplane.PickerPresentationItem, closeSource bool) surfacedialog.Action {
	if strings.TrimSpace(item.Command) == "" {
		return surfacedialog.ActionClose{}
	}
	return surfacedialog.ActionRunControlplaneCommand{Command: item.Command, CloseSource: closeSource}
}

func PickerCloseAction(picker controlplane.PickerData) surfacedialog.Action {
	if !picker.HasBack && !picker.HasClose {
		return surfacedialog.ActionClose{}
	}
	command := controlplane.PickerCloseCommand(picker)
	if strings.TrimSpace(command) == "" {
		return surfacedialog.ActionClose{}
	}
	return surfacedialog.ActionRunControlplaneCommand{Command: command}
}
