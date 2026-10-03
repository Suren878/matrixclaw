package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// providerPickerItems lists the providers for session; only the owner gets
// the ones still to configure and the custom provider.
func providerPickerItems(items []setup.ProviderSetupItem, session *core.Session, owner bool) []PickerItem {
	out := make([]PickerItem, 0, len(items)+2)
	sessionProviderID := ""
	sessionModelID := ""
	if session != nil {
		sessionProviderID = strings.TrimSpace(session.ProviderID)
		sessionModelID = strings.TrimSpace(session.ModelID)
	}
	for _, provider := range items {
		if !owner && !provider.Configured {
			continue
		}
		selected := strings.TrimSpace(provider.ID) == sessionProviderID
		info := providerPickerInfo(provider)
		if selected && sessionModelID != "" {
			info = sessionModelID
		}
		command := providerCommand(provider.ID)
		if provider.Configured && (!selected || !owner) {
			command = providerCommand("use", provider.ID)
		}
		out = append(out, PickerItem{
			ID:       provider.ID,
			Title:    providerPickerTitle(provider),
			Info:     info,
			Selected: selected,
			Command:  command,
		})
	}
	if owner {
		out = append(out, PickerItem{ID: "custom", Title: "Custom Provider", Command: customProviderCommand(), Role: PickerItemRoleAction})
	}
	return out
}

func providerPickerTitle(provider setup.ProviderSetupItem) string {
	if title := strings.TrimSpace(provider.Name); title != "" {
		return title
	}
	return strings.TrimSpace(provider.ID)
}

func providerPickerInfo(provider setup.ProviderSetupItem) string {
	if !provider.Configured {
		return ""
	}
	return strings.TrimSpace(provider.Model)
}
