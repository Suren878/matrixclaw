package telegram

import (
	"context"
	"errors"
	"log"
	"time"
)

func (w *Worker) Run(ctx context.Context) error {
	if !w.config.SkipCommandRegistration {
		w.registerCommands(ctx)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := w.pollOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if IsRetryable(err) {
				wait := w.config.PollRetryDelay
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
					wait = apiErr.RetryAfter
				}
				if !sleepContext(ctx, wait) {
					return nil
				}
				continue
			}
			return err
		}
		if err := w.deliverPendingAutomation(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telegram: automation delivery failed: %v", err)
		}
	}
}

func (w *Worker) pollOnce(ctx context.Context) error {
	updates, err := w.api.GetUpdates(ctx, GetUpdatesRequest{
		Offset:         w.offset.Load(),
		Limit:          w.config.PollLimit,
		TimeoutSeconds: int(w.config.PollTimeout / time.Second),
		AllowedUpdates: []string{"message", "callback_query"},
	})
	if err != nil {
		return err
	}
	for _, update := range updates {
		w.offset.Store(update.UpdateID + 1)
		if err := w.handleUpdate(ctx, update); err != nil {
			log.Printf("telegram: update %d failed: %v", update.UpdateID, err)
			_ = w.sendTextForUpdate(ctx, update, "Internal error while processing the request.")
		}
	}
	return nil
}

func (w *Worker) handleUpdate(ctx context.Context, update Update) error {
	if update.CallbackQuery != nil {
		return w.handleCallbackQuery(update.CallbackQuery)
	}
	return w.handleTextMessage(ctx, update.Message)
}
