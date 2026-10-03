package setup

type listEntryKind int

const (
	listEntryRow listEntryKind = iota
	listEntryHeader
	listEntryDivider
)

type listEntry struct {
	Kind       listEntryKind
	Text       string
	Status     string
	EntryIndex int
}

func rowEntry(text string, status string, entryIndex int) listEntry {
	return listEntry{
		Kind:       listEntryRow,
		Text:       text,
		Status:     status,
		EntryIndex: entryIndex,
	}
}

func selectedEntryRow(entries []listEntry, selectedIndex int) int {
	for i, entry := range entries {
		if entry.Kind == listEntryRow && entry.EntryIndex == selectedIndex {
			return i
		}
	}
	if len(entries) == 0 {
		return 0
	}
	return len(entries) - 1
}
