package core

import "strings"

func NormalizePermissionMode(mode string) PermissionMode {
	switch PermissionMode(strings.ToLower(strings.TrimSpace(mode))) {
	case PermissionModeAcceptEdits:
		return PermissionModeAcceptEdits
	case PermissionModeFullAuto:
		return PermissionModeFullAuto
	default:
		return PermissionModeDefault
	}
}

func PermissionModeForSessionApproval(toolName string) (PermissionMode, bool) {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "write", "edit", "multiedit", "multi_edit":
		return PermissionModeAcceptEdits, true
	default:
		return PermissionModeDefault, false
	}
}
