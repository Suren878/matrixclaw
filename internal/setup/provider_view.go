package setup

import "strings"

func ProviderSetupItemsFromConfig(cfg Config, options []ProviderOption) []ProviderSetupItem {
	providers := make([]providerItemSource, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		providers = append(providers, providerConfigItemSource(provider))
	}
	return providerSetupItems(providers, cfg.ActiveProviderID, options)
}

func ProviderDisplayName(item ProviderSetupItem) string {
	if name := strings.TrimSpace(item.Name); name != "" {
		return name
	}
	return strings.TrimSpace(item.ID)
}

func ProviderDisplayStatus(item ProviderSetupItem) string {
	if !item.Configured {
		if item.Implemented {
			return ""
		}
		return "Planned"
	}
	parts := []string{}
	if model := strings.TrimSpace(item.Model); model != "" {
		parts = append(parts, model)
	}
	if item.Active {
		parts = append(parts, "Active")
	}
	return strings.Join(parts, " · ")
}

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
