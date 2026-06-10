package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

type DeliveryAddress struct {
	Kind            string `json:"kind,omitempty"`
	ChatID          int64  `json:"chat_id,omitempty"`
	MessageID       int64  `json:"message_id,omitempty"`
	GuestQueryID    string `json:"guest_query_id,omitempty"`
	InlineMessageID string `json:"inline_message_id,omitempty"`
}

func encodeDeliveryAddress(address DeliveryAddress) json.RawMessage {
	data, err := json.Marshal(address)
	if err != nil {
		return nil
	}
	return data
}

func deliveryAddressFromTarget(target chatTarget, messageID int64) DeliveryAddress {
	if target.isInline() {
		return DeliveryAddress{
			Kind:            telegramTargetInline,
			InlineMessageID: strings.TrimSpace(target.inlineMessageID),
		}
	}
	if target.isGuest() {
		return DeliveryAddress{
			Kind:            telegramTargetGuest,
			GuestQueryID:    strings.TrimSpace(target.guestQueryID),
			InlineMessageID: strings.TrimSpace(target.inlineMessageID),
		}
	}
	return DeliveryAddress{
		Kind:      telegramTargetChat,
		ChatID:    target.chatID,
		MessageID: messageID,
	}
}

func deliveryTargetForMessage(clientName string, target chatTarget, messageID int64) core.ClientDeliveryTarget {
	return core.ClientDeliveryTarget{
		Client:      clientName,
		ExternalKey: target.externalKey,
		Address:     encodeDeliveryAddress(deliveryAddressFromTarget(target, messageID)),
	}
}

func decodeDeliveryAddress(raw json.RawMessage) (DeliveryAddress, error) {
	if len(raw) == 0 {
		return DeliveryAddress{}, fmt.Errorf("delivery address is required")
	}
	var address DeliveryAddress
	if err := json.Unmarshal(raw, &address); err != nil {
		return DeliveryAddress{}, fmt.Errorf("decode delivery address: %w", err)
	}
	address.Kind = strings.TrimSpace(address.Kind)
	switch {
	case address.Kind == telegramTargetGuest || strings.TrimSpace(address.GuestQueryID) != "":
		address.Kind = telegramTargetGuest
		address.GuestQueryID = strings.TrimSpace(address.GuestQueryID)
		address.InlineMessageID = strings.TrimSpace(address.InlineMessageID)
		if address.GuestQueryID == "" {
			return DeliveryAddress{}, fmt.Errorf("delivery address missing guest_query_id")
		}
	case address.Kind == telegramTargetInline || strings.TrimSpace(address.InlineMessageID) != "":
		address.Kind = telegramTargetInline
		address.InlineMessageID = strings.TrimSpace(address.InlineMessageID)
		if address.InlineMessageID == "" {
			return DeliveryAddress{}, fmt.Errorf("delivery address missing inline_message_id")
		}
	case address.Kind == "" || address.Kind == telegramTargetChat:
		address.Kind = telegramTargetChat
		if address.ChatID == 0 {
			return DeliveryAddress{}, fmt.Errorf("delivery address missing chat_id")
		}
	default:
		return DeliveryAddress{}, fmt.Errorf("unsupported delivery address kind %q", address.Kind)
	}
	return address, nil
}

func targetFromDeliveryAddress(raw json.RawMessage, externalKey string) (chatTarget, bool) {
	if len(raw) == 0 {
		return targetFromTelegramExternalKey(externalKey)
	}
	address, err := decodeDeliveryAddress(raw)
	if err != nil {
		return targetFromTelegramExternalKey(externalKey)
	}
	if address.Kind == telegramTargetGuest {
		target := chatTarget{
			kind:            telegramTargetGuest,
			guestQueryID:    address.GuestQueryID,
			inlineMessageID: address.InlineMessageID,
			externalKey:     externalKey,
		}
		if strings.TrimSpace(target.externalKey) == "" {
			target.externalKey = telegramGuestExternalKey(address.GuestQueryID)
		}
		return target, true
	}
	if address.Kind == telegramTargetInline {
		return chatTarget{
			kind:            telegramTargetInline,
			inlineMessageID: address.InlineMessageID,
			externalKey:     strings.TrimSpace(externalKey),
		}, true
	}
	target := chatTarget{
		kind:        telegramTargetChat,
		chatID:      address.ChatID,
		messageID:   address.MessageID,
		externalKey: telegramExternalKey(address.ChatID),
	}
	if target.chatID == 0 {
		target.externalKey = strings.TrimSpace(externalKey)
	}
	return target, true
}

func targetFromTelegramExternalKey(externalKey string) (chatTarget, bool) {
	externalKey = strings.TrimSpace(externalKey)
	if externalKey == "" {
		return chatTarget{}, false
	}
	if guestQueryID, ok := strings.CutPrefix(externalKey, "guest:"); ok {
		guestQueryID = strings.TrimSpace(guestQueryID)
		if guestQueryID == "" {
			return chatTarget{}, false
		}
		return chatTarget{
			kind:         telegramTargetGuest,
			guestQueryID: guestQueryID,
			externalKey:  telegramGuestExternalKey(guestQueryID),
		}, true
	}
	parts := strings.Split(externalKey, ":")
	if len(parts) > 2 {
		return chatTarget{}, false
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || chatID == 0 {
		return chatTarget{}, false
	}
	target := chatTarget{kind: telegramTargetChat, chatID: chatID, externalKey: telegramExternalKey(chatID)}
	if len(parts) == 2 {
		if _, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64); err != nil {
			return chatTarget{}, false
		}
	}
	return target, true
}

func telegramExternalKey(chatID int64) string {
	return strconv.FormatInt(chatID, 10)
}

func telegramGuestExternalKey(guestQueryID string) string {
	return "guest:" + strings.TrimSpace(guestQueryID)
}
