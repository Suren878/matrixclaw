package openaicompat

import (
	"encoding/json"
	"fmt"
	"strings"
)

type openAIErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func decodeOpenAIError(statusCode int, body []byte) string {
	var envelope openAIErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return fmt.Sprintf("status %d: %s", statusCode, strings.TrimSpace(envelope.Error.Message))
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Sprintf("status %d", statusCode)
	}
	return fmt.Sprintf("status %d: %s", statusCode, text)
}
