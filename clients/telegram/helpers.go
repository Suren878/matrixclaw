package telegram

import (
	"context"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/permission"
)

type targetKind string

const (
	telegramTargetChat   targetKind = "chat"
	telegramTargetGuest  targetKind = "guest"
	telegramTargetInline targetKind = "inline"
)

// daemon is the daemon client for the worker's own work (deliveries, lookups);
// it acts as a member, so it can do nothing only the owner may.
func (w *Worker) daemon(externalKey string) *daemonclient.Client {
	client := daemonclient.New(w.config.BaseURL, ClientName, externalKey).
		WithAPIToken(w.config.APIToken).
		WithCapabilities(core.ClientCapabilities{
			SupportsVoiceDelivery:    true,
			SupportsDocumentDelivery: true,
		})
	client.HTTPClient = w.daemonHTTP
	client.Role = core.RoleMember
	return client
}

// daemonFor is the daemon client that acts for target, with the role of the
// chat Telegram reported the update from (never anything in its text).
func (w *Worker) daemonFor(target chatTarget, externalKey string) *daemonclient.Client {
	client := w.daemon(externalKey)
	client.Role = w.role(target)
	return client
}

func (w *Worker) role(target chatTarget) core.Role {
	switch {
	case w.ownerChat(target):
		return core.RoleOwner
	case target.isChat():
		return core.RoleMember
	default:
		return core.RoleGuest
	}
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

// ownerChat reports whether target is the owner's private chat, the only
// Telegram chat that may change global permission rules.
func (w *Worker) ownerChat(target chatTarget) bool {
	return w.config.AllowedUserID != 0 && target.isChat() && target.chatID == w.config.AllowedUserID
}

// keepsRules reports whether target may keep permission rules of scope: session
// rules from any chat with the bot, global ones from the owner chat only.
func (w *Worker) keepsRules(target chatTarget, scope permission.Scope) bool {
	return target.isChat() && (scope == permission.ScopeSession || w.ownerChat(target))
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
	return target.kind == telegramTargetGuest
}

func (target chatTarget) isInline() bool {
	return target.kind == telegramTargetInline
}

func (target chatTarget) isChat() bool {
	return target.kind == telegramTargetChat
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
