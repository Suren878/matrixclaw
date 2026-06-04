package telegram

import (
	"context"
	"fmt"
	"log"

	"github.com/Suren878/matrixclaw/internal/commandcatalog"
)

func (w *Worker) dispatchRestartCommandAndEdit(target chatTarget, messageID int64) error {
	log.Printf("telegram: daemon restart requested chat=%d message=%d", target.chatID, messageID)
	telegramCtx, cancel := context.WithTimeout(context.Background(), defaultTelegramHTTPTimeout)
	defer cancel()
	if messageID > 0 {
		updatedMessageID, err := w.editOrSendMessage(telegramCtx, target, messageID, restartProgressText, nil)
		if err != nil {
			return err
		}
		messageID = updatedMessageID
	} else {
		sent, err := w.sendTelegramMessage(telegramCtx, SendMessageRequest{
			ChatID:          target.chatID,
			MessageThreadID: target.threadID,
			Text:            restartProgressText,
		})
		if err != nil {
			return err
		}
		messageID = sent.MessageID
	}

	restartCtx, cancel := context.WithTimeout(context.Background(), defaultDaemonHTTPTimeout)
	defer cancel()
	err := w.daemon("").RestartDaemonWithNotification(restartCtx, deliveryTargetForMessage(w.config.ClientName, target, messageID))

	telegramCtx, cancel = context.WithTimeout(context.Background(), defaultTelegramHTTPTimeout)
	defer cancel()
	if err != nil {
		log.Printf("telegram: daemon restart failed chat=%d message=%d: %v", target.chatID, messageID, err)
		return w.editOrSend(telegramCtx, target, messageID, fmt.Sprintf("Architect restart failed: %v", err), nil)
	}
	return nil
}

func isDaemonRestartCommand(text string) bool {
	return matchesCatalogCommand(text, commandcatalog.CommandRestart, "confirm")
}
