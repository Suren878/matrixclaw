package setup

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) renderTelegramForm() string {
	items := []listItem{
		{Title: "Enabled", Status: yesNo(m.cfg.Clients.Telegram.Enabled)},
		{Title: "Bot token", Status: maskOrBlank(m.cfg.Clients.Telegram.BotToken)},
		{Title: "Allowed user id", Status: m.cfg.Clients.Telegram.AllowedUserID},
		{Title: "Provider setup", Status: yesNo(m.cfg.Clients.Telegram.AllowProviderSetup)},
	}
	return m.renderEditableForm("Telegram", items, "Risk option: allows provider API key setup from Telegram.")
}

func (m *model) renderBoolPicker() string {
	items := []listItem{{Title: "Yes"}, {Title: "No"}}
	return m.renderPickerFrame(m.boolPickerTitle(), items, m.boolPickerCursor)
}

func (m *model) updateTelegramForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateForm(msg, 4, func() { m.cancelForm(screenChannelsList) }, m.handleTelegramFormSave, func() tea.Cmd {
		switch m.formFocus {
		case 0:
			m.openBoolPicker(boolEditTelegramEnabled, m.cfg.Clients.Telegram.Enabled)
		case 1:
			m.openTextEditor(textEditTelegramBotToken, "Bot Token", "Telegram bot token", m.cfg.Clients.Telegram.BotToken, true)
		case 2:
			m.openTextEditor(textEditTelegramAllowedUID, "Allowed User ID", "Allowed user id", m.cfg.Clients.Telegram.AllowedUserID, false)
		case 3:
			m.openBoolPicker(boolEditTelegramProviderSetup, m.cfg.Clients.Telegram.AllowProviderSetup)
		}
		return nil
	})
}

func (m *model) updateBoolPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	event := m.updateListSelection(keyMsg.String(), &m.boolPickerCursor, 2)
	switch event.Kind {
	case components.EventBack:
		m.screen = m.boolPickerReturnScreen()
	case components.EventSelect:
		value := m.boolPickerCursor == 0
		switch m.boolPickerTarget {
		case boolEditDaemonAutostart:
			m.cfg.Daemon.AutostartOnBoot = value
		case boolEditTelegramEnabled:
			m.cfg.Clients.Telegram.Enabled = value
		case boolEditTelegramProviderSetup:
			m.cfg.Clients.Telegram.AllowProviderSetup = value
		}
		m.screen = m.boolPickerReturnScreen()
	}
	return m, nil
}

func (m *model) handleTelegramFormSave() error {
	m.cfg.Clients.Telegram.BotToken = strings.TrimSpace(m.cfg.Clients.Telegram.BotToken)
	m.cfg.Clients.Telegram.AllowedUserID = strings.TrimSpace(m.cfg.Clients.Telegram.AllowedUserID)
	if m.cfg.Clients.Telegram.Enabled {
		if m.cfg.Clients.Telegram.BotToken == "" {
			return fmt.Errorf("telegram bot token is required when Telegram is enabled")
		}
		if m.cfg.Clients.Telegram.AllowedUserID == "" {
			return fmt.Errorf("telegram allowed user id is required when Telegram is enabled")
		}
	}
	return m.commitFormAndReturn(screenChannelsList)
}

func (m *model) openBoolPicker(target boolEditTarget, current bool) {
	m.boolPickerTarget = target
	m.boolPickerCursor = 1
	if current {
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

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
