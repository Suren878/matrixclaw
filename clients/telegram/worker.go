package telegram

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"
)

func (w *Worker) Run(ctx context.Context) error {
	if !w.config.SkipCommandRegistration {
		w.registerCommands(ctx)
	}
	if strings.TrimSpace(w.config.BaseURL) == "" {
		return w.runUpdateLoop(ctx)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	go func() {
		errc <- w.runUpdateLoop(runCtx)
	}()
	go func() {
		errc <- w.runDeliveryLoop(runCtx)
	}()
	err := <-errc
	cancel()
	if err == nil || ctx.Err() != nil {
		return nil
	}
	return err
}

func (w *Worker) runUpdateLoop(ctx context.Context) error {
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
	}
}

func (w *Worker) runDeliveryLoop(ctx context.Context) error {
	interval := w.config.StreamFlushInterval
	if interval <= 0 {
		interval = defaultStreamFlushInterval
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if err := w.deliverPendingRuns(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telegram: run delivery failed: %v", err)
		}
		if err := w.deliverPendingDocuments(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telegram: document delivery failed: %v", err)
		}
		timer.Reset(interval)
	}
}

func (w *Worker) pollOnce(ctx context.Context) error {
	updates, err := w.api.GetUpdates(ctx, GetUpdatesRequest{
		Offset:         w.offset.Load(),
		Limit:          w.config.PollLimit,
		TimeoutSeconds: int(w.config.PollTimeout / time.Second),
		AllowedUpdates: telegramAllowedUpdates(),
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
	if update.InlineQuery != nil {
		return w.handleInlineQuery(ctx, update.InlineQuery)
	}
	if update.ChosenInlineResult != nil {
		return w.handleChosenInlineResult(ctx, update.ChosenInlineResult)
	}
	if update.GuestMessage != nil {
		return w.handleTextMessage(ctx, update.GuestMessage)
	}
	return w.handleTextMessage(ctx, update.Message)
}

func telegramAllowedUpdates() []string {
	return []string{"message", "guest_message", "inline_query", "chosen_inline_result", "callback_query"}
}
