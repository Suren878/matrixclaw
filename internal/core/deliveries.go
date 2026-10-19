package core

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	ClientDeliveryTypeRun      = "run"
	ClientDeliveryTypeDocument = "document"
	// ClientDeliveryTypeNotice is a short text for the user outside any run.
	ClientDeliveryTypeNotice = "notice"
	// ClientDeliveryTypeApproval asks the user for a subagent's approval
	// outside the run that started it.
	ClientDeliveryTypeApproval = "approval"
)

// NoticeDeliveryPayload is a notice's optional payload; Replace asks the
// client to replace the message at the address instead of sending a new one.
type NoticeDeliveryPayload struct {
	Replace bool `json:"replace,omitempty"`
}

// ApprovalDeliveryPayload names the approval an approval delivery asks for.
type ApprovalDeliveryPayload struct {
	ApprovalID string `json:"approval_id"`
}

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
	delivery.Type = strings.TrimSpace(delivery.Type)
	delivery.Client = strings.TrimSpace(delivery.Client)
	delivery.ExternalKey = strings.TrimSpace(delivery.ExternalKey)
	delivery.SessionID = strings.TrimSpace(delivery.SessionID)
	delivery.RunID = strings.TrimSpace(delivery.RunID)
	delivery.TaskID = strings.TrimSpace(delivery.TaskID)
	delivery.Summary = strings.TrimSpace(delivery.Summary)
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

// ReleaseHeldClientDeliveries makes the deliveries held for the next daemon
// start, like the restart notice, pending for their clients.
func (c *Core) ReleaseHeldClientDeliveries(ctx context.Context) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("%w: store not configured", ErrExecutionUnavailable)
	}
	return c.store.ReleaseHeldClientDeliveries(ctx, c.now().UTC())
}

func (c *Core) AcknowledgeClientDelivery(ctx context.Context, deliveryID string) error {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return fmt.Errorf("%w: delivery id is required", ErrInvalidInput)
	}
	return c.finishClientDelivery(ctx, deliveryID, ClientDeliveryStatusSent, "")
}

func (c *Core) MarkClientDeliveryFailed(ctx context.Context, deliveryID string, deliveryErr error) error {
	errText := ""
	if deliveryErr != nil {
		errText = strings.TrimSpace(deliveryErr.Error())
	}
	return c.finishClientDelivery(ctx, deliveryID, ClientDeliveryStatusFailed, errText)
}

func (c *Core) finishClientDelivery(ctx context.Context, deliveryID string, status ClientDeliveryStatus, errText string) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("%w: store not configured", ErrExecutionUnavailable)
	}
	return c.store.FinishClientDelivery(ctx, deliveryID, status, errText, c.now().UTC())
}

func (c *Core) prepareSessionRunDelivery(run Run, text string, parts []transcript.MessagePart, client string, externalKey string, to deliveryTo) (ClientDelivery, bool, error) {
	if client == "" || externalKey == "" {
		return ClientDelivery{}, false, nil
	}
	delivery := ClientDelivery{
		Type:        ClientDeliveryTypeRun,
		Client:      client,
		ExternalKey: externalKey,
		SessionID:   run.SessionID,
		RunID:       run.ID,
		Summary:     sessionRunDeliverySummary(text, parts),
		Address:     cloneRawMessage(to.Address),
		ReplyOnce:   to.ReplyOnce,
		Status:      ClientDeliveryStatusPending,
	}
	prepared, err := c.prepareClientDelivery(delivery)
	if err != nil {
		return ClientDelivery{}, false, err
	}
	return prepared, true, nil
}

func sessionRunDeliverySummary(text string, parts []transcript.MessagePart) string {
	summary := strings.Join(strings.Fields(text), " ")
	if summary == "" {
		for _, part := range parts {
			if part.Text == nil {
				continue
			}
			summary = strings.Join(strings.Fields(part.Text.Text), " ")
			if summary != "" {
				break
			}
		}
	}
	if summary == "" {
		summary = "User message"
	}
	return textutil.Clip(summary, 240, "...")
}

// deliverRunToSteerer delivers the run a message steers to the address it came
// from as well, unless the run is delivered there already.
func (c *Core) deliverRunToSteerer(ctx context.Context, run Run, input HandleMessageInput, text string, parts []transcript.MessagePart) error {
	if !input.ClientCapabilities.ReceivesDeliveries {
		return nil
	}
	delivery, ok, err := c.prepareSessionRunDelivery(run, text, parts, input.Client, input.ExternalKey, deliveryTo{input.DeliveryAddress, input.ReplyOnce})
	if err != nil || !ok {
		return err
	}
	existing, err := c.store.ListClientDeliveries(ctx, ClientDeliveryFilter{Client: delivery.Client, ExternalKey: delivery.ExternalKey, RunID: run.ID, Type: ClientDeliveryTypeRun})
	if err != nil {
		return err
	}
	for _, other := range existing {
		if bytes.Equal(other.Address, delivery.Address) {
			return nil
		}
	}
	return c.store.CreateClientDelivery(ctx, delivery)
}
