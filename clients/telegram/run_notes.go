package telegram

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// renderEngineNotes sends each engine note of the run once, without a notification.
func (w *Worker) renderEngineNotes(ctx context.Context, target chatTarget, messages []transcript.Message, runID string, state *runDeliveryState) error {
	runID = strings.TrimSpace(runID)
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != runID || message.Origin != transcript.OriginEngine {
			continue
		}
		if _, sent := state.notes[message.ID]; sent {
			continue
		}
		text := strings.TrimSpace(message.Content)
		if text == "" {
			continue
		}
		if _, err := w.sendTelegramMessage(silentTelegramDelivery(ctx), SendMessageRequest{ChatID: target.chatID, Text: clipTelegramText("Note: " + text)}); err != nil {
			return err
		}
		state.notes[message.ID] = struct{}{}
	}
	return nil
}

// offerContinue sends the stop notice of a run that stopped before its work was
// done, with a Continue button.
func (w *Worker) offerContinue(ctx context.Context, target chatTarget, run core.Run, state *runDeliveryState) error {
	notice := controlplane.StopNotice(run.StopReason)
	if notice == "" || state.continueOffered {
		return nil
	}
	markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{commandButton("Continue", catalogCommand(controlplane.CommandContinue, ""))}}}
	if _, err := w.sendTelegramMessage(ctx, SendMessageRequest{ChatID: target.chatID, Text: notice, ReplyMarkup: markup}); err != nil {
		return err
	}
	state.continueOffered = true
	return nil
}
