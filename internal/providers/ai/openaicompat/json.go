package openaicompat

import (
	"encoding/json"
	"strings"
)

// toolArguments is a tool call's arguments compacted; blank ones, which some
// gateways send for a call without parameters, are {}.
func toolArguments(value string) json.RawMessage {
	if strings.TrimSpace(value) == "" {
		return json.RawMessage("{}")
	}
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return json.RawMessage([]byte(value))
	}
	return raw
}
