package components

import "strings"

type Role string

const (
	RoleNormal Role = ""
	RoleBack   Role = "back"
	RoleCancel Role = "cancel"
	RoleExit   Role = "exit"
	RoleSubmit Role = "submit"
)

type ItemKind string

const (
	ItemRow     ItemKind = ""
	ItemHeader  ItemKind = "header"
	ItemDivider ItemKind = "divider"
)

type EventKind string

const (
	EventNone   EventKind = ""
	EventSelect EventKind = "select"
	EventBack   EventKind = "back"
	EventCancel EventKind = "cancel"
	EventExit   EventKind = "exit"
	EventSubmit EventKind = "submit"
	EventEdit   EventKind = "edit"
)

type Event struct {
	Kind  EventKind
	ID    string
	Value string
}

func (e Event) IsZero() bool {
	return e.Kind == EventNone
}

type Item struct {
	Kind     ItemKind
	ID       string
	Title    string
	Status   string
	Shortcut string
	Role     Role
	Disabled bool
	Tone     RowTone
}

func (item Item) row() row {
	return row{
		Title:    item.Title,
		Status:   strings.TrimSpace(item.Status),
		Disabled: item.Disabled,
		Tone:     item.Tone,
	}
}

func Header(title string) Item {
	return Item{Kind: ItemHeader, Title: title, Disabled: true}
}

func Divider(id string) Item {
	return Item{Kind: ItemDivider, ID: id, Disabled: true}
}

func (item Item) Selectable() bool {
	return item.Kind == ItemRow && !item.Disabled
}

func itemRows(items []Item) []row {
	rows := make([]row, 0, len(items))
	for _, item := range items {
		rows = append(rows, item.row())
	}
	return rows
}

func eventForItem(item Item) Event {
	if !item.Selectable() {
		return Event{}
	}
	switch item.Role {
	case RoleBack:
		return Event{Kind: EventBack, ID: item.ID}
	case RoleCancel:
		return Event{Kind: EventCancel, ID: item.ID}
	case RoleExit:
		return Event{Kind: EventExit, ID: item.ID}
	case RoleSubmit:
		return Event{Kind: EventSubmit, ID: item.ID}
	default:
		return Event{Kind: EventSelect, ID: item.ID}
	}
}

func helpWithShortcuts(help string, items []Item) string {
	parts := make([]string, 0, 1+len(items))
	help = strings.TrimSpace(help)
	if help != "" {
		parts = append(parts, help)
	}
	seen := map[string]bool{}
	for _, item := range items {
		shortcut := strings.TrimSpace(item.Shortcut)
		if !item.Selectable() || shortcut == "" || seen[shortcut] {
			continue
		}
		seen[shortcut] = true
		parts = append(parts, shortcutHelp(item, shortcut))
	}
	return strings.Join(parts, " · ")
}

func shortcutHelp(item Item, shortcut string) string {
	label := strings.ToLower(strings.Join(strings.Fields(item.Title), " "))
	if label == "" {
		label = strings.ToLower(strings.TrimSpace(item.ID))
	}
	if label == "" {
		return shortcut
	}
	return shortcut + " " + label
}

func eventForRole(role Role) Event {
	switch role {
	case RoleBack:
		return Event{Kind: EventBack, ID: string(role)}
	case RoleCancel:
		return Event{Kind: EventCancel, ID: string(role)}
	case RoleExit:
		return Event{Kind: EventExit, ID: string(role)}
	case RoleSubmit:
		return Event{Kind: EventSubmit, ID: string(role)}
	default:
		return Event{}
	}
}

func eventForButton(buttons []ButtonSpec, index int) Event {
	if index < 0 || index >= len(buttons) {
		return Event{}
	}
	return eventForRole(buttons[index].Role)
}
