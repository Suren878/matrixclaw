package setup

import "strings"

func ProviderCompactStatus(item ProviderSetupItem) string {
	if model := strings.TrimSpace(item.Model); model != "" {
		return model
	}
	status := strings.TrimSpace(item.Status)
	status = strings.ReplaceAll(status, "Configured · ", "")
	status = strings.ReplaceAll(status, " · Default", "")
	status = strings.ReplaceAll(status, " · Active", "")
	if strings.EqualFold(status, "Configured") {
		return ""
	}
	return status
}
