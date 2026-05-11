package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Suren878/matrixclaw/internal/core"
)

type DeliveryAddress struct {
	ChatID    int64 `json:"chat_id"`
	ThreadID  int64 `json:"thread_id,omitempty"`
	MessageID int64 `json:"message_id,omitempty"`
}

func encodeDeliveryAddress(address DeliveryAddress) json.RawMessage {
	data, err := json.Marshal(address)
	if err != nil {
		return nil
	}
	return data
}

func deliveryAddressFromTarget(target chatTarget, messageID int64) DeliveryAddress {
	return DeliveryAddress{
		ChatID:    target.chatID,
		ThreadID:  target.threadID,
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
	if address.ChatID == 0 {
		return DeliveryAddress{}, fmt.Errorf("delivery address missing chat_id")
	}
	return address, nil
}

func targetFromDeliveryAddress(raw json.RawMessage, externalKey string) (chatTarget, bool) {
	address, err := decodeDeliveryAddress(raw)
	if err != nil {
		return chatTarget{}, false
	}
	target := chatTarget{
		chatID:      address.ChatID,
		threadID:    address.ThreadID,
		messageID:   address.MessageID,
		externalKey: externalKey,
	}
	if target.externalKey == "" {
		target.externalKey = telegramExternalKey(target.chatID, target.threadID)
	}
	return target, true
}

func telegramExternalKey(chatID int64, threadID int64) string {
	if threadID > 0 {
		return strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(threadID, 10)
	}
	return strconv.FormatInt(chatID, 10)
}
