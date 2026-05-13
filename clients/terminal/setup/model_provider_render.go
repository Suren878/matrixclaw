package setup

import (
	"strings"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
)

func (m *model) renderProviderList() string {
	entries := m.providerEntries()
	rows := m.providerSearchRows(entries)
	selectedRow := 0
	if m.cursor > 0 {
		selectedRow = selectedEntryRow(rows, m.cursor)
	}
	start, end := viewportBounds(selectedRow, len(rows), m.providerViewportHeight())
	topSelected := 0
	selected := -1
	if m.cursor > 0 {
		topSelected = -1
		selected = selectedProviderItemIndex(rows[start:end], m.cursor, start > 0)
	}
	card := commandui.RenderSearchListCard(m.commandFrame(), commandui.SearchListData{
		Title:             "Providers",
		Meta:              "Step 2/5",
		SearchValue:       m.filterInput.Value(),
		SearchPlaceholder: "Search providers",
		SearchActive:      true,
		EmptyText:         "No providers match the current filter.",
		TopItems:          []commandui.Item{{Title: "Continue", Role: commandui.RoleSubmit}},
		TopSelected:       topSelected,
		Items:             pagedSearchItems(rows[start:end], start > 0, end < len(rows)),
		Selected:          selected,
		Help:              "ctrl+a add · enter select · ↑/↓ move · esc back",
		Error:             m.formError,
	})
	return m.renderCommandCard(card)
}

func (m *model) renderProviderTypeList() string {
	items := []listItem{
		{Title: "OpenAI-Compatible", Status: ""},
		{Title: "Anthropic-Compatible", Status: ""},
	}
	return m.renderPickerFrame("Add Provider", items, m.providerTypeCursor)
}

func (m *model) renderProviderNoProviderConfirm() string {
	card := commandui.RenderConfirmCard(m.commandFrame(), commandui.ConfirmData{
		Message:      "No provider is configured. You can finish setup now, but chat and runs will not work until a provider is added. Continue without a provider?",
		ConfirmLabel: "Yes",
		CancelLabel:  "No",
		Selected:     m.providerNoProviderCursor,
	})
	return m.renderCommandCard(card)
}

func (m *model) renderProviderForm() string {
	return m.renderEditableForm("Provider", providerFormRows(m.providerFormItems()), m.providerFormSubtitle())
}

func (m *model) renderProviderBaseURLList() string {
	return m.renderPickerFrame("Endpoint", m.providerBaseURLItems(), m.providerBaseURLCursor)
}

func (m *model) renderProviderModelList() string {
	rows := m.providerModelRows()
	selectedRow := m.currentProviderModelRowIndex(rows)
	if selectedRow < 0 {
		selectedRow = 0
	}
	start, end := viewportBounds(selectedRow, len(rows), m.providerModelViewportHeight())
	card := commandui.RenderSearchListCard(m.commandFrame(), commandui.SearchListData{
		Title:             "Models",
		SearchValue:       m.filterInput.Value(),
		SearchPlaceholder: "Search models",
		SearchActive:      true,
		EmptyText:         "No models match the current filter.",
		Items:             pagedSearchItems(rows[start:end], start > 0, end < len(rows)),
		Selected:          selectedProviderItemIndex(rows[start:end], m.providerModelCursor, start > 0),
		Help:              "enter select · ↑/↓ move · esc cancel",
	})
	return m.renderCommandCard(card)
}

func (m *model) renderProviderEffortList() string {
	efforts := m.providerReasoningEfforts()
	items := make([]listItem, 0, len(efforts))
	for _, effort := range efforts {
		items = append(items, listItem{Title: strings.Title(effort)})
	}
	return m.renderPickerFrame("Reasoning Effort", items, m.providerEffortCursor)
}

func (m *model) renderProviderToolUseList() string {
	items := []listItem{
		{Title: "Enabled"},
		{Title: "Disabled"},
	}
	return m.renderPickerFrame("Tool Use", items, m.providerToolUseCursor)
}

func pagedSearchItems(rows []listEntry, hasPrevious bool, hasNext bool) []commandui.Item {
	items := make([]commandui.Item, 0, len(rows)+2)
	if hasPrevious {
		items = append(items, commandui.Header("↑ more"))
	}
	items = append(items, searchItems(rows)...)
	if hasNext {
		items = append(items, commandui.Header("↓ more"))
	}
	return items
}

func selectedProviderItemIndex(rows []listEntry, cursor int, hasPrevious bool) int {
	offset := 0
	if hasPrevious {
		offset = 1
	}
	for i, row := range rows {
		if row.Kind == listEntryRow && row.EntryIndex == cursor {
			return i + offset
		}
	}
	return -1
}
