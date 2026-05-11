package openaicompat

import (
	"encoding/json"
	"strings"
)

func compactJSONRaw(value string) json.RawMessage {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return json.RawMessage([]byte(value))
	}
	return raw
}
