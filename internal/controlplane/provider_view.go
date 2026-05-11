package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func ProviderPickerItems(items []setup.ProviderSetupItem, session *core.Session) []PickerItem {
	out := make([]PickerItem, 0, len(items)+2)
	sessionProviderID := ""
	sessionModelID := ""
	if session != nil {
		sessionProviderID = strings.TrimSpace(session.ProviderID)
		sessionModelID = strings.TrimSpace(session.ModelID)
	}
	for _, provider := range items {
		selected := strings.TrimSpace(provider.ID) == sessionProviderID
		info := providerPickerInfo(provider)
		if selected && sessionModelID != "" {
			info = sessionModelID
		}
		out = append(out, PickerItem{
			ID:       provider.ID,
			Title:    providerPickerTitle(provider),
			Info:     info,
			Selected: selected,
		})
	}
	out = append(out, PickerItem{ID: "custom", Title: "Custom Provider", Role: PickerItemRoleAction})
	out = append(out, CloseItem())
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
	return providerListInfo(provider)
}
