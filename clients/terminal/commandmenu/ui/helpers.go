package commandui

import "strings"

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func selectableCount(items []Item) int {
	count := 0
	for _, item := range items {
		if !item.Selectable() || item.Role == RoleBack || item.Role == RoleCancel || item.Role == RoleExit {
			continue
		}
		count++
	}
	return count
}
