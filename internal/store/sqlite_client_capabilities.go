package store

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func marshalClientCapabilities(capabilities core.ClientCapabilities) string {
	if !capabilities.SupportsVoiceDelivery && !capabilities.SupportsDocumentDelivery {
		return ""
	}
	data, err := json.Marshal(capabilities)
	if err != nil {
		return ""
	}
	return string(data)
}

func unmarshalClientCapabilities(raw string) core.ClientCapabilities {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return core.ClientCapabilities{}
	}
	var capabilities core.ClientCapabilities
	if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
		return core.ClientCapabilities{}
	}
	return capabilities
}
