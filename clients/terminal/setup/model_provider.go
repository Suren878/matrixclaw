package setup

import (
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) handleProviderFormSave() error {
	m.trimProviderFormFields()
	if message := m.providerFormRequiredMessage(); message != "" {
		return errors.New(message)
	}
	if !m.editingProvider.HasStoredAPIKey && (m.providerRequiresKeyCheck() || strings.TrimSpace(m.editingProvider.APIKey) != "") {
		return errors.New("enter a working API key first")
	}
	if m.providerSupportsReasoningEffort() {
		if m.editingProvider.ReasoningEffort == "" {
			m.editingProvider.ReasoningEffort = providers.DefaultReasoningEffortForModel(m.editingProvider.CatalogID, m.editingProvider.Type, m.editingProvider.Model)
		}
	} else {
		m.editingProvider.ReasoningEffort = ""
	}
	if m.providerSupportsToolUse() {
		m.editingProvider.ToolUseMode = providers.NormalizeToolUseMode(m.editingProvider.ToolUseMode)
	} else {
		m.editingProvider.ToolUseMode = ""
	}

	m.draft = setup.UpsertProviderDraft(m.draft, m.editingProvider)
	m.draft.ActiveProviderID = m.editingProvider.ID
	return m.saveDraftAndReturn(screenProviderList)
}

func (m *model) trimProviderFormFields() {
	m.editingProvider.Name = strings.TrimSpace(m.editingProvider.Name)
	m.editingProvider.APIKey = strings.TrimSpace(m.editingProvider.APIKey)
	m.editingProvider.BaseURL = strings.TrimSpace(m.editingProvider.BaseURL)
	m.editingProvider.Model = strings.TrimSpace(m.editingProvider.Model)
	m.editingProvider.ReasoningEffort = strings.TrimSpace(m.editingProvider.ReasoningEffort)
}

func (m *model) providerFormRequiredMessage() string {
	for _, item := range m.providerFormItems() {
		if item.RequiredMessage != "" && strings.TrimSpace(item.Row.Status) == "" {
			return item.RequiredMessage
		}
	}
	return ""
}
