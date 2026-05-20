package setup

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) renderTelegramForm() string {
	items := []listItem{
		{Title: "Enabled", Status: nonEmpty(m.draft.TelegramEnabled, "no")},
		{Title: "Bot token", Status: maskOrBlank(m.draft.TelegramBotToken)},
		{Title: "Allowed user id", Status: m.draft.TelegramAllowedUID},
		{Title: "Provider setup", Status: nonEmpty(m.draft.TelegramProviderSetup, "no")},
	}
	return m.renderEditableForm("Telegram", items, "Risk option: allows provider API key setup from Telegram.")
}

func (m *model) renderBoolPicker() string {
	items := []listItem{{Title: "Yes"}, {Title: "No"}}
	return m.renderPickerFrame(m.boolPickerTitle(), items, m.boolPickerCursor)
}

func (m *model) updateTelegramForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateForm(msg, 4, func() { m.cancelDraftForm(screenChannelsList) }, m.handleTelegramFormSave, func() tea.Cmd {
		switch m.formFocus {
		case 0:
			m.openBoolPicker(boolEditTelegramEnabled, m.draft.TelegramEnabled)
		case 1:
			m.openTextEditor(textEditTelegramBotToken, "Bot Token", "Telegram bot token", m.draft.TelegramBotToken, true)
		case 2:
			m.openTextEditor(textEditTelegramAllowedUID, "Allowed User ID", "Allowed user id", m.draft.TelegramAllowedUID, false)
		case 3:
			m.openBoolPicker(boolEditTelegramProviderSetup, m.draft.TelegramProviderSetup)
		}
		return nil
	})
}

func (m *model) updateBoolPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	event := m.updateListSelection(keyMsg.String(), &m.boolPickerCursor, 2, commandui.RoleBack)
	switch event.Kind {
	case commandui.EventBack:
		m.screen = m.boolPickerReturnScreen()
	case commandui.EventSelect:
		value := "no"
		if m.boolPickerCursor == 0 {
			value = "yes"
		}
		switch m.boolPickerTarget {
		case boolEditDaemonAutostart:
			m.draft.AutostartOnBoot = value
		case boolEditTelegramEnabled:
			m.draft.TelegramEnabled = value
		case boolEditTelegramProviderSetup:
			m.draft.TelegramProviderSetup = value
		}
		m.screen = m.boolPickerReturnScreen()
	}
	return m, nil
}

func (m *model) handleTelegramFormSave() error {
	m.draft.TelegramEnabled = strings.TrimSpace(m.draft.TelegramEnabled)
	m.draft.TelegramBotToken = strings.TrimSpace(m.draft.TelegramBotToken)
	m.draft.TelegramAllowedUID = strings.TrimSpace(m.draft.TelegramAllowedUID)
	m.draft.TelegramProviderSetup = strings.TrimSpace(m.draft.TelegramProviderSetup)
	if setup.ParseBool(m.draft.TelegramEnabled) {
		if m.draft.TelegramBotToken == "" {
			return fmt.Errorf("telegram bot token is required when Telegram is enabled")
		}
		if m.draft.TelegramAllowedUID == "" {
			return fmt.Errorf("telegram allowed user id is required when Telegram is enabled")
		}
	}
	return m.saveDraftAndReturn(screenChannelsList)
}

func (m *model) openBoolPicker(target boolEditTarget, current string) {
	m.boolPickerTarget = target
	m.boolPickerCursor = 1
	if setup.ParseBool(current) {
		m.boolPickerCursor = 0
	}
	m.formError = ""
	m.screen = screenBoolPicker
}

func (m *model) boolPickerReturnScreen() screen {
	switch m.boolPickerTarget {
	case boolEditDaemonAutostart:
		return screenDaemonForm
	case boolEditTelegramEnabled, boolEditTelegramProviderSetup:
		return screenTelegramForm
	default:
		return screenDaemonForm
	}
}

func (m *model) boolPickerTitle() string {
	switch m.boolPickerTarget {
	case boolEditDaemonAutostart:
		return "Autostart"
	case boolEditTelegramEnabled:
		return "Telegram Enabled"
	case boolEditTelegramProviderSetup:
		return "Telegram Provider Setup"
	default:
		return "Select"
	}
}

func maskOrBlank(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return setup.MaskSecret(value)
}
