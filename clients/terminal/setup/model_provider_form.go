package setup

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) providerFormSubtitle() string {
	selected := m.editingProvider
	parts := []string{}
	if selected.HasStoredAPIKey {
		parts = append(parts, "Saved key "+selected.StoredAPIKeyPreview)
	}
	return strings.Join(parts, " ")
}

type providerFormItem struct {
	Row             listItem
	Target          textEditTarget
	RequiredMessage string
	Reasoning       bool
}

func (m *model) providerFormItems() []providerFormItem {
	items := make([]providerFormItem, 0, 5)
	if m.editingProviderIsCustom() {
		items = append(items,
			providerFormItem{Row: listItem{Title: "Provider name", Status: m.editingProvider.Name}, Target: textEditProviderName, RequiredMessage: "provider name is required"},
			providerFormItem{Row: listItem{Title: "Base URL", Status: m.editingProvider.BaseURL}, Target: textEditProviderBaseURL, RequiredMessage: "provider base URL is required"},
		)
	}
	items = append(items,
		providerFormItem{Row: listItem{Title: "API key", Status: m.providerAPIKeyValue()}, Target: textEditProviderAPIKey, RequiredMessage: "provider API key is required"},
		providerFormItem{Row: listItem{Title: "Model", Status: m.editingProvider.Model}, Target: textEditProviderModel, RequiredMessage: "provider model is required"},
	)
	if m.providerSupportsReasoningEffort() {
		items = append(items, providerFormItem{
			Row:       listItem{Title: "Reasoning effort", Status: nonEmpty(m.editingProvider.ReasoningEffort, "medium")},
			Reasoning: true,
		})
	}
	return items
}

func providerFormRows(items []providerFormItem) []listItem {
	rows := make([]listItem, 0, len(items))
	for _, item := range items {
		rows = append(rows, item.Row)
	}
	return rows
}

func (m *model) reasoningEffortIndex() int {
	current := strings.TrimSpace(m.editingProvider.ReasoningEffort)
	for i, effort := range setup.ReasoningEfforts() {
		if effort == current {
			return i
		}
	}
	return 1
}

func (m *model) providerSupportsReasoningEffort() bool {
	providerID := strings.TrimSpace(m.editingProvider.CatalogID)
	if providerID == "" {
		providerID = strings.TrimSpace(m.editingProvider.ID)
	}
	return providers.DefaultReasoningEffortForProvider(providerID, strings.TrimSpace(m.editingProvider.Type)) != ""
}

func (m *model) providerAPIKeyPlaceholder() string {
	if m.editingProvider.HasStoredAPIKey {
		return "Leave empty to keep " + m.editingProvider.StoredAPIKeyPreview
	}
	return "Enter your API key"
}

func (m *model) providerAPIKeyValue() string {
	if strings.TrimSpace(m.editingProvider.APIKey) != "" {
		return setup.MaskSecret(m.editingProvider.APIKey)
	}
	if m.editingProvider.HasStoredAPIKey {
		return m.editingProvider.StoredAPIKeyPreview
	}
	return ""
}

func (m *model) providerRequiresKeyCheck() bool {
	if m.editingProviderIsCustom() {
		return false
	}
	providerID := strings.TrimSpace(m.editingProvider.CatalogID)
	if providerID == "" {
		providerID = strings.TrimSpace(m.editingProvider.ID)
	}
	entry, ok := providers.CatalogEntryByID(providerID)
	return ok && entry.Capabilities.ModelDiscovery
}

func (m *model) editingProviderIsCustom() bool {
	return strings.TrimSpace(m.editingProvider.CatalogID) == ""
}
