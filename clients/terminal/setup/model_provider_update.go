package setup

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) updateProviderList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.returnToList(screenDaemonList)
			return m, nil
		case "ctrl+a":
			m.providerTypeCursor = 0
			m.screen = screenProviderTypeList
			return m, nil
		case "up", "k", "down", "j":
			entries := m.providerEntries()
			m.moveIndex(msg.String(), &m.cursor, len(entries)-1)
			return m, nil
		case "enter":
			entries := m.providerEntries()
			if len(entries) == 0 {
				return m, nil
			}
			selected := entries[m.cursor]
			m.formError = ""
			switch selected.Kind {
			case providerEntryConfigured:
				m.editingProvider = selected.Provider
				m.openDraftForm(screenProviderForm)
				return m, nil
			case providerEntryAvailable:
				provider, err := m.service.BuiltInProviderDraft(m.draft, selected.Option.ID)
				if err != nil {
					m.formError = err.Error()
					return m, nil
				}
				m.editingProvider = provider
				m.openDraftForm(screenProviderForm)
				return m, nil
			case providerEntryContinue:
				if len(setup.ConfiguredProviders(m.draft)) == 0 {
					m.providerNoProviderCursor = 0
					m.screen = screenProviderNoProviderConfirm
					return m, nil
				}
				m.openDraftForm(screenAssistantForm)
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
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
	switch keyMsg.String() {
	case "esc", "n":
		m.screen = screenProviderList
		return m, nil
	case "up", "k", "down", "j":
		m.moveIndex(keyMsg.String(), &m.providerNoProviderCursor, 1)
	case "enter", "y":
		if m.providerNoProviderCursor == 0 || keyMsg.String() == "y" {
			m.openDraftForm(screenAssistantForm)
			return m, nil
		}
		m.screen = screenProviderList
		return m, nil
	}
	return m, nil
}

func (m *model) updateProviderTypeList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc":
		m.screen = screenProviderList
		return m, nil
	case "up", "k", "down", "j":
		m.moveIndex(keyMsg.String(), &m.providerTypeCursor, 1)
	case "enter":
		providerType := providers.TypeOpenAICompat
		if m.providerTypeCursor == 1 {
			providerType = providers.TypeAnthropic
		}
		provider, err := m.service.NewCustomProviderDraft(m.draft, providerType)
		if err != nil {
			m.formError = err.Error()
			return m, nil
		}
		m.editingProvider = provider
		m.openDraftForm(screenProviderForm)
		return m, nil
	}
	return m, nil
}

func (m *model) updateProviderForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	items := m.providerFormItems()
	return m.updateForm(msg, len(items), func() { m.returnToList(screenProviderList) }, m.handleProviderFormSave, func() {
		if m.formFocus < 0 || m.formFocus >= len(items) {
			return
		}
		item := items[m.formFocus]
		switch item.Target {
		case textEditProviderName:
			m.openTextEditor(textEditProviderName, "Provider Name", "Provider name", m.editingProvider.Name, false)
		case textEditProviderAPIKey:
			m.openTextEditor(textEditProviderAPIKey, "API Key", m.providerAPIKeyPlaceholder(), "", true)
		case textEditProviderModel:
			if !m.providerModelUsesPicker() {
				m.openTextEditor(textEditProviderModel, "Model", "model-id", m.editingProvider.Model, false)
			} else {
				if err := m.openProviderModelPicker(context.Background()); err != nil {
					m.openProviderModelTextEditor(modelDiscoveryErrorMessage(err))
				}
			}
		case textEditProviderBaseURL:
			if item.BaseURL {
				m.providerBaseURLCursor = m.providerBaseURLIndex()
				m.screen = screenProviderBaseURLList
			} else {
				m.openTextEditor(textEditProviderBaseURL, "Base URL", "https://api.example.com/v1", m.editingProvider.BaseURL, false)
			}
		case textEditNone:
			if item.Reasoning {
				m.providerEffortCursor = m.reasoningEffortIndex()
				m.screen = screenProviderEffortList
			} else if item.ToolUse {
				m.providerToolUseCursor = m.toolUseModeIndex()
				m.screen = screenProviderToolUseList
			}
		}
	})
}

func (m *model) updateProviderBaseURLList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	options := m.providerBaseURLOptions()
	switch keyMsg.String() {
	case "esc":
		m.screen = screenProviderForm
		return m, nil
	case "up", "k", "down", "j":
		m.moveIndex(keyMsg.String(), &m.providerBaseURLCursor, len(options)-1)
	case "enter":
		if len(options) > 0 {
			m.editingProvider.BaseURL = options[m.providerBaseURLCursor]
		}
		m.screen = screenProviderForm
		return m, nil
	}
	return m, nil
}

func (m *model) updateProviderModelList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		rows := m.providerModelRows()
		switch msg.String() {
		case "esc":
			m.screen = screenProviderForm
			return m, nil
		case "up", "k", "down", "j":
			m.moveProviderModelCursor(msg.String(), rows)
			return m, nil
		case "enter":
			if len(rows) == 0 {
				return m, nil
			}
			m.editingProvider.Model = m.providerModels[m.providerModelCursor]
			m.screen = screenProviderForm
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.clampProviderModelCursor(m.providerModelRows())
	return m, cmd
}

func (m *model) updateProviderEffortList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	efforts := m.providerReasoningEfforts()
	switch keyMsg.String() {
	case "esc":
		m.screen = screenProviderForm
		return m, nil
	case "up", "k", "down", "j":
		m.moveIndex(keyMsg.String(), &m.providerEffortCursor, len(efforts)-1)
	case "enter":
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
	modes := providerToolUseModes()
	switch keyMsg.String() {
	case "esc":
		m.screen = screenProviderForm
		return m, nil
	case "up", "k", "down", "j":
		m.moveIndex(keyMsg.String(), &m.providerToolUseCursor, len(modes)-1)
	case "enter":
		if len(modes) > 0 {
			m.editingProvider.ToolUseMode = modes[m.providerToolUseCursor]
		}
		m.screen = screenProviderForm
		return m, nil
	}
	return m, nil
}
