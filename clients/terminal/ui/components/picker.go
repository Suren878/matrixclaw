package components

type PickerData struct {
	Title           string
	Meta            string
	Items           []Item
	Selected        int
	Footer          []Item
	FooterSelected  int
	MinVisibleItems int
	Help            string
}

func RenderPickerCard(frame Frame, data PickerData) string {
	help := helpWithShortcuts(firstNonEmpty(data.Help, "enter select · ↑/↓ move · esc close"), append(data.Items, data.Footer...))
	frame = frame.WithInnerWidth(0)
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Meta:  data.Meta,
		Body:  renderPickerItemsWithFooter(frame, data.Items, data.Selected, data.Footer, data.FooterSelected, data.MinVisibleItems),
		Help:  help,
	})
}

func renderPickerItemsWithFooter(frame Frame, items []Item, selected int, footer []Item, footerSelected int, minVisibleItems int) []string {
	body := renderItemLines(frame, items, selected)
	for len(body) < minVisibleItems {
		body = append(body, "")
	}
	if len(footer) > 0 {
		body = append(body, "")
		body = append(body, renderItemLines(frame, footer, footerSelected)...)
	}
	return body
}
