package dialog

import (
	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
)

func (p *Picker) visibleWindow(limit int) ([]commandui.Item, int, []commandui.Item, int) {
	mainOptions, footerOptions := splitFooterOptions(p.visible)
	mainCursor, footerCursor := p.splitCursor(mainOptions, footerOptions)
	if limit <= 0 || len(mainOptions) <= limit {
		return pickerItems(mainOptions), mainCursor, pickerItems(footerOptions), footerCursor
	}
	cursor := mainCursor
	if cursor < 0 || cursor >= len(mainOptions) {
		cursor = 0
	}
	start, end := viewportBounds(cursor, len(mainOptions), limit)
	selected := -1
	if mainCursor >= start && mainCursor < end {
		selected = mainCursor - start
	}
	return pickerItems(mainOptions[start:end]), selected, pickerItems(footerOptions), footerCursor
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

func pickerItems(options []pickerOption) []commandui.Item {
	items := make([]commandui.Item, 0, len(options))
	for _, option := range options {
		items = append(items, option.item)
	}
	return items
}

func splitFooterOptions(options []pickerOption) ([]pickerOption, []pickerOption) {
	mainOptions := make([]pickerOption, 0, len(options))
	footerOptions := make([]pickerOption, 0, 2)
	for _, option := range options {
		if option.footer {
			footerOptions = append(footerOptions, option)
			continue
		}
		mainOptions = append(mainOptions, option)
	}
	return mainOptions, footerOptions
}

func (p *Picker) splitCursor(mainOptions []pickerOption, footerOptions []pickerOption) (int, int) {
	mainCursor := -1
	footerCursor := -1
	for index, option := range p.visible {
		if index != p.cursor {
			continue
		}
		if option.footer {
			for footerIndex, footer := range footerOptions {
				if footer.item.ID == option.item.ID {
					footerCursor = footerIndex
					break
				}
			}
			break
		}
		for mainIndex, main := range mainOptions {
			if main.item.ID == option.item.ID {
				mainCursor = mainIndex
				break
			}
		}
		break
	}
	return mainCursor, footerCursor
}
