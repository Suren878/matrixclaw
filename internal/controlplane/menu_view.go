package controlplane

import (
	"slices"
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

// Menu is the command menu of the terminal and of Telegram's /help: the
// menu commands, the session's ones first, with their current state.
func Menu(state MenuState) []PickerItem {
	first := []CommandID{CommandSessions, CommandContext, CommandProvider, CommandPermissions}
	titles := map[CommandID]string{CommandProvider: "Providers", CommandPermissions: "Permissions"}
	var lead, rest []PickerItem
	for _, spec := range Catalog() {
		if !spec.Public || !spec.Menu || spec.ID == CommandNewSession || spec.ID == CommandMemory {
			continue
		}
		item := PickerItem{ID: string(spec.ID), Title: spec.Title, Command: spec.Command}
		if title, ok := titles[spec.ID]; ok {
			item.Title = title
		}
		item.Info, item.Disabled = menuStatus(spec.ID, state)
		if item.Disabled {
			item.Command = ""
		}
		if index := slices.Index(first, spec.ID); index >= 0 {
			lead = append(lead, item)
		} else {
			rest = append(rest, item)
		}
	}
	slices.SortStableFunc(lead, func(a, b PickerItem) int {
		return slices.Index(first, CommandID(a.ID)) - slices.Index(first, CommandID(b.ID))
	})
	return append(lead, rest...)
}

func menuStatus(id CommandID, state MenuState) (string, bool) {
	limited := hasSessionCapabilities(state.Capabilities)
	switch id {
	case CommandSessions:
		return strings.TrimSpace(state.SessionTitle), false
	case CommandProvider:
		if limited && !state.Capabilities.ProviderSelection {
			return "Matrixclaw only", true
		}
		return strings.TrimSpace(state.ProviderID), false
	case CommandPermissions:
		if limited && !state.Capabilities.PermissionMode {
			return "Matrixclaw only", true
		}
		return permissionModeStatus(state.PermissionMode), false
	case CommandTodo:
		if limited && !state.Capabilities.NativeTools {
			return "Matrixclaw only", true
		}
	}
	return "", false
}

func hasSessionCapabilities(capabilities core.SessionCapabilities) bool {
	return capabilities.ProviderSelection ||
		capabilities.PermissionMode ||
		capabilities.NativeTools ||
		capabilities.ExternalAgent
}

func commandMenuPicker() *PickerData {
	return NewPickerData(PickerCommandMenu, "Menu").Command(helpCommand()).Items(Menu(MenuState{})...).Ptr()
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
