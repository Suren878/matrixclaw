package telegram

import (
	"context"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (w *Worker) updateRunTypingIndicator(ctx context.Context, target chatTarget, run *core.Run) {
	key := runTypingIndicatorKey(target, run)
	if key == "" {
		return
	}
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning:
		w.sendRunTypingIndicator(ctx, target, key)
	default:
		w.clearRunTypingIndicator(key)
	}
}

func runTypingIndicatorKey(target chatTarget, run *core.Run) string {
	if run == nil || target.chatID == 0 {
		return ""
	}
	runID := strings.TrimSpace(run.ID)
	if runID == "" {
		return ""
	}
	return strings.Join([]string{strconv.FormatInt(target.chatID, 10), strings.TrimSpace(target.externalKey), runID}, "\x00")
}

func (w *Worker) sendRunTypingIndicator(ctx context.Context, target chatTarget, key string) {
	if w == nil || w.api == nil || key == "" {
		return
	}
	now := w.nowUTC()
	w.mu.Lock()
	if last, ok := w.chatActions[key]; ok && now.Sub(last) < chatActionInterval {
		w.mu.Unlock()
		return
	}
	w.chatActions[key] = now
	w.mu.Unlock()

	w.sendTypingChatAction(ctx, target, "run")
}

func (w *Worker) clearRunTypingIndicator(key string) {
	if w == nil || key == "" {
		return
	}
	w.mu.Lock()
	delete(w.chatActions, key)
	w.mu.Unlock()
}
