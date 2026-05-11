package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

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

func (m *model) openProviderModelPicker(ctx context.Context) error {
	m.resetFilter("Find a model")
	if err := m.loadProviderModels(ctx); err != nil {
		return err
	}
	m.screen = screenProviderModelList
	return nil
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

func (m *model) currentProviderModelRowIndex(rows []listEntry) int {
	for i, row := range rows {
		if row.EntryIndex == m.providerModelCursor {
			return i
		}
	}
	return -1
}

func (m *model) moveProviderModelCursor(key string, rows []listEntry) bool {
	if len(rows) == 0 {
		return false
	}
	current := m.currentProviderModelRowIndex(rows)
	if current < 0 {
		m.providerModelCursor = rows[0].EntryIndex
		return true
	}
	next := current
	if !m.moveIndex(key, &next, len(rows)-1) || next == current {
		return false
	}
	m.providerModelCursor = rows[next].EntryIndex
	return true
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
