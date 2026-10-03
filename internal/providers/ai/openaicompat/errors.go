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

// openAIErrorMessage is the error message of a response body, or the body itself.
func openAIErrorMessage(body []byte) string {
	var envelope openAIErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return strings.TrimSpace(envelope.Error.Message)
	}
	return strings.TrimSpace(string(body))
}

func decodeOpenAIError(statusCode int, body []byte) string {
	if message := openAIErrorMessage(body); message != "" {
		return fmt.Sprintf("status %d: %s", statusCode, message)
	}
	return fmt.Sprintf("status %d", statusCode)
}
