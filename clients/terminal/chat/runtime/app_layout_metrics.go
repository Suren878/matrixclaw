package runtime

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type appLayout struct {
	headerView     string
	statusHelpView string
	statusInfoView string
	inputView      string
	planWidth      int
	headerHeight   int
	footerHeight   int
	inputHeight    int
	bodyTop        int
	bodyBottom     int
	editorTop      int
	editorBottom   int
}

func (layout appLayout) bodyHeight() int {
	return max(0, layout.bodyBottom-layout.bodyTop)
}

func (layout appLayout) chatWidth(totalWidth int) int {
	return max(0, totalWidth-layout.planWidth)
}

func (layout appLayout) footerView() string {
	if strings.TrimSpace(layout.statusInfoView) != "" {
		return layout.statusInfoView
	}
	return layout.statusHelpView
}

func (m *appModel) layout() appLayout {
	headerView := m.headerView()
	statusHelpView, statusInfoView := m.statusViews()
	headerHeight, footerHeight := m.chromeHeights(headerView, statusHelpView)
	inputView := m.inputSectionView()
	inputHeight := lipgloss.Height(inputView)
	if inputHeight < 0 {
		inputHeight = 0
	}

	bodyTop := headerHeight
	bodyBottom := m.height - footerHeight - inputHeight
	if bodyBottom < bodyTop {
		bodyBottom = bodyTop
	}
	editorTop := m.height - footerHeight - inputHeight
	if editorTop < headerHeight {
		editorTop = headerHeight
	}
	editorBottom := m.height - footerHeight
	if editorBottom < editorTop {
		editorBottom = editorTop
	}

	return appLayout{
		headerView:     headerView,
		statusHelpView: statusHelpView,
		statusInfoView: statusInfoView,
		inputView:      inputView,
		planWidth:      m.visiblePlanPanelWidth(),
		headerHeight:   headerHeight,
		footerHeight:   footerHeight,
		inputHeight:    inputHeight,
		bodyTop:        bodyTop,
		bodyBottom:     bodyBottom,
		editorTop:      editorTop,
		editorBottom:   editorBottom,
	}
}

func (m *appModel) resizeChat() {
	if m.chat == nil || m.width <= 0 || m.height <= 0 {
		return
	}
	layout := m.layout()
	bodyHeight := layout.bodyBottom - layout.bodyTop
	if bodyHeight < 1 {
		bodyHeight = m.height
	}
	m.chat.SetSize(layout.chatWidth(m.width), bodyHeight)
}

func (m *appModel) bodyBounds() (int, int) {
	layout := m.layout()
	return layout.bodyTop, layout.bodyBottom
}

func (m *appModel) editorBounds() (int, int) {
	layout := m.layout()
	return layout.editorTop, layout.editorBottom
}

func (m *appModel) chromeHeights(header string, footer string) (int, int) {
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)
	if m.height < 4 {
		return 0, 0
	}
	if headerHeight < 0 {
		headerHeight = 0
	}
	if footerHeight < 0 {
		footerHeight = 0
	}
	if headerHeight+footerHeight >= m.height {
		if headerHeight > 0 {
			headerHeight = 1
		}
		if footerHeight > 0 {
			footerHeight = 1
		}
	}
	return headerHeight, footerHeight
}

func (m *appModel) editorWidth() int {
	if m.width <= 0 {
		return 0
	}
	return m.width
}

func (m *appModel) visiblePlanPanelWidth() int {
	if !m.shouldShowPlanPanel() {
		return 0
	}
	return m.availablePlanPanelWidth()
}

func (m *appModel) availablePlanPanelWidth() int {
	if m.width < 132 || m.height < compactModeHeightBreakpoint {
		return 0
	}
	width := 36
	if m.width >= 170 {
		width = 42
	}
	if m.width-width < 80 {
		return 0
	}
	return width
}

func (m *appModel) isCompactLayout() bool {
	if m.width <= 0 || m.height <= 0 {
		return false
	}
	return m.width < compactModeWidthBreakpoint || m.height < compactModeHeightBreakpoint
}
