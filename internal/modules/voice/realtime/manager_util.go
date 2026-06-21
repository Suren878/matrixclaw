package realtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func newEvent(sessionID string, eventType EventType, payload any) Event {
	raw, _ := json.Marshal(payload)
	return Event{
		V:              ProtocolVersion,
		Type:           eventType,
		VoiceSessionID: strings.TrimSpace(sessionID),
		Payload:        raw,
		At:             time.Now().UTC(),
	}
}

func decodePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: invalid event payload", ErrInvalidRequest)
	}
	return nil
}

func payloadAs(payload any, target any) bool {
	if payload == nil {
		return false
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return json.Unmarshal(body, target) == nil
}

func toolResultContent(result core.ExecuteToolResult, execErr error) (string, bool) {
	if execErr != nil {
		return execErr.Error(), true
	}
	if result.ToolResultMessage == nil {
		return "ok", false
	}
	content := strings.TrimSpace(result.ToolResultMessage.Content)
	isError := false
	for _, part := range result.ToolResultMessage.Parts {
		if part.ToolResult == nil {
			continue
		}
		if strings.TrimSpace(part.ToolResult.Content) != "" {
			content = strings.TrimSpace(part.ToolResult.Content)
		}
		isError = part.ToolResult.IsError
	}
	if content == "" {
		content = "ok"
	}
	return content, isError
}

func appendTranscript(builder *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte(' ')
	}
	builder.WriteString(text)
}

func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return strings.TrimSpace(prefix) + "_" + hex.EncodeToString(buf)
}
