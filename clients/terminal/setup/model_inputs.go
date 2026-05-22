package setup

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	terminaltextfield "github.com/Suren878/matrixclaw/clients/terminal/ui/textfield"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) renderTextEditor() string {
	if m.textEditorTarget == textEditAssistantCustomPrompt {
		m.syncTextAreaSize()
		card := commandui.RenderTextViewCard(m.commandFrame(), commandui.TextViewData{
			Title:          m.textEditorTitle,
			Text:           m.textAreaInput.View(),
			Buttons:        setupFormButtons(),
			Button:         m.textEditState.Button,
			ButtonsFocused: m.textEditState.ButtonsFocused,
		})
		return m.renderCommandCard(card)
	}
	card := commandui.RenderPromptCard(m.commandFrame(), commandui.PromptData{
		Title:       m.textEditorTitle,
		Value:       m.textEditorInput.View(),
		Placeholder: m.textEditorInput.Placeholder(),
		Error:       m.formError,
	})
	return m.renderCommandCard(card)
}

func (m *model) updateTextEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.textEditorTarget == textEditAssistantCustomPrompt {
		return m.updateLargeTextEditor(msg)
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if ok {
		switch keyMsg.String() {
		case "esc":
			m.screen = m.textEditorReturnScreen()
			m.formError = ""
			return m, nil
		case "ctrl+s", "enter":
			m.commitTextEditor()
			return m, nil
		}
	}
	var cmd tea.Cmd
	cmd = m.textEditorInput.Update(msg)
	return m, cmd
}

func (m *model) commitTextEditor() {
	if err := m.applyTextEditorValue(); err != nil {
		m.formError = err.Error()
		return
	}
	if moved, err := m.afterTextEditorApply(context.Background()); err != nil {
		m.formError = err.Error()
		return
	} else if moved {
		return
	}
	m.formError = ""
	m.screen = m.textEditorReturnScreen()
}

func (m *model) openTextEditor(target textEditTarget, title string, placeholder string, value string, secret bool) {
	m.textEditorTarget = target
	m.textEditorTitle = title
	m.formError = ""
	m.textEditState = commandui.TextEditState{}
	if target == textEditAssistantCustomPrompt {
		m.textAreaInput = newTextArea(placeholder, value)
		m.syncTextAreaSize()
		m.screen = screenTextEditor
		return
	}
	m.textEditorInput = newTextField(placeholder, value, secret)
	m.screen = screenTextEditor
}

func (m *model) applyTextEditorValue() error {
	value := strings.TrimSpace(m.textEditorInput.Value())
	if m.textEditorTarget == textEditAssistantCustomPrompt {
		value = strings.TrimSpace(m.textAreaInput.Value())
	}
	switch m.textEditorTarget {
	case textEditDaemonHTTPAddr:
		m.draft.HTTPAddr = value
	case textEditDaemonDBPath:
		m.draft.DBPath = value
	case textEditDaemonTimezone:
		m.draft.Timezone = value
	case textEditProviderName:
		m.editingProvider.Name = value
	case textEditProviderAPIKey:
		if value != "" {
			m.editingProvider.APIKey = value
			if m.providerRequiresKeyCheck() {
				m.editingProvider.HasStoredAPIKey = false
				m.editingProvider.StoredAPIKeyPreview = ""
			} else {
				m.editingProvider.HasStoredAPIKey = true
				m.editingProvider.StoredAPIKeyPreview = setup.MaskSecret(value)
			}
		}
	case textEditProviderModel:
		m.editingProvider.Model = value
	case textEditProviderBaseURL:
		m.editingProvider.BaseURL = value
	case textEditAssistantName:
		m.draft.AssistantName = value
	case textEditAssistantCustomPrompt:
		m.draft.AssistantCustomPrompt = value
	case textEditTelegramBotToken:
		m.draft.TelegramBotToken = value
	case textEditTelegramAllowedUID:
		m.draft.TelegramAllowedUID = value
	}
	return nil
}

