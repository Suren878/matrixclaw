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

func viewportBounds(selected int, total int, visible int) (int, int) {
	if total <= visible {
		return 0, total
	}
	if visible <= 0 {
		return 0, total
	}
	start := selected - visible/2
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > total {
		end = total
		start = end - visible
		if start < 0 {
			start = 0
		}
	}
	return start, end
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
