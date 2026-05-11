package telegram

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
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
	if _, ok := core.PermissionModeForSessionApproval(approval.ToolName); !ok {
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
