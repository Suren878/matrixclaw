package dialog

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
)

func (p *Picker) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	frame := components.NewFrame(area.Dx(), area.Dy()).WithInnerWidth(0)
	items, selected, footer, footerSelected := p.visibleWindow(p.maxVisibleItems(area))
	if p.data.Filter {
		p.input.SetWidth(max(1, frame.InnerWidth()-2))
		view := components.RenderSearchListCard(frame, components.SearchListData{
			Title:          p.title(),
			Meta:           p.meta(),
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
	view := components.RenderPickerCard(frame, components.PickerData{
		Title:           p.title(),
		Meta:            p.meta(),
		Items:           items,
		Selected:        selected,
		Footer:          footer,
		FooterSelected:  footerSelected,
		MinVisibleItems: p.minVisibleItems(area),
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
	if meta := strings.TrimSpace(p.data.Meta); meta != "" {
		return meta
	}
	for _, option := range p.options {
		if !option.selected || !option.item.Selectable() {
			continue
		}
		if title := strings.TrimSpace(option.item.Title); title != "" {
			return title
		}
		if status := strings.TrimSpace(option.item.Status); status != "" {
			return status
		}
	}
	return ""
}

func (p *Picker) legend() string {
	if legend := strings.TrimSpace(p.data.Legend); legend != "" {
		return legend
	}
	return "enter select · esc close"
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

func (p *Picker) minVisibleItems(area uv.Rectangle) int {
	if !p.data.ShowFooter {
		return 0
	}
	return p.maxVisibleItems(area)
}
