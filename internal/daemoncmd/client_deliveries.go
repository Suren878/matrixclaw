package daemoncmd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

type restartDeliveryAddressNormalizer interface {
	ClientName() string
	NormalizeRestartDeliveryAddress(json.RawMessage) (json.RawMessage, error)
}

type restartDeliverySender interface {
	ClientName() string
	DeliverRestartNotification(context.Context, core.ClientDelivery, string) error
}

func (r *clientRegistry) NormalizeRestartDeliveryAddress(notification *core.ClientDeliveryTarget) (json.RawMessage, error) {
	if notification == nil || len(notification.Address) == 0 {
		return nil, nil
	}
	client := strings.TrimSpace(notification.Client)
	if r == nil {
		return cloneRawJSON(notification.Address), nil
	}
	for _, adapter := range r.clients {
		if adapter == nil {
			continue
		}
		normalizer := adapter.RestartDeliveryAddressNormalizer()
		if normalizer == nil {
			continue
		}
		if client == strings.TrimSpace(normalizer.ClientName()) {
			return normalizer.NormalizeRestartDeliveryAddress(notification.Address)
		}
	}
	return cloneRawJSON(notification.Address), nil
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}
