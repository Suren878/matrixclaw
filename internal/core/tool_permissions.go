package core

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func (c *Core) autoApprovesTool(ctx context.Context, prepared preparedToolCall, result tools.Result) (bool, error) {
	session, err := c.store.GetSession(ctx, prepared.SessionID)
	if err != nil {
		return false, err
	}
	switch NormalizePermissionMode(string(session.PermissionMode)) {
	case PermissionModeFullAuto:
		return true, nil
	case PermissionModeAcceptEdits:
		return acceptEditsAllows(prepared, result, session), nil
	default:
		return false, nil
	}
}

func acceptEditsAllows(prepared preparedToolCall, result tools.Result, session Session) bool {
	if result.Approval == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(prepared.ToolName)) {
	case "write", "edit", "multiedit":
	default:
		return false
	}
	root := firstNonEmpty(normalizeWorkingDir(session.WorkingDir), normalizeWorkingDir(prepared.WorkingDir))
	if root == "" {
		return false
	}
	path := strings.TrimSpace(result.Approval.Path)
	if result.FileVersion != nil && strings.TrimSpace(result.FileVersion.Path) != "" {
		path = result.FileVersion.Path
	}
	return pathWithinRoot(path, root)
}

func pathWithinRoot(path string, root string) bool {
	path = strings.TrimSpace(path)
	root = strings.TrimSpace(root)
	if path == "" || root == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath = filepath.Clean(absPath)
	absRoot = filepath.Clean(absRoot)
	if absPath == absRoot {
		return true
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}
