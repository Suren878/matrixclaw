package store

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// storedCapabilities always writes receives_deliveries, so a stored row tells
// a false flag from a row written before the flag existed.
type storedCapabilities struct {
	core.ClientCapabilities
	ReceivesDeliveries *bool `json:"receives_deliveries"`
}

func marshalClientCapabilities(capabilities core.ClientCapabilities) string {
	if capabilities == (core.ClientCapabilities{}) {
		return ""
	}
	data, err := json.Marshal(storedCapabilities{ClientCapabilities: capabilities, ReceivesDeliveries: &capabilities.ReceivesDeliveries})
	if err != nil {
		return ""
	}
	return string(data)
}

// unmarshalClientCapabilities reads stored capabilities; rows written before
// receives_deliveries existed came only from Telegram, which fetches them.
func unmarshalClientCapabilities(raw string) core.ClientCapabilities {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return core.ClientCapabilities{}
	}
	var stored storedCapabilities
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return core.ClientCapabilities{}
	}
	capabilities := stored.ClientCapabilities
	if stored.ReceivesDeliveries != nil {
		capabilities.ReceivesDeliveries = *stored.ReceivesDeliveries
	} else {
		capabilities.ReceivesDeliveries = capabilities.SupportsVoiceDelivery || capabilities.SupportsDocumentDelivery
	}
	return capabilities
}
