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

func Entries(state State) []surfacedialog.PickerEntry {
	items := controlplane.Menu(controlplane.MenuState{
		SessionTitle:   state.SessionTitle,
		ProviderID:     state.ProviderID,
		ModelID:        state.ModelID,
		PermissionMode: state.PermissionMode,
		Capabilities:   state.Capabilities,
	})
	entries := make([]surfacedialog.PickerEntry, 0, len(items)+2)
	for _, item := range items {
		entries = append(entries, surfacedialog.PickerEntry{
			ID:       item.ID,
			Title:    item.Title,
			Status:   item.Info,
			Tone:     components.RowToneNormal,
			Disabled: item.Disabled,
			Action:   surfacedialog.ActionRunControlplaneCommand{Command: item.Command},
		})
	}
	if state.ExternalEditorAvailable {
		entries = append(entries, surfacedialog.PickerEntry{ID: "open_external_editor", Title: "External Editor", Shortcut: "ctrl+o", Action: surfacedialog.ActionExternalEditor{}})
	}
	entries = append(entries, surfacedialog.PickerEntry{ID: "quit", Title: "Exit", Role: components.RoleExit, Footer: true, Action: surfacedialog.ActionQuit{}})
	return entries
}

// PickerEntries are a picker's rows, with a line above actions and danger
// rows, and its Back button when it has one.
func PickerEntries(picker controlplane.PickerData) []surfacedialog.PickerEntry {
	entries := make([]surfacedialog.PickerEntry, 0, len(picker.Items)+1)
	for _, item := range picker.Items {
		if item.NeedsSeparator() && len(entries) > 0 {
			entries = append(entries, surfacedialog.PickerEntry{Kind: surfacedialog.ListEntryDivider, ID: "divider_" + item.ID})
		}
		tone := components.RowToneNormal
		if item.Selected {
			tone = components.RowToneAccent
		}
		var action surfacedialog.Action = surfacedialog.ActionClose{}
		if command := strings.TrimSpace(item.Command); command != "" {
			action = surfacedialog.ActionRunControlplaneCommand{Command: command}
		}
		entries = append(entries, surfacedialog.PickerEntry{
			ID:       item.ID,
			Title:    firstNonEmpty(item.Title, item.ID),
			Status:   strings.TrimSpace(item.Info),
			Search:   firstNonEmpty(item.Search, item.Title+" "+item.Info),
			Role:     components.RoleNormal,
			Tone:     tone,
			Selected: item.Selected || item.Focused,
			Disabled: item.Disabled,
			Action:   action,
		})
	}
	if picker.Back != "" {
		entries = append(entries, surfacedialog.PickerEntry{
			ID:     "footer_back",
			Title:  "Back",
			Role:   components.RoleBack,
			Footer: true,
			Action: surfacedialog.ActionRunControlplaneCommand{Command: picker.Back},
		})
	}
	return entries
}

// PickerCloseAction is what dismissing the picker does: go back, run its
// close command, or just close.
func PickerCloseAction(picker controlplane.PickerData) surfacedialog.Action {
	if command := firstNonEmpty(picker.Back, picker.Close); command != "" {
		return surfacedialog.ActionRunControlplaneCommand{Command: command}
	}
	return surfacedialog.ActionClose{}
}

// PickerLegend names the keys of a picker opened as a menu.
func PickerLegend(kind controlplane.PickerKind) string {
	switch kind {
	case controlplane.PickerSessions:
		return "enter open · esc back"
	case controlplane.PickerSessionActions, controlplane.PickerProviderActions, controlplane.PickerContext, controlplane.PickerTasks, controlplane.PickerTaskActions, controlplane.PickerTaskArchive, controlplane.PickerServer:
		return "enter run · esc back"
	case controlplane.PickerPermissions:
		return "enter apply · esc back"
	default:
		return "enter select · esc back"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
