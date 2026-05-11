package telegram

import (
	"context"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (w *Worker) deliverPendingAutomation(ctx context.Context) error {
	daemon := w.daemon("")
	deliveries, err := daemon.ListClientDeliveries(ctx, core.ClientDeliveryFilter{
		Client: w.config.ClientName,
		Type:   core.ClientDeliveryTypeAutomationRun,
		Status: core.ClientDeliveryStatusPending,
		Limit:  20,
	})
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		target, ok := targetFromClientDelivery(delivery)
		if !ok {
			_ = daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
			continue
		}
		sessionID, runID := automationDeliveryRun(delivery)
		if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(runID) == "" {
			_ = daemon.AcknowledgeClientDelivery(ctx, delivery.ID)
			continue
		}
		if w.isMonitoringRun(target.externalKey, runID) {
			continue
		}
		w.startMonitorWithDelivery(ctx, target, sessionID, runID, delivery.ID)
	}
	return nil
}

func targetFromClientDelivery(delivery core.ClientDelivery) (chatTarget, bool) {
	if target, ok := targetFromDeliveryAddress(delivery.Address, delivery.ExternalKey); ok {
		return target, true
	}
	return chatTarget{}, false
}

func (w *Worker) ackRunDelivery(ctx context.Context, daemon *daemonclient.Client, state *runDeliveryState) {
	if state == nil || strings.TrimSpace(state.deliveryID) == "" || daemon == nil {
		return
	}
	if err := daemon.AcknowledgeClientDelivery(ctx, state.deliveryID); err != nil {
		log.Printf("telegram: ack automation delivery %s failed: %v", state.deliveryID, err)
		return
	}
	state.deliveryID = ""
}

func automationDeliveryRun(delivery core.ClientDelivery) (string, string) {
	return strings.TrimSpace(delivery.SessionID), strings.TrimSpace(delivery.RunID)
}
