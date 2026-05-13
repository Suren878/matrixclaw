package setup

import (
	"strings"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
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
	BaseURL         bool
	Reasoning       bool
	ToolUse         bool
}

func (m *model) providerFormItems() []providerFormItem {
	spec := m.providerFormSpec()
	items := make([]providerFormItem, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		row := listItem{Title: field.Label, Status: field.Status}
		if field.ID == setup.ProviderFormFieldModel && strings.TrimSpace(field.Status) != "" {
			row.Tone = commandui.RowToneAccent
		}
		item := providerFormItem{
			Row:             row,
			Target:          providerFormTarget(field.ID),
			RequiredMessage: providerFormRequiredMessage(field),
			BaseURL:         field.ID == setup.ProviderFormFieldBaseURL && field.Picker,
			Reasoning:       field.ID == setup.ProviderFormFieldReasoningEffort,
			ToolUse:         field.ID == setup.ProviderFormFieldToolUse,
		}
		items = append(items, item)
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

func (m *model) providerBaseURLOptions() []string {
	field, ok := m.providerFormSpec().Field(setup.ProviderFormFieldBaseURL)
	if !ok || len(field.Options) == 0 {
		return nil
	}
	return append([]string(nil), field.Options...)
}

func (m *model) providerBaseURLItems() []listItem {
	catalogID := providers.NormalizeProviderID(m.editingProvider.CatalogID)
	if catalogID == "" {
		catalogID = providers.NormalizeProviderID(m.editingProvider.ID)
	}
	entry, ok := providers.CatalogEntryByID(catalogID)
	if !ok || len(entry.BaseURLOptions) == 0 {
		options := m.providerBaseURLOptions()
		items := make([]listItem, 0, len(options))
		for _, option := range options {
			items = append(items, listItem{Title: option})
		}
		return items
	}
	items := make([]listItem, 0, len(entry.BaseURLOptions))
	for _, option := range entry.BaseURLOptions {
		items = append(items, listItem{Title: option.Name, Status: option.URL})
	}
	return items
}

func (m *model) providerBaseURLIndex() int {
	current := strings.TrimSpace(m.editingProvider.BaseURL)
	for i, value := range m.providerBaseURLOptions() {
		if strings.TrimSpace(value) == current {
			return i
		}
	}
	return 0
}

func (m *model) reasoningEffortIndex() int {
	current := strings.TrimSpace(m.editingProvider.ReasoningEffort)
	for i, effort := range m.providerReasoningEfforts() {
		if effort == current {
			return i
		}
	}
	return defaultReasoningEffortIndex(m.providerReasoningEfforts())
}

func (m *model) providerReasoningEfforts() []string {
	field, ok := m.providerFormSpec().Field(setup.ProviderFormFieldReasoningEffort)
	if !ok || len(field.Options) == 0 {
		return nil
	}
	return append([]string(nil), field.Options...)
}

func (m *model) providerSupportsReasoningEffort() bool {
	_, ok := m.providerFormSpec().Field(setup.ProviderFormFieldReasoningEffort)
	return ok
}

func (m *model) providerSupportsToolUse() bool {
	_, ok := m.providerFormSpec().Field(setup.ProviderFormFieldToolUse)
	return ok
}

func defaultReasoningEffortIndex(efforts []string) int {
	for i, effort := range efforts {
		if effort == providers.DefaultReasoningEffort {
			return i
		}
	}
	return 0
}

func (m *model) toolUseModeIndex() int {
	current := providers.NormalizeToolUseMode(m.editingProvider.ToolUseMode)
	for i, mode := range setup.ProviderFormToolUseModes() {
		if mode == current {
			return i
		}
	}
	return 0
}

func (m *model) providerAPIKeyPlaceholder() string {
	if m.editingProvider.HasStoredAPIKey {
		return "Leave empty to keep " + m.editingProvider.StoredAPIKeyPreview
	}
	return "Enter your API key"
}

func (m *model) providerFormSpec() setup.ProviderFormSpec {
	return setup.ProviderFormSpecForDraft(m.editingProvider)
}

func providerFormTarget(fieldID setup.ProviderFormFieldID) textEditTarget {
	switch fieldID {
	case setup.ProviderFormFieldName:
		return textEditProviderName
	case setup.ProviderFormFieldBaseURL:
		return textEditProviderBaseURL
	case setup.ProviderFormFieldAPIKey:
		return textEditProviderAPIKey
	case setup.ProviderFormFieldModel:
		return textEditProviderModel
	default:
		return textEditNone
	}
}

func providerFormRequiredMessage(field setup.ProviderFormField) string {
	if !field.Required {
		return ""
	}
	switch field.ID {
	case setup.ProviderFormFieldName:
		return "provider name is required"
	case setup.ProviderFormFieldBaseURL:
		return "provider base URL is required"
	case setup.ProviderFormFieldAPIKey:
		return "provider API key is required"
	case setup.ProviderFormFieldModel:
		return "provider model is required"
	default:
		return ""
	}
}

func (m *model) providerModelUsesPicker() bool {
	field, ok := m.providerFormSpec().Field(setup.ProviderFormFieldModel)
	return ok && field.Picker
}

func (m *model) providerRequiresKeyCheck() bool {
	return m.providerModelUsesPicker()
}
