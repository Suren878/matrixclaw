package providers

import "strings"

type ToolUseMode string

const (
	ToolUseNative   ToolUseMode = "native"
	ToolUseDisabled ToolUseMode = "disabled"
)

type RuntimeProfile struct {
	ToolUseMode ToolUseMode
}

func NormalizeRequest(request Request, profile RuntimeProfile) Request {
	profile = NormalizeRuntimeProfile(profile)
	normalized := request
	normalized.Messages = NormalizeMessages(request.Messages, profile.ToolUseMode)
	normalized.Tools = NormalizeTools(request.Tools, profile.ToolUseMode)
	return normalized
}

func NormalizeRuntimeProfile(profile RuntimeProfile) RuntimeProfile {
	profile.ToolUseMode = NormalizeToolUseMode(profile.ToolUseMode)
	return profile
}

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

func NormalizeTools(tools []ToolDefinition, mode ToolUseMode) []ToolDefinition {
	if len(tools) == 0 || mode == ToolUseDisabled {
		return nil
	}
	out := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" {
			continue
		}
		tool.Description = strings.TrimSpace(tool.Description)
		out = append(out, tool)
	}
	return out
}
