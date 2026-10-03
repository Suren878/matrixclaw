package providers

import "strings"

type ToolUseMode string

const (
	ToolUseNative   ToolUseMode = "native"
	ToolUseDisabled ToolUseMode = "disabled"
)

func NormalizeToolUseMode(value ToolUseMode) ToolUseMode {
	switch ToolUseMode(strings.ToLower(strings.TrimSpace(string(value)))) {
	case ToolUseNative:
		return ToolUseNative
	case ToolUseDisabled:
		return ToolUseDisabled
	default:
		return ToolUseNative
	}
}

func NormalizeOptionalToolUseMode(value ToolUseMode) ToolUseMode {
	switch ToolUseMode(strings.ToLower(strings.TrimSpace(string(value)))) {
	case "":
		return ""
	case ToolUseNative:
		return ToolUseNative
	case ToolUseDisabled:
		return ToolUseDisabled
	default:
		return ""
	}
}

func NormalizeMessages(messages []Message, mode ToolUseMode) []Message {
	if mode != ToolUseDisabled {
		return messages
	}
	out := make([]Message, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.Role) == "tool" {
			continue
		}
		message.ToolCallID = ""
		if len(message.ToolCalls) == 0 {
			out = append(out, message)
			continue
		}
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		message.ToolCalls = nil
		out = append(out, message)
	}
	return out
}

// Label is the mode as setup screens show it.
func (m ToolUseMode) Label() string {
	if NormalizeToolUseMode(m) == ToolUseDisabled {
		return "Disabled"
	}
	return "Enabled"
}
