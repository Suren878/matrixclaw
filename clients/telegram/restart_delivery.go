package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

type RestartDeliveryCodec struct{}

func (RestartDeliveryCodec) ClientName() string {
	return ClientName
}

func (RestartDeliveryCodec) NormalizeRestartDeliveryAddress(raw json.RawMessage) (json.RawMessage, error) {
	address, err := decodeRestartDeliveryAddress(raw)
	if err != nil {
		return nil, err
	}
	return encodeDeliveryAddress(address), nil
}

type RestartDeliverySenderConfig struct {
	BotToken   string
	BaseURL    string
	HTTPClient HTTPDoer
	API        BotAPI
}

type RestartDeliverySender struct {
	api BotAPI
}

func NewRestartDeliverySender(cfg RestartDeliverySenderConfig) (*RestartDeliverySender, error) {
	api := cfg.API
	if api == nil {
		client, err := NewClient(ClientConfig{
			Token:      cfg.BotToken,
			BaseURL:    cfg.BaseURL,
			HTTPClient: cfg.HTTPClient,
		})
		if err != nil {
			return nil, err
		}
		api = client
	}
	return &RestartDeliverySender{api: api}, nil
}

func (s *RestartDeliverySender) ClientName() string {
	return ClientName
}

func (s *RestartDeliverySender) DeliverRestartNotification(ctx context.Context, delivery core.ClientDelivery, fallback string) error {
	if s == nil || s.api == nil {
		return fmt.Errorf("telegram: restart delivery sender is not configured")
	}
	address, err := decodeRestartDeliveryAddress(delivery.Address)
	if err != nil {
		return err
	}
	text := fallback
	if summary := strings.TrimSpace(delivery.Summary); summary != "" {
		text = summary
	}
	_, err = s.api.EditMessageText(ctx, EditMessageTextRequest{
		ChatID:    address.ChatID,
		MessageID: address.MessageID,
		Text:      telegramPersonaText(text),
	})
	return err
}

func decodeRestartDeliveryAddress(raw json.RawMessage) (DeliveryAddress, error) {
	address, err := decodeDeliveryAddress(raw)
	if err != nil {
		return DeliveryAddress{}, fmt.Errorf("telegram: restart %w", err)
	}
	if address.MessageID == 0 {
		return DeliveryAddress{}, fmt.Errorf("telegram: restart delivery address missing message_id")
	}
	return address, nil
}
