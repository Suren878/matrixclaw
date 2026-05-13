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

func (w *Worker) editTelegramMessage(ctx context.Context, req EditMessageTextRequest) (EditMessageTextResponse, error) {
	req.ReplyMarkup = w.compactInlineKeyboardMarkup(req.ReplyMarkup)
	response, err := w.api.EditMessageText(ctx, req)
	if !shouldRetryTelegramAfter(err) {
		return response, err
	}
	if !sleepContext(ctx, telegramRetryAfter(err)) {
		return EditMessageTextResponse{}, ctx.Err()
	}
	return w.api.EditMessageText(ctx, req)
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
