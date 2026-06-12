package telegram

import (
	"context"
	"errors"
	"time"
)

func (w *Worker) sendTelegramMessage(ctx context.Context, req SendMessageRequest) (SentMessage, error) {
	req.ReplyMarkup = w.compactReplyMarkup(req.ReplyMarkup)
	if req.ChatID != 0 && req.ReplyMarkup == nil {
		req.ReplyMarkup = telegramReplyKeyboardRemove()
	}
	reply, err := w.api.SendMessage(ctx, req)
	if !shouldRetryTelegramAfter(err) {
		return reply, err
	}
	if !sleepContext(ctx, telegramRetryAfter(err)) {
		return SentMessage{}, ctx.Err()
	}
	return w.api.SendMessage(ctx, req)
}

func telegramReplyKeyboardRemove() *ReplyKeyboardRemove {
	return &ReplyKeyboardRemove{RemoveKeyboard: true}
}

func (w *Worker) sendTelegramDraft(ctx context.Context, req SendMessageDraftRequest) error {
	err := w.api.SendMessageDraft(ctx, req)
	if !shouldRetryTelegramAfter(err) {
		return err
	}
	if !sleepContext(ctx, telegramRetryAfter(err)) {
		return ctx.Err()
	}
	return w.api.SendMessageDraft(ctx, req)
}

func (w *Worker) editTelegramMessage(ctx context.Context, req EditMessageTextRequest) error {
	req.ReplyMarkup = w.compactInlineKeyboardMarkup(req.ReplyMarkup)
	_, err := w.api.EditMessageText(ctx, req)
	if !shouldRetryTelegramAfter(err) {
		return err
	}
	if !sleepContext(ctx, telegramRetryAfter(err)) {
		return ctx.Err()
	}
	_, err = w.api.EditMessageText(ctx, req)
	return err
}

func (w *Worker) editTelegramMessageMedia(ctx context.Context, req EditMessageMediaRequest) error {
	req.ReplyMarkup = w.compactInlineKeyboardMarkup(req.ReplyMarkup)
	_, err := w.api.EditMessageMedia(ctx, req)
	if !shouldRetryTelegramAfter(err) {
		return err
	}
	if !sleepContext(ctx, telegramRetryAfter(err)) {
		return ctx.Err()
	}
	_, err = w.api.EditMessageMedia(ctx, req)
	return err
}

func (w *Worker) answerGuestQuery(ctx context.Context, req AnswerGuestQueryRequest) (SentGuestMessage, error) {
	req.Result.ReplyMarkup = w.compactInlineKeyboardMarkup(req.Result.ReplyMarkup)
	reply, err := w.api.AnswerGuestQuery(ctx, req)
	if !shouldRetryTelegramAfter(err) {
		return reply, err
	}
	if !sleepContext(ctx, telegramRetryAfter(err)) {
		return SentGuestMessage{}, ctx.Err()
	}
	return w.api.AnswerGuestQuery(ctx, req)
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

func shouldRetryTelegramAfter(err error) bool {
	return telegramRetryAfter(err) > 0
}

func telegramRetryAfter(err error) time.Duration {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return 0
	}
	return apiErr.RetryAfter
}
