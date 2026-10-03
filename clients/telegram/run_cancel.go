package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func isCancelRunCommand(text string) bool {
	command, args, ok := splitTelegramCommand(text)
	return ok && command == "cancel" && args == ""
}

// cancelRunMarkup is the button of a run's status message that cancels it.
func cancelRunMarkup(runID string) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "⛔ Cancel", CallbackData: cbCancelRun + runID}}}}
}

// cancelSessionRun cancels the run of the chat's session, if one is running.
func (w *Worker) cancelSessionRun(ctx context.Context, target chatTarget) error {
	snapshot, err := w.daemonFor(target, target.externalKey).LoadSnapshot(ctx)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Cancel failed: %v", err))
	}
	if snapshot.Run == nil || runFinished(snapshot.Run.Status) {
		return w.sendText(ctx, target, "Nothing is running.")
	}
	return w.cancelRun(ctx, target, snapshot.Run.ID)
}

// cancelRun cancels the run and shows its end in the chat: through the run's
// delivery here, or with a reply when the run is delivered elsewhere.
func (w *Worker) cancelRun(ctx context.Context, target chatTarget, runID string) error {
	daemon := w.daemonFor(target, target.externalKey)
	deliveries, err := daemon.ListClientDeliveries(ctx, core.ClientDeliveryFilter{Client: ClientName, RunID: runID, Type: core.ClientDeliveryTypeRun, Status: core.ClientDeliveryStatusPending})
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Cancel failed: %v", err))
	}
	run, err := daemon.CancelRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Cancel failed: %v", err))
	}
	if !deliveredToChat(deliveries, target.chatID) {
		return w.sendText(ctx, target, renderRunStatus(run))
	}
	return w.deliverPendingRun(ctx, target, run.SessionID, run.ID)
}

func deliveredToChat(deliveries []core.ClientDelivery, chatID int64) bool {
	for _, delivery := range deliveries {
		if target, ok := targetFromClientDelivery(delivery); ok && target.isChat() && target.chatID == chatID {
			return true
		}
	}
	return false
}
