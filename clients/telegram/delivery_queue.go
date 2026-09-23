package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (w *Worker) deliverPendingDeliveries(ctx context.Context, deliveryType string, filter core.ClientDeliveryFilter) error {
	w.delivery.Lock()
	defer w.delivery.Unlock()

	ctx = context.WithValue(ctx, telegramDeferredRetryKey{}, true)
	daemon := w.daemon("")
	filter.Client = w.config.ClientName
	filter.Type = deliveryType
	filter.Status = core.ClientDeliveryStatusPending
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	deliveries, err := daemon.ListClientDeliveries(ctx, filter)
	if err != nil {
		return err
	}
	now := w.nowUTC()
	if w.deliveryRetryAt == nil {
		w.deliveryRetryAt = make(map[string]time.Time)
	}
	for key, deadline := range w.deliveryRetryAt {
		if !now.Before(deadline) {
			delete(w.deliveryRetryAt, key)
		}
	}
	// An acknowledgement may succeed remotely while its response is lost.
	// Keep local receipts bounded even if those deliveries disappear from polling.
	for id, sentAt := range w.deliveryReceipts {
		if now.Sub(sentAt) >= 24*time.Hour {
			delete(w.deliveryReceipts, id)
		}
	}
	var pendingErr error
	for _, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := deliveryRetryKey(delivery)
		if w.nowUTC().Before(w.deliveryRetryAt[key]) {
			continue
		}
		if _, sent := w.deliveryReceipts[delivery.ID]; sent {
			err = w.acknowledgeSentDelivery(ctx, daemon, delivery.ID)
			if err == nil && delivery.Type == core.ClientDeliveryTypeRun {
				if target, ok := targetFromClientDelivery(delivery); ok {
					w.clearRunRenderState(target.externalKey, delivery.RunID)
				}
			}
		} else if deliveryType == core.ClientDeliveryTypeDocument {
			err = w.deliverDocument(ctx, delivery)
		} else {
			err = w.deliverPendingRunDelivery(ctx, daemon, delivery)
		}
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, sent := w.deliveryReceipts[delivery.ID]
		if retryableDeliveryError(err) || sent {
			delay := telegramRetryAfter(err)
			if delay <= 0 {
				delay = w.config.PollRetryDelay
				if delay <= 0 {
					delay = 2 * time.Second
				}
			}
			w.deliveryRetryAt[key] = w.nowUTC().Add(delay)
		} else {
			if failErr := daemon.FailClientDelivery(ctx, delivery.ID, err.Error()); failErr != nil {
				err = errors.Join(err, failErr)
			}
		}
		pendingErr = errors.Join(pendingErr, fmt.Errorf("delivery %s: %w", delivery.ID, err))
	}
	return pendingErr
}

func deliveryRetryKey(delivery core.ClientDelivery) string {
	if target, ok := targetFromClientDelivery(delivery); ok {
		switch {
		case target.isChat():
			return fmt.Sprintf("chat:%d", target.chatID)
		case target.isInline():
			return "inline:" + target.inlineMessageID
		case target.isGuest():
			return "guest:" + target.guestQueryID
		}
	}
	return "delivery:" + delivery.ID
}

func retryableDeliveryError(err error) bool {
	if IsRetryable(err) {
		return true
	}
	var apiErr *daemonclient.APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusRequestTimeout ||
		apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500)
}

// This avoids resending confirmed Telegram output after a daemon ack failure
// within this worker's lifetime. It cannot resolve ambiguous Telegram timeouts
// or provide exactly-once delivery across a process restart.
func (w *Worker) acknowledgeSentDelivery(ctx context.Context, daemon *daemonclient.Client, id string) error {
	if w.deliveryReceipts == nil {
		w.deliveryReceipts = make(map[string]time.Time)
	}
	w.deliveryReceipts[id] = w.nowUTC()
	if err := daemon.AcknowledgeClientDelivery(ctx, id); err != nil {
		return err
	}
	delete(w.deliveryReceipts, id)
	return nil
}
