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
		{Title: "HTTP address", Status: m.draft.HTTPAddr},
		{Title: "SQLite path", Status: m.draft.DBPath},
		{Title: "Timezone", Status: m.draft.Timezone},
		{Title: "Autostart on boot", Status: nonEmpty(m.draft.AutostartOnBoot, "no")},
	}
	return m.renderEditableForm("Daemon", items)
}

func (m *model) renderDaemonTimezoneList() string {
	options := setup.TimezoneOptions(time.Now())
	items := make([]listItem, 0, len(options)+1)
	for _, option := range options {
		status := option.ID
		if option.ID == strings.TrimSpace(m.draft.Timezone) {
			status = option.ID + " · selected"
		}
		items = append(items, listItem{Title: option.Label, Status: status})
	}
	items = append(items, listItem{Title: "Custom...", Status: "IANA timezone"})
	return m.renderPickerFrame("Timezone", items, m.timezoneCursor)
}

func (m *model) updateDaemonForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateForm(msg, 4, func() { m.cancelDraftForm(screenDaemonList) }, m.handleDaemonFormSave, func() tea.Cmd {
		switch m.formFocus {
		case 0:
			m.openTextEditor(textEditDaemonHTTPAddr, "HTTP Address", "127.0.0.1:8080", m.draft.HTTPAddr, false)
		case 1:
			m.openTextEditor(textEditDaemonDBPath, "SQLite Path", "/path/to/matrixclaw.db", m.draft.DBPath, false)
		case 2:
			m.openTimezonePicker()
		case 3:
			m.openBoolPicker(boolEditDaemonAutostart, m.draft.AutostartOnBoot)
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
	event := m.updateListSelection(keyMsg.String(), &m.timezoneCursor, len(options)+1, components.RoleBack)
	switch event.Kind {
	case components.EventBack:
		m.screen = screenDaemonForm
		return m, nil
	case components.EventSelect:
		if m.timezoneCursor >= 0 && m.timezoneCursor < len(options) {
			m.draft.Timezone = options[m.timezoneCursor].ID
			m.screen = screenDaemonForm
			return m, nil
		}
		m.openTextEditor(textEditDaemonTimezone, "Custom Timezone", "Europe/Berlin", m.draft.Timezone, false)
	}
	return m, nil
}

func (m *model) openTimezonePicker() {
	options := setup.TimezoneOptions(time.Now())
	m.timezoneCursor = len(options)
	for i, option := range options {
		if option.ID == strings.TrimSpace(m.draft.Timezone) {
			m.timezoneCursor = i
			break
		}
	}
	m.formError = ""
	m.screen = screenDaemonTimezoneList
}

func (m *model) handleDaemonFormSave() error {
	m.draft.HTTPAddr = strings.TrimSpace(m.draft.HTTPAddr)
	m.draft.DBPath = strings.TrimSpace(m.draft.DBPath)
	m.draft.Timezone = strings.TrimSpace(m.draft.Timezone)
	m.draft.AutostartOnBoot = strings.TrimSpace(m.draft.AutostartOnBoot)
	if m.draft.HTTPAddr == "" {
		return fmt.Errorf("daemon HTTP address is required")
	}
	if m.draft.DBPath == "" {
		return fmt.Errorf("daemon DB path is required")
	}
	if m.draft.Timezone == "" {
		return fmt.Errorf("daemon timezone is required")
	}
	return m.saveDraftAndReturn(screenProviderList)
}

func daemonListStatus(summary setup.DaemonSummary) string {
	if summary.Status == "Configured" && summary.Autostart {
		return "Configured · Autostart"
	}
	return summary.Status
}
