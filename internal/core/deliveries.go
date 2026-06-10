package core

import (
	"context"
	"fmt"
	"strings"
)

const (
	ClientDeliveryTypeDaemonRestart = "daemon_restart"
	ClientDeliveryTypeRun           = "run"
	ClientDeliveryTypeDocument      = "document"
)

type DocumentDeliveryPayload struct {
	StoragePath string `json:"storage_path"`
	Temporary   bool   `json:"temporary,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	Caption     string `json:"caption,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

func (c *Core) CreateClientDelivery(ctx context.Context, delivery ClientDelivery) (ClientDelivery, error) {
	if c == nil || c.store == nil {
		return ClientDelivery{}, fmt.Errorf("%w: store not configured", ErrExecutionUnavailable)
	}
	delivery, err := c.prepareClientDelivery(delivery)
	if err != nil {
		return ClientDelivery{}, err
	}
	if err := c.store.CreateClientDelivery(ctx, delivery); err != nil {
		return ClientDelivery{}, err
	}
	return delivery, nil
}

func (c *Core) prepareClientDelivery(delivery ClientDelivery) (ClientDelivery, error) {
	delivery.Type = normalizeText(delivery.Type)
	delivery.Client = normalizeText(delivery.Client)
	delivery.ExternalKey = normalizeText(delivery.ExternalKey)
	delivery.SessionID = normalizeText(delivery.SessionID)
	delivery.RunID = normalizeText(delivery.RunID)
	delivery.TaskID = normalizeText(delivery.TaskID)
	delivery.Summary = normalizeText(delivery.Summary)
	if delivery.Type == "" {
		return ClientDelivery{}, fmt.Errorf("%w: delivery type is required", ErrInvalidInput)
	}
	if delivery.Client == "" {
		return ClientDelivery{}, fmt.Errorf("%w: delivery client is required", ErrInvalidInput)
	}
	if len(delivery.Payload) == 0 {
		delivery.Payload = nil
	}
	if delivery.ID == "" {
		delivery.ID = c.newID("delivery")
	}
	if delivery.Status == "" {
		delivery.Status = ClientDeliveryStatusPending
	}
	if len(delivery.Address) == 0 {
		delivery.Address = nil
	}
	now := c.now().UTC()
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = now
	}
	if delivery.UpdatedAt.IsZero() {
		delivery.UpdatedAt = now
	}
	return delivery, nil
}

func (c *Core) ListClientDeliveries(ctx context.Context, filter ClientDeliveryFilter) ([]ClientDelivery, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("%w: store not configured", ErrExecutionUnavailable)
	}
	filter.Client = strings.TrimSpace(filter.Client)
	filter.ExternalKey = strings.TrimSpace(filter.ExternalKey)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.RunID = strings.TrimSpace(filter.RunID)
	filter.TaskID = strings.TrimSpace(filter.TaskID)
	filter.Type = strings.TrimSpace(filter.Type)
	return c.store.ListClientDeliveries(ctx, filter)
}

func (c *Core) MarkClientDeliveryReady(ctx context.Context, delivery ClientDelivery) error {
	return c.finishClientDelivery(ctx, delivery, ClientDeliveryStatusReady, "")
}

func (c *Core) MarkClientDeliverySent(ctx context.Context, delivery ClientDelivery) error {
	return c.finishClientDelivery(ctx, delivery, ClientDeliveryStatusSent, "")
}

func (c *Core) AcknowledgeClientDelivery(ctx context.Context, deliveryID string) error {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return fmt.Errorf("%w: delivery id is required", ErrInvalidInput)
	}
	return c.finishClientDelivery(ctx, ClientDelivery{ID: deliveryID}, ClientDeliveryStatusSent, "")
}

func (c *Core) MarkClientDeliveryFailed(ctx context.Context, delivery ClientDelivery, deliveryErr error) error {
	errText := ""
	if deliveryErr != nil {
		errText = strings.TrimSpace(deliveryErr.Error())
	}
	return c.finishClientDelivery(ctx, delivery, ClientDeliveryStatusFailed, errText)
}

func (c *Core) finishClientDelivery(ctx context.Context, delivery ClientDelivery, status ClientDeliveryStatus, errText string) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("%w: store not configured", ErrExecutionUnavailable)
	}
	now := c.now().UTC()
	delivery.Status = status
	delivery.Error = errText
	delivery.UpdatedAt = now
	delivery.FinishedAt = &now
	return c.store.UpdateClientDelivery(ctx, delivery)
}
