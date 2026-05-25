package setup

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) renderSummary() string {
	summary := setup.SummaryFromDraft(m.draft)
	items := []components.Item{
		summaryItem("Active provider", fmt.Sprintf("%s (%s)", nonEmpty(summary.Provider.Name, "Not configured"), nonEmpty(summary.Provider.Model, "no model"))),
		summaryItem(providerAuthSummaryLabel(summary.Provider.ID), nonEmpty(summary.Provider.APIKeyPreview, "Not configured")),
		summaryItem("Configured providers", fmt.Sprintf("%d", len(setup.ConfiguredProviders(m.draft)))),
		summaryItem("Assistant", fmt.Sprintf("%s · %s", nonEmpty(summary.Assistant.Name, "matrixclaw"), summary.Assistant.Status)),
		summaryItem("Daemon HTTP", m.draft.HTTPAddr),
		summaryItem("SQLite", m.draft.DBPath),
		summaryItem("Timezone", m.draft.Timezone),
		summaryItem("Autostart", nonEmpty(m.draft.AutostartOnBoot, "no")),
		summaryItem("Telegram", summary.Telegram.Status),
	}
	if m.formError != "" {
		items = append(items, components.Divider(""), components.Item{Title: m.formError, Disabled: true})
	}
	return m.renderInfoScreen("Review", "Step 5/5", items, "enter save · esc back")
}

func (m *model) renderSuccess() string {
	summary := m.result.Summary
	items := []components.Item{
		components.Item{Title: "Setup complete.", Disabled: true},
		components.Divider(""),
		summaryItem("Config path", m.result.Path),
		summaryItem("Active provider", fmt.Sprintf("%s (%s)", nonEmpty(summary.Provider.Name, "Not configured"), nonEmpty(summary.Provider.Model, "no model"))),
		summaryItem("Assistant", fmt.Sprintf("%s · %s", nonEmpty(summary.Assistant.Name, "matrixclaw"), summary.Assistant.Status)),
		summaryItem(providerAuthSummaryLabel(summary.Provider.ID), nonEmpty(summary.Provider.APIKeyPreview, "Not configured")),
		summaryItem("Configured providers", fmt.Sprintf("%d", len(m.result.Config.Providers))),
		summaryItem("Daemon", fmt.Sprintf("%s · %s", summary.Daemon.Status, nonEmpty(summary.Daemon.RuntimeStatus, "Unknown"))),
		summaryItem("Telegram", summary.Telegram.Status),
	}
	items = appendOptionalSummaryItem(items, "Telegram bot", func() string {
		if summary.Telegram.Username == "" {
			return ""
		}
		return "@" + summary.Telegram.Username
	}())
	items = appendOptionalSummaryItem(items, "Warning", firstNonEmpty(summary.Daemon.Warning, summary.Telegram.Warning))
	return m.renderInfoScreen("Setup Saved", "", items, "enter quit · q quit")
}

func (m *model) renderInfoScreen(title string, meta string, items []components.Item, help string) string {
	card := components.RenderInfoCard(m.commandFrame(), components.InfoData{
		Title:    title,
		Meta:     meta,
		Items:    items,
		Selected: -1,
		Help:     help,
	})
	return m.renderCommandCard(card)
}

func summaryItem(label string, value string) components.Item {
	return components.Item{Title: label, Status: value, Disabled: true}
}

func providerAuthSummaryLabel(providerID string) string {
	if strings.TrimSpace(providerID) == "openai-codex" {
		return "Auth"
	}
	return "API key"
}

func appendOptionalSummaryItem(items []components.Item, label string, value string) []components.Item {
	if strings.TrimSpace(value) == "" {
		return items
	}
	return append(items, summaryItem(label, value))
}

func (m *model) updateSummary(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc":
		m.screen = screenChannelsList
		m.cursor = 0
	case "enter", "ctrl+s":
		result, err := m.service.Apply(m.draft)
		if err != nil {
			m.formError = err.Error()
			return m, nil
		}
		m.result = result
		m.formError = ""
		m.screen = screenSuccess
	}
	return m, nil
}

func (m *model) updateSuccess(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "enter", "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}
