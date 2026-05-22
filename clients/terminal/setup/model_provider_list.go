package setup

import (
	"strings"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) providerEntries() []providerListEntry {
	query := m.providerSearchQuery()
	entries := make([]providerListEntry, 0, len(m.draft.Providers)+len(m.builtInProviders)+2)

	entries = append(entries, providerListEntry{
		Kind:   providerEntryContinue,
		Title:  "Continue",
		Status: "",
	})

	for _, item := range setup.ProviderSetupItemsFromDraft(m.draft, m.builtInProviders) {
		if !matchesProviderSearch(query,
			item.Name,
			item.ID,
			item.Model,
			item.DefaultModel,
			item.Status,
		) {
			continue
		}
		entry := providerListEntry{
			Kind:   providerEntryAvailable,
			Title:  setup.ProviderDisplayName(item),
			Status: setup.ProviderDisplayStatus(item),
		}
		if item.Configured {
			provider, _ := setup.FindProviderDraft(m.draft, item.ID)
			entry.Kind = providerEntryConfigured
			entry.Provider = provider
		} else {
			option, _ := lookupOption(m.builtInProviders, item.CatalogID)
			entry.Option = option
		}
		entries = append(entries, entry)
	}

	return entries
}

func (m *model) providerSearchRows(entries []providerListEntry) []listEntry {
	rows := make([]listEntry, 0, len(entries))
	for i, entry := range entries {
		if entry.Kind == providerEntryContinue {
			continue
		}
		rows = append(rows, rowEntry(entry.Title, entry.Status, i))
	}
	return rows
}

func (m *model) providerViewportHeight() int {
	if m.height <= 0 {
		return 10
	}
	height := m.height - 20
	if height < 6 {
		return 6
	}
	return height
}

func (m *model) providerModelViewportHeight() int {
	return m.providerViewportHeight()
}

func (m *model) providerSearchQuery() string {
	return strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
}

func matchesProviderSearch(query string, values ...string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.Join(values, " ")), query)
}

func searchItems(rows []listEntry) []commandui.Item {
	items := make([]commandui.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, commandui.Item{Title: row.Text, Status: row.Status})
	}
	return items
}

func lookupOption(options []setup.ProviderOption, providerID string) (setup.ProviderOption, bool) {
	providerID = providers.CanonicalProviderID(providerID)
	for _, option := range options {
		if providers.CanonicalProviderID(option.ID) == providerID {
			return option, true
		}
	}
	return setup.ProviderOption{}, false
}
