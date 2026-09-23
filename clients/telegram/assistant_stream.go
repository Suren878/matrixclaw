package telegram

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	assistantInitialChars = 40
	assistantInitialWait  = 1500 * time.Millisecond
	assistantDraftRefresh = 15 * time.Second
)

type telegramPreviewKey struct{}
type telegramSilentKey struct{}

func silentTelegramDelivery(ctx context.Context) context.Context {
	return context.WithValue(ctx, telegramSilentKey{}, true)
}

// Private chats use Telegram's ephemeral draft API. Nothing is persisted (or
// notified) until the segment is complete. Other chats use buffered edits.
func (w *Worker) streamAssistantMessage(ctx context.Context, target chatTarget, state *runDeliveryState, messageID, text string) error {
	sent := state.assistant[messageID]
	defer func() { state.assistant[messageID] = sent }()
	now := w.nowUTC()
	if sent.firstSeenAt.IsZero() {
		sent.firstSeenAt = now
	}
	if now.Before(sent.nextUpdateAt) {
		return nil
	}
	interval := w.config.StreamFlushInterval
	if interval <= 0 {
		interval = defaultStreamFlushInterval
	}
	ctx = context.WithValue(ctx, telegramPreviewKey{}, true)
	if target.chatID > 0 && !sent.draftDisabled {
		if sent.draftID == 0 {
			hash := fnv.New32a()
			_, _ = hash.Write([]byte(messageID))
			sent.draftID = int64(hash.Sum32() & 0x7fffffff)
			if sent.draftID == 0 {
				sent.draftID = 1
			}
		}
		// A preview is bounded; the final send uses the complete text and
		// splits it into persistent messages without dropping the overflow.
		preview := clipTelegramText(text)
		if sent.draftActive && sent.draftText == preview && now.Sub(sent.draftUpdatedAt) < assistantDraftRefresh {
			return nil
		}
		err := w.sendTelegramDraft(ctx, SendMessageDraftRequest{ChatID: target.chatID, DraftID: sent.draftID, Text: preview})
		if err == nil {
			sent.draftText = preview
			sent.draftUpdatedAt = now
			sent.draftActive = true
			sent.nextUpdateAt = now.Add(interval)
			return nil
		}
		if !unsupportedTelegramDraft(err) {
			sent.nextUpdateAt = now.Add(max(interval, telegramRetryAfter(err)))
			return err
		}
		sent.draftDisabled = true
	}
	if len(sent.chunks) == 0 && !assistantPreviewReady(text, now.Sub(sent.firstSeenAt)) {
		return nil
	}
	var err error
	sent, err = w.sendAssistantMessage(ctx, target, sent, text)
	sent.nextUpdateAt = now.Add(max(interval, telegramRetryAfter(err)))
	return err
}

func assistantPreviewReady(text string, age time.Duration) bool {
	length := utf8.RuneCountInString(strings.TrimSpace(text))
	if length >= assistantInitialChars || age >= assistantInitialWait {
		return true
	}
	return length >= 12 && strings.ContainsAny(text, ".!?\n。！？")
}

func unsupportedTelegramDraft(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.ErrorCode == 404 || apiErr.StatusCode == 404 {
		return true
	}
	if apiErr.ErrorCode != 400 && apiErr.StatusCode != 400 {
		return false
	}
	description := strings.ToLower(apiErr.Description)
	return strings.Contains(description, "draft") || strings.Contains(description, "method not found") || strings.Contains(description, "not supported")
}
