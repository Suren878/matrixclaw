package controlplane

import (
	"strconv"
	"strings"
)

func voiceThreadsStatus(threads int) string {
	if threads <= 0 {
		return "Auto"
	}
	return strconv.Itoa(threads) + " threads"
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func formatYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "enable", "enabled":
		return true
	default:
		return false
	}
}
