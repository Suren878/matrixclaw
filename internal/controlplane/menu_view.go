package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

type MenuState struct {
	SessionTitle   string
	ProviderID     string
	ModelID        string
	PermissionMode core.PermissionMode
	Capabilities   core.SessionCapabilities
}

type CommandView struct {
	ID       string
	Command  string
	Title    string
	Status   string
	Group    MenuItemGroup
	Public   bool
	Menu     bool
	Disabled bool
}

func BuildCommandView(state MenuState) []CommandView {
	items := make([]CommandView, 0, len(Catalog()))
	for _, spec := range Catalog() {
		status := ""
		disabled := false
		switch spec.ID {
		case CommandSessions:
			status = strings.TrimSpace(state.SessionTitle)
		case CommandProvider:
			if !state.Capabilities.ProviderSelection && hasSessionCapabilities(state.Capabilities) {
				status = "Matrixclaw only"
				disabled = true
			} else {
				status = strings.TrimSpace(state.ProviderID)
			}
		case CommandPermissions:
			if !state.Capabilities.PermissionMode && hasSessionCapabilities(state.Capabilities) {
				status = "Matrixclaw only"
				disabled = true
			} else {
				status = permissionModeStatus(state.PermissionMode)
			}
		case CommandTodo:
			if !state.Capabilities.NativeTools && hasSessionCapabilities(state.Capabilities) {
				status = "Matrixclaw only"
				disabled = true
			}
		}
		items = append(items, CommandView{
			ID:       string(spec.ID),
			Command:  spec.Command,
			Title:    spec.Title,
			Status:   status,
			Group:    spec.Group,
			Public:   spec.Public,
			Menu:     spec.Menu,
			Disabled: disabled,
		})
	}
	return items
}

func hasSessionCapabilities(capabilities core.SessionCapabilities) bool {
	return capabilities.ProviderSelection ||
		capabilities.PermissionMode ||
		capabilities.NativeTools ||
		capabilities.ExternalAgent
}

func CommandMenuPicker(state MenuState) *PickerData {
	picker := NewPickerData(PickerCommandMenu, "Menu")
	for _, view := range CommandMenuView(SurfaceTelegram, state).Items {
		item := PickerItem{
			ID:       view.ID,
			Title:    view.Title,
			Info:     view.Info,
			Command:  view.Command,
			Disabled: view.Disabled,
		}
		if view.Disabled {
			item.Command = ""
		}
		picker.Item(item)
	}
	return picker.Ptr()
}

func permissionModeStatus(mode core.PermissionMode) string {
	switch core.NormalizePermissionMode(string(mode)) {
	case core.PermissionModeAcceptEdits:
		return "Edits only"
	case core.PermissionModeFullAuto:
		return "Full auto"
	default:
		return "Ask first"
	}
}