func (m *model) afterTextEditorApply(ctx context.Context) (bool, error) {
	if m.textEditorTarget != textEditProviderAPIKey {
		return false, nil
	}
	if strings.TrimSpace(m.editingProvider.APIKey) == "" {
		return false, errors.New("API key is required")
	}
	if !m.providerRequiresKeyCheck() {
		return false, nil
	}
	response, err := m.loadProviderModels(ctx)
	if err != nil {
		m.editingProvider.HasStoredAPIKey = true
		m.editingProvider.StoredAPIKeyPreview = setup.MaskSecret(m.editingProvider.APIKey)
		m.formError = "Could not load remote models: " + err.Error()
		m.screen = screenProviderForm
		return true, nil
	}
	if response.Status != setup.ProviderModelStatusOK {
		m.editingProvider.HasStoredAPIKey = true
		m.editingProvider.StoredAPIKeyPreview = setup.MaskSecret(m.editingProvider.APIKey)
		if setup.ProviderModelCatalogAllowsManualInput(response) {
			m.openProviderModelTextEditor(setup.ProviderModelCatalogManualMessage(response))
		} else {
			m.formError = setup.ProviderModelCatalogMessage(response)
			m.screen = screenProviderForm
		}
		return true, nil
	}
	m.editingProvider.HasStoredAPIKey = true
	m.editingProvider.StoredAPIKeyPreview = setup.MaskSecret(m.editingProvider.APIKey)
	m.formError = ""
	m.screen = screenProviderModelList
	return true, nil
}

func (m *model) textEditorReturnScreen() screen {
	switch m.textEditorTarget {
	case textEditDaemonHTTPAddr, textEditDaemonDBPath, textEditDaemonTimezone:
		return screenDaemonForm
	case textEditProviderName, textEditProviderAPIKey, textEditProviderModel, textEditProviderBaseURL:
		return screenProviderForm
	case textEditAssistantName, textEditAssistantCustomPrompt:
		return screenAssistantForm
	case textEditTelegramBotToken, textEditTelegramAllowedUID:
		return screenTelegramForm
	default:
		return screenProviderList
	}
}

func newTextField(placeholder string, value string, secret bool) terminaltextfield.Model {
	return terminaltextfield.New(placeholder, value,
		terminaltextfield.WithCharLimit(4096),
		terminaltextfield.WithWidth(64),
		terminaltextfield.WithSecret(secret),
	)
}

func newSearchField(placeholder string) terminaltextfield.Model {
	return terminaltextfield.New(placeholder, "",
		terminaltextfield.WithCharLimit(128),
		terminaltextfield.WithWidth(64),
	)
}

func newTextArea(placeholder string, value string) textarea.Model {
	input := textarea.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.ShowLineNumbers = false
	input.SetValue(value)
	input.CharLimit = 4096
	styleTextArea(&input)
	input.Focus()
	return input
}

func (m *model) updateLargeTextEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if ok {
		event := m.textEditState.Update(keyMsg.String(), setupFormButtons(), commandui.RoleCancel)
		switch event.Kind {
		case commandui.EventCancel, commandui.EventBack:
			m.screen = m.textEditorReturnScreen()
			m.formError = ""
			return m, nil
		case commandui.EventSubmit:
			m.commitTextEditor()
			return m, nil
		}
		if m.textEditState.ButtonsFocused {
			return m, nil
		}
	}
	m.syncTextAreaSize()
	var cmd tea.Cmd
	m.textAreaInput, cmd = m.textAreaInput.Update(msg)
	return m, cmd
}

func (m *model) syncTextAreaSize() {
	frame := m.commandFrame()
	m.textAreaInput.SetWidth(max(1, frame.InnerWidth()))
	m.textAreaInput.SetHeight(commandui.TextViewEditorHeight(frame))
}

func (m *model) resetFilter(placeholder string) {
	m.filterInput = newSearchField(placeholder)
}
