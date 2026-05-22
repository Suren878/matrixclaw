package dialog

import (
	"strings"

	"github.com/sahilm/fuzzy"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
)

func (p *Picker) setItems() {
	p.options = make([]pickerOption, 0, len(p.data.Entries))
	for _, entry := range p.data.Entries {
		p.options = append(p.options, pickerOption{
			item:     pickerEntryItem(entry),
			action:   entry.Action,
			search:   strings.TrimSpace(entry.Search),
			selected: entry.Selected,
			footer:   entry.Footer,
		})
	}
	p.input = p.input.Reset("")
	p.applyFilter()
}

func pickerEntryItem(entry PickerEntry) commandui.Item {
	switch entry.Kind {
	case ListEntryHeader:
		return commandui.Header(entry.Title)
	case ListEntryDivider:
		return commandui.Divider(entry.ID)
	default:
		return commandui.Item{
			ID:       entry.ID,
			Title:    entry.Title,
			Status:   entry.Status,
			Shortcut: entry.Shortcut,
			Role:     entry.Role,
			Tone:     entry.Tone,
			Disabled: entry.Disabled,
		}
	}
}

func PickerNeedsFilter(entries []PickerEntry) bool {
	selectable := 0
	for _, entry := range entries {
		if entry.Kind == ListEntryRow && !entry.Footer {
			selectable++
		}
	}
	return selectable > 8
}

func (p *Picker) applyFilter() {
	query := strings.TrimSpace(p.input.Value())
	if query == "" {
		p.visible = append(p.visible[:0], p.options...)
		p.selectPreferredSelectable()
		return
	}
	p.visible = p.visible[:0]
	mainOptions, footerOptions := splitFooterOptions(p.options)
	matches := fuzzy.FindFrom(query, pickerOptionSource(mainOptions))
	for _, match := range matches {
		option := mainOptions[match.Index]
		if option.item.Selectable() {
			p.visible = append(p.visible, option)
		}
	}
	p.visible = append(p.visible, footerOptions...)
	p.selectPreferredSelectable()
}

type pickerOptionSource []pickerOption

func (s pickerOptionSource) Len() int {
	return len(s)
}

func (s pickerOptionSource) String(index int) string {
	option := s[index]
	if option.search != "" {
		return option.search
	}
	return strings.TrimSpace(option.item.Title + " " + option.item.Status)
}
