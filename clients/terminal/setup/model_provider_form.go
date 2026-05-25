package setup

import (
	"strings"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
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
	fields := setup.ProviderFormViewFields(spec, setup.ProviderFormViewOptions{})
	items := make([]providerFormItem, 0, len(fields))
	for _, field := range fields {
		row := listItem{Title: field.Label, Status: field.Status}
		if field.Accent {
			row.Tone = components.RowToneAccent
		}
		row.Disabled = field.Disabled
		item := providerFormItem{
			Row:             row,
			Target:          providerFormTarget(field.ID),
			RequiredMessage: field.RequiredMessage,
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
	field, ok := m.providerFormSpec().Field(setup.ProviderFormFieldBaseURL)
	if !ok || len(field.Choices) == 0 {
		return nil
	}
	items := make([]listItem, 0, len(field.Choices))
	for _, choice := range field.Choices {
		items = append(items, listItem{Title: choice.Title, Status: choice.Status})
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

func (m *model) providerModelUsesPicker() bool {
	field, ok := m.providerFormSpec().Field(setup.ProviderFormFieldModel)
	return ok && field.Picker
}

func (m *model) providerRequiresKeyCheck() bool {
	providerID := strings.TrimSpace(m.editingProvider.CatalogID)
	if providerID == "" {
		providerID = m.editingProvider.ID
	}
	if !providers.PolicyForProvider(providerID, m.editingProvider.Type).RequiresAPIKey {
		return false
	}
	return m.providerModelUsesPicker()
}
