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
