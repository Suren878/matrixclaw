package setup

import (
	"context"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) updateProviderList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		entries := m.providerEntries()
		event := m.updateListSelection(msg.String(), &m.cursor, len(entries))
		switch msg.String() {
		case "ctrl+a":
			m.providerTypeCursor = 0
			m.screen = screenProviderTypeList
			return m, nil
		}
		switch event.Kind {
		case components.EventBack:
			m.returnToList(screenDaemonList)
			return m, nil
		case components.EventSelect:
			if len(entries) == 0 {
				return m, nil
			}
			selected := entries[m.cursor]
			m.formError = ""
			switch selected.Kind {
			case providerEntryConfigured:
				m.editingProvider = selected.Provider
				m.openForm(screenProviderForm)
				return m, nil
			case providerEntryAvailable:
				m.editingProvider = selected.Provider
				m.openForm(screenProviderForm)
				return m, nil
			case providerEntryContinue:
				if len(m.cfg.Providers) == 0 {
					m.providerNoProviderCursor = 0
					m.screen = screenProviderNoProviderConfirm
					return m, nil
				}
				m.openForm(screenAssistantForm)
				return m, nil
			}
		}
	}

	cmd := m.filterInput.Update(msg)
	entries := m.providerEntries()
	if m.cursor >= len(entries) {
		m.cursor = max(0, len(entries)-1)
	}
	return m, cmd
}

func (m *model) updateProviderNoProviderConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	event := updateConfirmSelection(keyMsg.String(), &m.providerNoProviderCursor)
	switch event.Kind {
	case components.EventCancel:
		m.screen = screenProviderList
		return m, nil
	case components.EventSubmit:
		m.openForm(screenAssistantForm)
		return m, nil
	}
	return m, nil
}

func (m *model) updateProviderTypeList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	event := m.updateListSelection(keyMsg.String(), &m.providerTypeCursor, 2)
	switch event.Kind {
	case components.EventBack:
		m.screen = screenProviderList
		return m, nil
	case components.EventSelect:
		providerType := providers.TypeOpenAICompat
		if m.providerTypeCursor == 1 {
			providerType = providers.TypeAnthropic
		}
		provider, err := m.newCustomProvider(providerType)
		if err != nil {
			m.formError = err.Error()
			return m, nil
		}
		m.editingProvider = provider
		m.openForm(screenProviderForm)
		return m, nil
	}
	return m, nil
}

func (m *model) updateProviderForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	items := m.providerFormItems()
	return m.updateForm(msg, len(items), func() { m.returnToList(screenProviderList) }, m.handleProviderFormSave, func() tea.Cmd {
		if m.formFocus < 0 || m.formFocus >= len(items) {
			return nil
		}
		item := items[m.formFocus]
		view := m.providerForm()
		switch item.Field {
		case providerFieldName:
			m.openTextEditor(textEditProviderName, "Provider Name", "Provider name", m.editingProvider.Name, false)
		case providerFieldAPIKey:
			m.openTextEditor(textEditProviderAPIKey, "API Key", m.providerAPIKeyPlaceholder(), "", true)
		case providerFieldModel:
			if item.Row.Disabled {
				m.formError = "Enter an API key before loading models."
				return nil
			}
			if view.modelPicker() {
				return m.openProviderModelPicker(context.Background())
			}
			m.openTextEditor(textEditProviderModel, "Model", "model-id", view.effective.Model, false)
		case providerFieldBaseURL:
			if options := view.policy.BaseURLOptions; len(options) > 0 {
				m.providerBaseURLCursor = max(0, baseURLOptionIndex(options, view.effective.BaseURL))
				m.screen = screenProviderBaseURLList
			} else {
				m.openTextEditor(textEditProviderBaseURL, "Base URL", "https://api.example.com/v1", m.editingProvider.BaseURL, false)
			}
		case providerFieldReasoning:
			efforts := view.capabilities.ReasoningEfforts
			m.providerEffortCursor = max(0, slices.Index(efforts, view.effective.ReasoningEffort))
			m.screen = screenProviderEffortList
		case providerFieldToolUse:
			m.providerToolUseCursor = max(0, slices.Index(toolUseModes, providers.NormalizeToolUseMode(view.effective.ToolUseMode)))
			m.screen = screenProviderToolUseList
		}
		return nil
	})
}

func (m *model) updateProviderBaseURLList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	options := m.providerForm().policy.BaseURLOptions
	event := m.updateListSelection(keyMsg.String(), &m.providerBaseURLCursor, len(options))
	switch event.Kind {
	case components.EventBack:
		m.screen = screenProviderForm
		return m, nil
	case components.EventSelect:
		if len(options) > 0 {
			m.editingProvider.BaseURL = options[m.providerBaseURLCursor].URL
		}
		m.screen = screenProviderForm
		return m, nil
	}
	return m, nil
}

func (m *model) updateProviderModelList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.providerModelsLoading {
			state := components.ListState{NoWrap: true}
			event := state.Update(msg.String(), nil, components.RoleBack)
			if event.Kind == components.EventBack {
				m.providerModelsLoading = false
				m.providerModelLoadSeq++
				m.screen = screenProviderForm
				return m, nil
			}
			return m, nil
		}
		rows := m.providerModelRows()
		rowCursor := m.currentProviderModelRowIndex(rows)
		if rowCursor < 0 {
			rowCursor = 0
		}
		nextRowCursor, event := providerModelRowSelection(msg.String(), rowCursor, rows, components.RoleBack)
		if len(rows) > 0 {
			m.providerModelCursor = rows[nextRowCursor].EntryIndex
		}
		switch event.Kind {
		case components.EventBack:
			m.screen = screenProviderForm
			return m, nil
		case components.EventSelect:
			if len(rows) == 0 {
				return m, nil
			}
			m.editingProvider.Model = m.providerModels[m.providerModelCursor]
			m.screen = screenProviderForm
			return m, nil
		}
		if nextRowCursor != rowCursor {
			return m, nil
		}
	}

	cmd := m.modelFilter.Update(msg)
	m.clampProviderModelCursor(m.providerModelRows())
	return m, cmd
}

func (m *model) updateProviderEffortList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	efforts := m.providerReasoningEfforts()
	event := m.updateListSelection(keyMsg.String(), &m.providerEffortCursor, len(efforts))
	switch event.Kind {
	case components.EventBack:
		m.screen = screenProviderForm
		return m, nil
	case components.EventSelect:
		if len(efforts) > 0 {
			m.editingProvider.ReasoningEffort = efforts[m.providerEffortCursor]
		}
		m.screen = screenProviderForm
		return m, nil
	}
	return m, nil
}

func (m *model) updateProviderToolUseList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	modes := toolUseModes
	event := m.updateListSelection(keyMsg.String(), &m.providerToolUseCursor, len(modes))
	switch event.Kind {
	case components.EventBack:
		m.screen = screenProviderForm
		return m, nil
	case components.EventSelect:
		if len(modes) > 0 {
			m.editingProvider.ToolUseMode = modes[m.providerToolUseCursor]
		}
		m.screen = screenProviderForm
		return m, nil
	}
	return m, nil
}

func (m *model) newCustomProvider(providerType string) (setup.ProviderConfig, error) {
	base, name := "custom-openai-compatible", "Custom OpenAI-Compatible"
	if providerType == providers.TypeAnthropic {
		base, name = "custom-anthropic-compatible", "Custom Anthropic-Compatible"
	}
	id := base
	for i := 2; ; i++ {
		if _, taken := m.cfg.Provider(id); !taken {
			break
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return setup.NewProviderConfig(id, providerType, name)
}
