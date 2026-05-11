package commandui

type PickerData struct {
	Title          string
	Meta           string
	Items          []Item
	Selected       int
	Footer         []Item
	FooterSelected int
	Help           string
}

func RenderPickerCard(frame Frame, data PickerData) string {
	help := helpWithShortcuts(firstNonEmpty(data.Help, "enter select · ↑/↓ move · esc cancel"), append(data.Items, data.Footer...))
	frame = frame.WithInnerWidth(0)
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Meta:  data.Meta,
		Body:  renderItemsWithFooter(frame, data.Items, data.Selected, nil, data.Footer, data.FooterSelected),
		Help:  help,
	})
}
