package runtime

import "charm.land/lipgloss/v2"

type appLayout struct {
	headerView     string
	statusHelpView string
	statusInfoView string
	inputView      string
	todoWidth      int
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
	return max(0, totalWidth-layout.todoWidth)
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
		todoWidth:      m.visibleTodoPanelWidth(),
		headerHeight:   headerHeight,
		footerHeight:   footerHeight,
		inputHeight:    inputHeight,
		bodyTop:        bodyTop,
		bodyBottom:     bodyBottom,
		editorTop:      editorTop,
		editorBottom:   editorBottom,
	}
}

// relayout lays the screen out once per update: View draws m.frame, mouse
// hits are tested against it, and the chat is resized when its area changed.
func (m *appModel) relayout() {
	if m.width <= 0 || m.height <= 0 {
		m.frame = appLayout{}
		return
	}
	m.frame = m.layout()
	if m.chat == nil {
		return
	}
	width, height := m.frame.chatWidth(m.width), m.frame.bodyHeight()
	if height < 1 {
		height = m.height
	}
	if width != m.chatWidth || height != m.chat.Height() {
		m.chatWidth = width
		m.chat.SetSize(width, height)
	}
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

func (m *appModel) visibleTodoPanelWidth() int {
	if !m.shouldShowTodoPanel() {
		return 0
	}
	return m.availableTodoPanelWidth()
}

func (m *appModel) availableTodoPanelWidth() int {
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
