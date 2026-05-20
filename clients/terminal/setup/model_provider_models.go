package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
)

type providerModelsLoadedMsg struct {
	seq    int
	models []string
	err    error
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
		models, err := m.service.ProviderModels(ctx, provider)
		return providerModelsLoadedMsg{seq: seq, models: models, err: err}
	}
}

func (m *model) openProviderModelTextEditor(message string) {
	m.openTextEditor(textEditProviderModel, "Model", "model-id", m.editingProvider.Model, false)
	m.formError = strings.TrimSpace(message)
}

func modelDiscoveryErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("Could not load remote models: %s. Enter the model manually.", err)
}

func (m *model) loadProviderModels(ctx context.Context) error {
	models, err := m.service.ProviderModels(ctx, m.editingProvider)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return errors.New("no models available")
	}
	m.providerModels = append([]string(nil), models...)
	m.providerModelCursor = 0
	for i, modelID := range m.providerModels {
		if strings.TrimSpace(modelID) == strings.TrimSpace(m.editingProvider.Model) {
			m.providerModelCursor = i
			break
		}
	}
	return nil
}

func (m *model) handleProviderModelsLoaded(msg providerModelsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.providerModelLoadSeq {
		return m, nil
	}
	m.providerModelsLoading = false
	if msg.err != nil {
		m.openProviderModelTextEditor(modelDiscoveryErrorMessage(msg.err))
		return m, nil
	}
	if len(msg.models) == 0 {
		m.openProviderModelTextEditor(modelDiscoveryErrorMessage(errors.New("no models available")))
		return m, nil
	}
	m.providerModels = append([]string(nil), msg.models...)
	m.providerModelCursor = 0
	for i, modelID := range m.providerModels {
		if strings.TrimSpace(modelID) == strings.TrimSpace(m.editingProvider.Model) {
			m.providerModelCursor = i
			break
		}
	}
	m.screen = screenProviderModelList
	return m, nil
}

func (m *model) currentProviderModelRowIndex(rows []listEntry) int {
	for i, row := range rows {
		if row.EntryIndex == m.providerModelCursor {
			return i
		}
	}
	return -1
}

func providerModelRowSelection(key string, cursor int, rows []listEntry, closeRole commandui.Role) (int, commandui.Event) {
	state := commandui.ListState{Cursor: cursor, NoWrap: true}
	event := state.Update(key, listEntryItems(rows), closeRole)
	state.Clamp(len(rows))
	return state.Cursor, event
}

func listEntryItems(rows []listEntry) []commandui.Item {
	items := make([]commandui.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, commandui.Item{
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
