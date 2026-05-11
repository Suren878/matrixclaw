package commandui

type InfoData struct {
	Title          string
	Meta           string
	Items          []Item
	Selected       int
	Footer         []Item
	FooterSelected int
	Help           string
}

func RenderInfoCard(frame Frame, data InfoData) string {
	help := firstNonEmpty(data.Help, "enter/esc back")
	frame = frame.WithInnerWidth(0)
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Meta:  data.Meta,
		Body:  renderItemsWithFooter(frame, data.Items, data.Selected, nil, data.Footer, data.FooterSelected),
		Help:  help,
	})
}
