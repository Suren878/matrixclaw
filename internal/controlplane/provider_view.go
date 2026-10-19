package controlplane

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// providerPickerItems lists the providers for session: a configured one opens
// its models. Only the owner gets the ones still to configure, the custom
// provider and the default for new sessions.
func providerPickerItems(items []setup.ProviderSetupItem, session core.Session, owner bool) []PickerItem {
	out := make([]PickerItem, 0, len(items)+2)
	for _, provider := range items {
		if !owner && !provider.Configured {
			continue
		}
		selected := strings.EqualFold(strings.TrimSpace(provider.ID), strings.TrimSpace(session.ProviderID))
		info := providerPickerInfo(provider)
		if selected && strings.TrimSpace(session.ModelID) != "" {
			info = strings.TrimSpace(session.ModelID)
		}
		command := providerEdit.command(provider.ID)
		if provider.Configured {
			command = providerModels.command(session.ID, provider.ID)
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
		out = append(out,
			PickerItem{ID: "default", Title: "Default for new sessions", Command: providerDefault.command(), Role: PickerItemRoleAction},
			PickerItem{ID: "custom", Title: "Custom Provider", Command: providerCustom.command(), Role: PickerItemRoleAction},
		)
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
