package core

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

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
	spec, ok := tools.CoreSpec(toolName)
	if ok && spec.IsFilesystemMutation() {
		return PermissionModeAcceptEdits, true
	}
	return PermissionModeDefault, false
}
