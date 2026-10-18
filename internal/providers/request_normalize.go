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

// Label is the mode as setup screens show it.
func (m ToolUseMode) Label() string {
	if NormalizeToolUseMode(m) == ToolUseDisabled {
		return "Disabled"
	}
	return "Enabled"
}
