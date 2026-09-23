package telegram

import (
	"context"
	"errors"
	"time"
)

func (w *Worker) sendTelegramMessage(ctx context.Context, req SendMessageRequest) (SentMessage, error) {
	if silent, _ := ctx.Value(telegramSilentKey{}).(bool); silent {
		req.DisableNotification = true
	}
	req.ReplyMarkup = w.compactReplyMarkup(req.ReplyMarkup)
	if req.ChatID != 0 && req.ReplyMarkup == nil && !req.SkipReplyKeyboardRemove {
		req.ReplyMarkup = telegramReplyKeyboardRemove()
	}
	return retryTelegramCall(ctx, func() (SentMessage, error) { return w.api.SendMessage(ctx, req) })
}

func telegramReplyKeyboardRemove() *ReplyKeyboardRemove {
	return &ReplyKeyboardRemove{RemoveKeyboard: true}
}

func (w *Worker) sendTelegramDraft(ctx context.Context, req SendMessageDraftRequest) error {
	_, err := retryTelegramCall(ctx, func() (struct{}, error) {
		return struct{}{}, w.api.SendMessageDraft(ctx, req)
	})
	return err
}

func (w *Worker) editTelegramMessage(ctx context.Context, req EditMessageTextRequest) error {
	req.ReplyMarkup = w.compactInlineKeyboardMarkup(req.ReplyMarkup)
	_, err := retryTelegramCall(ctx, func() (EditMessageTextResponse, error) { return w.api.EditMessageText(ctx, req) })
	return err
}

func isTelegramPreview(ctx context.Context) bool {
	preview, _ := ctx.Value(telegramPreviewKey{}).(bool)
	return preview
}

func (w *Worker) editTelegramMessageMedia(ctx context.Context, req EditMessageMediaRequest) error {
	req.ReplyMarkup = w.compactInlineKeyboardMarkup(req.ReplyMarkup)
	_, err := retryTelegramCall(ctx, func() (EditMessageMediaResponse, error) { return w.api.EditMessageMedia(ctx, req) })
	return err
}

func (w *Worker) answerGuestQuery(ctx context.Context, req AnswerGuestQueryRequest) (SentGuestMessage, error) {
	req.Result.ReplyMarkup = w.compactInlineKeyboardMarkup(req.Result.ReplyMarkup)
	return retryTelegramCall(ctx, func() (SentGuestMessage, error) { return w.api.AnswerGuestQuery(ctx, req) })
}

func (w *Worker) compactReplyMarkup(markup any) any {
	if markup == nil {
		return nil
	}
	if reply, ok := markup.(*ReplyKeyboardMarkup); ok && reply == nil {
		return nil
	}
	if remove, ok := markup.(*ReplyKeyboardRemove); ok && remove == nil {
		return nil
	}
	inline, ok := markup.(*InlineKeyboardMarkup)
	if !ok {
		return markup
	}
	if inline == nil {
		return nil
	}
	return w.compactInlineKeyboardMarkup(inline)
}

// Delivery polling and previews schedule their own retries. Sleeping inside
// their API calls would hold the delivery lock and stall unrelated chats.
type telegramDeferredRetryKey struct{}

func retryTelegramCall[T any](ctx context.Context, call func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	result, err := call()
	delay := telegramRetryAfter(err)
	deferred, _ := ctx.Value(telegramDeferredRetryKey{}).(bool)
	if delay <= 0 || deferred || isTelegramPreview(ctx) {
		return result, err
	}
	if !sleepContext(ctx, delay) {
		return zero, ctx.Err()
	}
	return call()
}

func telegramRetryAfter(err error) time.Duration {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return 0
	}
	return apiErr.RetryAfter
}
