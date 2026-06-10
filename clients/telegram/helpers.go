package telegram

import (
	"context"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

const (
	telegramTargetChat   = "chat"
	telegramTargetGuest  = "guest"
	telegramTargetInline = "inline"
)

func (w *Worker) daemon(externalKey string) *daemonclient.Client {
	client := daemonclient.New(w.config.BaseURL, w.config.ClientName, externalKey).WithAPIToken(w.config.APIToken)
	client.HTTPClient = w.config.DaemonHTTPClient
	return client
}

func (w *Worker) allowMessage(message *Message) bool {
	if message == nil {
		return false
	}
	if strings.TrimSpace(message.GuestQueryID) != "" {
		if w.config.AllowedUserID == 0 {
			return true
		}
		return message.From != nil && message.From.ID == w.config.AllowedUserID
	}
	if message.Chat.Type != "private" {
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

func (w *Worker) allowInlineUser(user *User) bool {
	if user == nil {
		return false
	}
	if w.config.AllowedUserID == 0 {
		return true
	}
	return user.ID == w.config.AllowedUserID
}

func targetFromMessage(message *Message) chatTarget {
	if guestQueryID := strings.TrimSpace(message.GuestQueryID); guestQueryID != "" {
		return chatTarget{
			kind:         telegramTargetGuest,
			chatID:       message.Chat.ID,
			messageID:    message.MessageID,
			guestQueryID: guestQueryID,
			externalKey:  telegramGuestExternalKey(guestQueryID),
		}
	}
	target := chatTarget{
		kind:      telegramTargetChat,
		chatID:    message.Chat.ID,
		messageID: message.MessageID,
	}
	target.externalKey = telegramExternalKey(message.Chat.ID)
	return target
}

func (target chatTarget) isGuest() bool {
	return target.kind == telegramTargetGuest || strings.TrimSpace(target.guestQueryID) != ""
}

func (target chatTarget) isInline() bool {
	return target.kind == telegramTargetInline ||
		(target.kind == "" && strings.TrimSpace(target.inlineMessageID) != "" && strings.TrimSpace(target.guestQueryID) == "")
}

func (target chatTarget) isChat() bool {
	return !target.isGuest() && !target.isInline()
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
