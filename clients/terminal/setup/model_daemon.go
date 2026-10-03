package setup

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) renderDaemonForm() string {
	items := []listItem{
		{Title: "HTTP address", Status: m.cfg.Daemon.HTTPAddr},
		{Title: "SQLite path", Status: m.cfg.Daemon.DBPath},
		{Title: "Timezone", Status: m.cfg.Daemon.Timezone},
		{Title: "Autostart on boot", Status: yesNo(m.cfg.Daemon.AutostartOnBoot)},
	}
	return m.renderEditableForm("Daemon", items)
}

func (m *model) renderDaemonTimezoneList() string {
	options := setup.TimezoneOptions(time.Now())
	items := make([]listItem, 0, len(options)+1)
	for _, option := range options {
		status := option.ID
		if option.ID == strings.TrimSpace(m.cfg.Daemon.Timezone) {
			status = option.ID + " · selected"
		}
		items = append(items, listItem{Title: option.Label, Status: status})
	}
	items = append(items, listItem{Title: "Custom...", Status: "IANA timezone"})
	return m.renderPickerFrame("Timezone", items, m.timezoneCursor)
}

func (m *model) updateDaemonForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateForm(msg, 4, func() { m.cancelForm(screenDaemonList) }, m.handleDaemonFormSave, func() tea.Cmd {
		switch m.formFocus {
		case 0:
			m.openTextEditor(textEditDaemonHTTPAddr, "HTTP Address", "127.0.0.1:8080", m.cfg.Daemon.HTTPAddr, false)
		case 1:
			m.openTextEditor(textEditDaemonDBPath, "SQLite Path", "/path/to/matrixclaw.db", m.cfg.Daemon.DBPath, false)
		case 2:
			m.openTimezonePicker()
		case 3:
			m.openBoolPicker(boolEditDaemonAutostart, m.cfg.Daemon.AutostartOnBoot)
		}
		return nil
	})
}

func (m *model) updateDaemonTimezoneList(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	options := setup.TimezoneOptions(time.Now())
	event := m.updateListSelection(keyMsg.String(), &m.timezoneCursor, len(options)+1)
	switch event.Kind {
	case components.EventBack:
		m.screen = screenDaemonForm
		return m, nil
	case components.EventSelect:
		if m.timezoneCursor >= 0 && m.timezoneCursor < len(options) {
			m.cfg.Daemon.Timezone = options[m.timezoneCursor].ID
			m.screen = screenDaemonForm
			return m, nil
		}
		m.openTextEditor(textEditDaemonTimezone, "Custom Timezone", "Europe/Berlin", m.cfg.Daemon.Timezone, false)
	}
	return m, nil
}

func (m *model) openTimezonePicker() {
	options := setup.TimezoneOptions(time.Now())
	m.timezoneCursor = len(options)
	for i, option := range options {
		if option.ID == strings.TrimSpace(m.cfg.Daemon.Timezone) {
			m.timezoneCursor = i
			break
		}
	}
	m.formError = ""
	m.screen = screenDaemonTimezoneList
}

func (m *model) handleDaemonFormSave() error {
	m.cfg.Daemon.HTTPAddr = strings.TrimSpace(m.cfg.Daemon.HTTPAddr)
	m.cfg.Daemon.DBPath = strings.TrimSpace(m.cfg.Daemon.DBPath)
	m.cfg.Daemon.Timezone = strings.TrimSpace(m.cfg.Daemon.Timezone)
	if m.cfg.Daemon.HTTPAddr == "" {
		return fmt.Errorf("daemon HTTP address is required")
	}
	if m.cfg.Daemon.DBPath == "" {
		return fmt.Errorf("daemon DB path is required")
	}
	if m.cfg.Daemon.Timezone == "" {
		return fmt.Errorf("daemon timezone is required")
	}
	return m.commitFormAndReturn(screenProviderList)
}

func daemonListStatus(summary setup.DaemonSummary) string {
	if summary.Status == "Configured" && summary.Autostart {
		return "Configured · Autostart"
	}
	return summary.Status
}
