package commandui

type ListState struct {
	Cursor int
}

func (s *ListState) Update(key string, items []Item, closeRole Role) Event {
	if event := shortcutEvent(key, items); !event.IsZero() {
		return event
	}
	switch key {
	case "esc", "q":
		return eventForRole(closeRole)
	case "up", "k":
		s.move(-1, items)
	case "down", "j":
		s.move(1, items)
	case "enter":
		if s.Cursor >= 0 && s.Cursor < len(items) && items[s.Cursor].Selectable() {
			return eventForItem(items[s.Cursor])
		}
	}
	return Event{}
}

func (s *ListState) Clamp(count int) {
	if count <= 0 {
		s.Cursor = 0
		return
	}
	if s.Cursor < 0 {
		s.Cursor = 0
	}
	if s.Cursor >= count {
		s.Cursor = count - 1
	}
}

func shortcutEvent(key string, items []Item) Event {
	for _, item := range items {
		if !item.Selectable() || item.Shortcut == "" {
			continue
		}
		if key == item.Shortcut {
			return eventForItem(item)
		}
	}
	return Event{}
}

func (s *ListState) move(delta int, items []Item) {
	if len(items) == 0 {
		s.Cursor = 0
		return
	}
	if !hasSelectable(items) {
		s.Cursor = 0
		return
	}
	next := s.Cursor
	for range items {
		next += delta
		if next < 0 {
			next = len(items) - 1
		}
		if next >= len(items) {
			next = 0
		}
		if items[next].Selectable() {
			s.Cursor = next
			return
		}
	}
}

func hasSelectable(items []Item) bool {
	for _, item := range items {
		if item.Selectable() {
			return true
		}
	}
	return false
}
