package setup

import (
	"slices"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type providerField int

const (
	providerFieldName providerField = iota
	providerFieldBaseURL
	providerFieldAPIKey
	providerFieldModel
	providerFieldReasoning
	providerFieldToolUse
)

var toolUseModes = []providers.ToolUseMode{providers.ToolUseNative, providers.ToolUseDisabled}

type providerFormItem struct {
	Row      listItem
	Field    providerField
	Required string
}

// providerFormView is what the provider form shows for the provider being
// edited: its effective values and the catalog facts that pick the fields.
type providerFormView struct {
	effective    setup.ProviderConfig
	policy       providers.ProviderPolicy
	capabilities providers.ModelCapabilitySet
}

func (m *model) providerForm() providerFormView {
	effective := m.editingProvider.Effective()
	return providerFormView{
		effective: effective,
		policy:    providers.PolicyForProvider(m.editingProvider.ID, m.editingProvider.Type),
		capabilities: providers.ResolveModelCapabilities(providers.ModelCapabilityInput{
			ProviderID: effective.ID, ProviderType: effective.Type, ModelID: effective.Model,
		}),
	}
}

func (v providerFormView) custom() bool {
	return !v.policy.Known
}

func (v providerFormView) modelPicker() bool {
	return !v.custom() && v.capabilities.ProviderCapabilities.ModelDiscovery
}

// modelNeedsKey: the model list can't load until an API key is entered.
func (v providerFormView) modelNeedsKey() bool {
	if !v.modelPicker() || !v.policy.RequiresAPIKey || v.policy.PublicModelCatalog {
		return false
	}
	_, ok := v.effective.Runtime()
	return !ok
}

func (m *model) providerFormItems() []providerFormItem {
	view := m.providerForm()
	p := m.editingProvider
	items := make([]providerFormItem, 0, 6)
	if view.custom() {
		items = append(items, providerFormItem{Row: listItem{Title: "Provider name", Status: p.Name}, Field: providerFieldName, Required: "provider name is required"})
	}
	if options := view.policy.BaseURLOptions; view.custom() || len(options) > 0 {
		title, status := "Base URL", view.effective.BaseURL
		if len(options) > 0 {
			title = "Endpoint"
			if i := baseURLOptionIndex(options, status); i >= 0 {
				status = options[i].Name
			}
		}
		items = append(items, providerFormItem{Row: listItem{Title: title, Status: status}, Field: providerFieldBaseURL, Required: "provider base URL is required"})
	}
	if view.policy.RequiresAPIKey {
		items = append(items, providerFormItem{Row: listItem{Title: "API key", Status: p.APIKeyPreview()}, Field: providerFieldAPIKey, Required: "provider API key is required"})
	}
	modelRow := listItem{Title: "Model", Status: view.effective.Model}
	if view.modelNeedsKey() {
		modelRow.Status, modelRow.Disabled = "API key required", true
	} else if modelRow.Status != "" {
		modelRow.Tone = components.RowToneAccent
	}
	items = append(items, providerFormItem{Row: modelRow, Field: providerFieldModel, Required: "provider model is required"})
	if view.capabilities.ProviderCapabilities.ReasoningEffort {
		items = append(items, providerFormItem{Row: listItem{Title: "Reasoning effort", Status: view.effective.ReasoningEffort}, Field: providerFieldReasoning})
	}
	if view.capabilities.ProviderCapabilities.ToolCalling {
		items = append(items, providerFormItem{Row: listItem{Title: "Tool use", Status: toolUseLabel(view.effective.ToolUseMode)}, Field: providerFieldToolUse})
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

func baseURLOptionIndex(options []providers.BaseURLOption, url string) int {
	return slices.IndexFunc(options, func(option providers.BaseURLOption) bool {
		return strings.TrimSpace(option.URL) == strings.TrimSpace(url)
	})
}

func (m *model) providerBaseURLItems() []listItem {
	options := m.providerForm().policy.BaseURLOptions
	items := make([]listItem, 0, len(options))
	for _, option := range options {
		items = append(items, listItem{Title: option.Name, Status: option.URL})
	}
	return items
}

func (m *model) providerReasoningEfforts() []string {
	return m.providerForm().capabilities.ReasoningEfforts
}

func (m *model) providerReasoningItems() []listItem {
	efforts := m.providerReasoningEfforts()
	items := make([]listItem, 0, len(efforts))
	for _, effort := range efforts {
		items = append(items, listItem{Title: cases.Title(language.Und).String(effort)})
	}
	return items
}

func toolUseItems() []listItem {
	items := make([]listItem, 0, len(toolUseModes))
	for _, mode := range toolUseModes {
		items = append(items, listItem{Title: toolUseLabel(mode)})
	}
	return items
}

func toolUseLabel(mode providers.ToolUseMode) string {
	if providers.NormalizeToolUseMode(mode) == providers.ToolUseDisabled {
		return "Disabled"
	}
	return "Enabled"
}

func (m *model) providerFormSubtitle() string {
	if strings.TrimSpace(m.editingProvider.APIKey) == "" {
		return ""
	}
	return "Saved key " + setup.MaskSecret(m.editingProvider.APIKey)
}

func (m *model) providerAPIKeyPlaceholder() string {
	if strings.TrimSpace(m.editingProvider.APIKey) != "" {
		return "Leave empty to keep " + setup.MaskSecret(m.editingProvider.APIKey)
	}
	return "Enter your API key"
}

// providerChecksKey: a new key is checked by loading the model list.
func (m *model) providerChecksKey() bool {
	view := m.providerForm()
	return view.policy.RequiresAPIKey && view.modelPicker()
}
