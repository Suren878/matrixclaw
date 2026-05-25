package components

type ListData struct {
	Title          string
	Meta           string
	Items          []Item
	Selected       int
	ExtraLines     []string
	Footer         []Item
	FooterSelected int
	Help           string
	Error          string
}

func RenderListCard(frame Frame, data ListData) string {
	help := helpWithShortcuts(firstNonEmpty(data.Help, "enter select · ↑/↓ move · esc back"), append(data.Items, data.Footer...))
	frame = frame.WithInnerWidth(0)
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Meta:  data.Meta,
		Body:  renderItemsWithFooter(frame, data.Items, data.Selected, data.ExtraLines, data.Footer, data.FooterSelected),
		Help:  help,
		Error: data.Error,
	})
}

func renderItemsWithFooter(frame Frame, items []Item, selected int, extraLines []string, footer []Item, footerSelected int) []string {
	body := renderItemLines(frame, items, selected)
	body = append(body, extraLines...)
	if len(footer) > 0 {
		body = append(body, "")
		body = append(body, renderItemLines(frame, footer, footerSelected)...)
	}
	return body
}

func renderItemLines(frame Frame, items []Item, selected int) []string {
	styles := frame.styles()
	width := frame.InnerWidth()
	body := make([]string, 0, len(items))
	for index, item := range items {
		switch item.Kind {
		case ItemHeader:
			body = append(body, renderTruncated(styles.Subtitle, item.Title, width))
		case ItemDivider:
			body = append(body, "")
		default:
			rowSelected := -1
			if index == selected {
				rowSelected = 0
			}
			body = append(body, renderRows(styles, []row{item.row()}, rowSelected, width)...)
		}
	}
	return body
}
