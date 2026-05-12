package telegram

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func (w *Worker) rememberAutoEditSession(target chatTarget, sessionID string) {
	key := autoEditSessionKey(target, sessionID)
	if key == "" {
		return
	}
	w.mu.Lock()
	w.autoEdits[key] = struct{}{}
	w.mu.Unlock()
}

func (w *Worker) autoApprovesEditApproval(target chatTarget, approval core.Approval) bool {
	if !canAllowSessionApproval(approval) {
		return false
	}
	key := autoEditSessionKey(target, approval.SessionID)
	if key == "" {
		return false
	}
	w.mu.Lock()
	_, ok := w.autoEdits[key]
	w.mu.Unlock()
	return ok
}

func autoEditSessionKey(target chatTarget, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return strings.TrimSpace(target.externalKey) + ":" + sessionID
}

func canAllowSessionApproval(approval core.Approval) bool {
	if _, ok := core.PermissionModeForSessionApproval(approval.ToolName); !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(approval.ToolName)) {
	case "write":
		params, ok := decodeApprovalParams[tools.WritePermissionsParams](approval.Params)
		return ok && params.WithinWorkingDir
	case "edit":
		params, ok := decodeApprovalParams[tools.EditPermissionsParams](approval.Params)
		return ok && params.WithinWorkingDir
	case "multiedit", "multi_edit":
		params, ok := decodeApprovalParams[tools.MultiEditPermissionsParams](approval.Params)
		return ok && params.WithinWorkingDir
	default:
		return false
	}
}

func decodeApprovalParams[T any](raw json.RawMessage) (T, bool) {
	var params T
	if len(raw) == 0 {
		return params, false
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, false
	}
	return params, true
}
