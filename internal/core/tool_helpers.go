package core

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func marshalJSONRaw(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func errorText(result tools.Result) string {
	if result.IsError {
		return strings.TrimSpace(result.Content)
	}
	return ""
}

func toolResultStatus(result tools.Result) tools.ResultStatus {
	if result.Status != "" {
		return result.Status
	}
	if result.IsError {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

func normalizeToolContent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Tool completed"
	}
	return value
}

func toolResultCallIDs(messages []Message) map[string]struct{} {
	resultIDs := make(map[string]struct{})
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			toolCallID := strings.TrimSpace(part.ToolResult.ToolCallID)
			if toolCallID == "" {
				continue
			}
			resultIDs[toolCallID] = struct{}{}
		}
	}
	return resultIDs
}
