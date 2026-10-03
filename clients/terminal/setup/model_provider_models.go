package setup

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	setupcore "github.com/Suren878/matrixclaw/internal/setup"
)

type providerModelsLoadedMsg struct {
	seq      int
	response setupcore.ProviderModelsResponse
}

func (m *model) providerModelRows() []listEntry {
	query := m.providerSearchQuery()
	rows := make([]listEntry, 0, len(m.providerModels))
	for i, modelID := range m.providerModels {
		if !matchesProviderSearch(query, modelID) {
			continue
		}
		rows = append(rows, rowEntry(modelID, "", i))
	}
	return rows
}

func (m *model) openProviderModelPicker(ctx context.Context) tea.Cmd {
	m.resetFilter("Find a model")
	m.providerModels = nil
	m.providerModelsLoading = true
	m.providerModelLoadSeq++
	seq := m.providerModelLoadSeq
	provider := m.editingProvider
	m.screen = screenProviderModelList
	return func() tea.Msg {
		return providerModelsLoadedMsg{seq: seq, response: setupcore.ProviderModelCatalog(ctx, provider)}
	}
}

func (m *model) openProviderModelTextEditor(message string) {
	m.openTextEditor(textEditProviderModel, "Model", "model-id", m.editingProvider.Model, false)
	m.formError = strings.TrimSpace(message)
}

func (m *model) loadProviderModels(ctx context.Context) setupcore.ProviderModelsResponse {
	response := setupcore.ProviderModelCatalog(ctx, m.editingProvider)
	if response.Status == setupcore.ProviderModelStatusOK {
		m.setProviderModels(response.Models)
	}
	return response
}

func (m *model) setProviderModels(models []string) {
	m.providerModels = slices.Clone(models)
	m.providerModelCursor = max(0, slices.Index(m.providerModels, m.editingProvider.Effective().Model))
}

func (m *model) handleProviderModelsLoaded(msg providerModelsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.providerModelLoadSeq {
		return m, nil
	}
	m.providerModelsLoading = false
	response := msg.response
	if response.Status != setupcore.ProviderModelStatusOK {
		if response.ManualInput {
			m.openProviderModelTextEditor(manualModelMessage(response.Message))
		} else {
			m.formError = response.Message
			m.screen = screenProviderForm
		}
		return m, nil
	}
	m.setProviderModels(response.Models)
	m.screen = screenProviderModelList
	return m, nil
}

func manualModelMessage(message string) string {
	return strings.TrimRight(strings.TrimSpace(message), ".!?") + ". Enter the model manually."
}

func (m *model) currentProviderModelRowIndex(rows []listEntry) int {
	for i, row := range rows {
		if row.EntryIndex == m.providerModelCursor {
			return i
		}
	}
	return -1
}

func providerModelRowSelection(key string, cursor int, rows []listEntry, closeRole components.Role) (int, components.Event) {
	state := components.ListState{Cursor: cursor, NoWrap: true}
	event := state.Update(key, listEntryItems(rows), closeRole)
	state.Clamp(len(rows))
	return state.Cursor, event
}

func listEntryItems(rows []listEntry) []components.Item {
	items := make([]components.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, components.Item{
			Title:    row.Text,
			Status:   row.Status,
			Disabled: row.Kind != listEntryRow,
		})
	}
	return items
}

func (m *model) clampProviderModelCursor(rows []listEntry) {
	if len(rows) == 0 {
		m.providerModelCursor = 0
		return
	}
	if m.currentProviderModelRowIndex(rows) < 0 {
		m.providerModelCursor = rows[0].EntryIndex
	}
}
