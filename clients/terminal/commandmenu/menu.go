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
	menu := controlplane.CommandMenuView(controlplane.SurfaceTerminal, controlplane.MenuState{
		SessionTitle:   state.SessionTitle,
		ProviderID:     state.ProviderID,
		ModelID:        state.ModelID,
		PermissionMode: state.PermissionMode,
		Capabilities:   state.Capabilities,
	})

	entries := make([]surfacedialog.CommandEntry, 0, 12)
	for _, item := range menu.Items {
		entries = append(entries, commandEntry(item))
	}
	if state.ExternalEditorAvailable {
		entries = append(entries, surfacedialog.CommandEntry{ID: "open_external_editor", Title: "External Editor", Shortcut: "ctrl+o", Action: surfacedialog.ActionExternalEditor{}})
	}
	entries = append(entries, surfacedialog.CommandEntry{ID: "quit", Title: "Exit", Role: components.RoleExit, Footer: true, Action: surfacedialog.ActionQuit{}})
	return entries
}

func commandEntry(item controlplane.ResultViewItem) surfacedialog.CommandEntry {
	return surfacedialog.CommandEntry{
		ID:       item.ID,
		Title:    item.Title,
		Status:   item.Info,
		Tone:     components.RowToneNormal,
		Disabled: item.Disabled,
		Action:   surfacedialog.ActionRunControlplaneCommand{Command: item.Command},
	}
}

func PickerEntries(view controlplane.PickerViewData) []surfacedialog.PickerEntry {
	return pickerEntries(view, true)
}

func PickerRows(view controlplane.PickerViewData) []surfacedialog.PickerEntry {
	return pickerEntries(view, false)
}

func pickerEntries(view controlplane.PickerViewData, includeFooter bool) []surfacedialog.PickerEntry {
	entries := make([]surfacedialog.PickerEntry, 0, len(view.Items)+1)
	for _, presented := range view.Items {
		if presented.SeparatorBefore && len(entries) > 0 && entries[len(entries)-1].Kind != surfacedialog.ListEntryDivider && entries[len(entries)-1].Kind != surfacedialog.ListEntryHeader {
			entries = append(entries, surfacedialog.PickerEntry{Kind: surfacedialog.ListEntryDivider, ID: "divider_" + presented.ID})
		}
		entries = append(entries, surfacedialog.PickerEntry{
			ID:       presented.ID,
			Title:    presented.Title,
			Status:   presented.Info,
			Search:   presented.Search,
			Role:     components.RoleNormal,
			Tone:     pickerEntryTone(presented),
			Selected: presented.Selected || presented.Focused,
			Disabled: presented.Disabled,
			Action:   pickerItemAction(presented),
		})
	}
	if includeFooter {
		if footer := pickerFooterEntry(view.Footer); footer != nil {
			entries = append(entries, *footer)
		}
	}
	return entries
}

func pickerEntryTone(presented controlplane.ResultViewItem) components.RowTone {
	if presented.Selected {
		return components.RowToneAccent
	}
	return components.RowToneNormal
}

func pickerFooterEntry(footer *controlplane.ResultViewFooter) *surfacedialog.PickerEntry {
	if footer == nil {
		return nil
	}
	if footer.Hidden {
		return nil
	}
	label := strings.TrimSpace(footer.Label)
	if label == "" {
		label = "Close"
	}
	role := components.RoleCancel
	if footer.Kind == controlplane.FooterBack {
		role = components.RoleBack
	}
	return &surfacedialog.PickerEntry{
		ID:     "footer_" + string(footer.Kind),
		Title:  label,
		Role:   role,
		Footer: true,
		Action: footerAction(footer),
	}
}

func pickerItemAction(item controlplane.ResultViewItem) surfacedialog.Action {
	if strings.TrimSpace(item.Command) == "" {
		return surfacedialog.ActionClose{}
	}
	return surfacedialog.ActionRunControlplaneCommand{Command: item.Command}
}

func PickerCloseAction(view controlplane.PickerViewData) surfacedialog.Action {
	return footerAction(view.Footer)
}

func footerAction(footer *controlplane.ResultViewFooter) surfacedialog.Action {
	if footer == nil {
		return surfacedialog.ActionClose{}
	}
	if command := strings.TrimSpace(footer.Command); command != "" {
		return surfacedialog.ActionRunControlplaneCommand{Command: command}
	}
	return surfacedialog.ActionClose{}
}
