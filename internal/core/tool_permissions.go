package core

import (
	"context"
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
	if !prepared.Spec.IsFilesystemMutation() {
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
	return mutationPathWithinRoot(path, root)
}

func mutationPathWithinRoot(path string, root string) bool {
	path = strings.TrimSpace(path)
	root = strings.TrimSpace(root)
	if path == "" || root == "" {
		return false
	}
	policy, err := tools.ResolveFilesystemPath(root, path)
	if err != nil {
		return false
	}
	return policy.WithinWorkingDir
}
