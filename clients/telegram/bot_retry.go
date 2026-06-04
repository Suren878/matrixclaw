package telegram

import (
	"context"
	"errors"
	"time"
)

func (w *Worker) sendTelegramMessage(ctx context.Context, req SendMessageRequest) (SentMessage, error) {
	req.ReplyMarkup = w.compactInlineKeyboardMarkup(req.ReplyMarkup)
	reply, err := w.api.SendMessage(ctx, req)
	if !shouldRetryTelegramAfter(err) {
		return reply, err
	}
	if !sleepContext(ctx, telegramRetryAfter(err)) {
		return SentMessage{}, ctx.Err()
	}
	return w.api.SendMessage(ctx, req)
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
