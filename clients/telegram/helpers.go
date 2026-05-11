package telegram

import (
	"context"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (w *Worker) daemon(externalKey string) *daemonclient.Client {
	client := daemonclient.New(w.config.BaseURL, w.config.ClientName, externalKey).WithAPIToken(w.config.APIToken)
	client.HTTPClient = w.config.DaemonHTTPClient
	return client
}

func (w *Worker) allowMessage(message *Message) bool {
	if message == nil || message.Chat.Type != "private" {
		return false
	}
	if w.config.AllowedUserID == 0 {
		return true
	}
	return message.Chat.ID == w.config.AllowedUserID || (message.From != nil && message.From.ID == w.config.AllowedUserID)
}

func (w *Worker) allowCallback(cq *CallbackQuery) bool {
	if cq == nil || cq.Message == nil || cq.Message.Chat.Type != "private" {
		return false
	}
	if w.config.AllowedUserID == 0 {
		return true
	}
	return cq.Message.Chat.ID == w.config.AllowedUserID || (cq.From != nil && cq.From.ID == w.config.AllowedUserID)
}

func targetFromMessage(message *Message) chatTarget {
	target := chatTarget{
		chatID:    message.Chat.ID,
		messageID: message.MessageID,
	}
	if message.MessageThreadID > 0 {
		target.threadID = message.MessageThreadID
		target.externalKey = telegramExternalKey(message.Chat.ID, message.MessageThreadID)
		return target
	}
	target.externalKey = telegramExternalKey(message.Chat.ID, 0)
	return target
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
