package dialog

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
)

func (p *Picker) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	frame := commandui.NewFrame(area.Dx(), area.Dy()).WithInnerWidth(0)
	items, selected, footer, footerSelected := p.visibleWindow(p.maxVisibleItems(area))
	if p.data.Filter {
		p.input.SetWidth(max(1, frame.InnerWidth()-2))
		view := commandui.RenderSearchListCard(frame, commandui.SearchListData{
			Title:          p.title(),
			SearchValue:    p.input.View(),
			SearchActive:   true,
			EmptyText:      "No items match",
			Items:          items,
			Selected:       selected,
			Footer:         footer,
			FooterSelected: footerSelected,
			Help:           p.legend(),
		})
		DrawCenter(scr, area, view)
		return nil
	}
	view := commandui.RenderPickerCard(frame, commandui.PickerData{
		Title:           p.title(),
		Meta:            p.meta(),
		Items:           items,
		Selected:        selected,
		Footer:          footer,
		FooterSelected:  footerSelected,
		MinVisibleItems: p.maxVisibleItems(area),
		Help:            p.legend(),
	})
	DrawCenter(scr, area, view)
	return nil
}

func (p *Picker) title() string {
	if title := strings.TrimSpace(p.data.Title); title != "" {
		return loadingTitle(title, p.loading, p.frame)
	}
	return loadingTitle("Choose", p.loading, p.frame)
}

func (p *Picker) meta() string {
	return strings.TrimSpace(p.data.Meta)
}

func (p *Picker) legend() string {
	if legend := strings.TrimSpace(p.data.Legend); legend != "" {
		return legend
	}
	return "enter select · esc cancel"
}

func (p *Picker) maxVisibleItems(area uv.Rectangle) int {
	height := defaultDialogHeight
	if area.Dy() > 0 {
		height = min(height, area.Dy())
	}
	fixedRows := 7 // border, title, divider, body gap, footer gap, footer
	if p.data.Filter {
		fixedRows += 2 // search field plus gap below it
	}
	return max(3, height-fixedRows)
}
