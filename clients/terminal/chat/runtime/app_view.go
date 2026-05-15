package runtime

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

func (m *appModel) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	// Do not set View.ForegroundColor/BackgroundColor here. Bubble Tea v2 maps
	// those fields to terminal default colors (OSC 10/11), which leaks into all
	// child rendering and makes setup/chat palettes drift globally.
	return view
}

func (m *appModel) viewContent() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	layout := m.layout()
	canvas := uv.NewScreenBuffer(m.width, m.height)
	m.drawContent(canvas, layout)
	if m.dialog.HasDialogs() {
		m.dialog.Draw(canvas, canvas.Bounds())
	}
	return normalizeScreenContent(canvas.Render())
}

func (m *appModel) drawContent(canvas uv.Screen, layout appLayout) {
	if layout.headerHeight > 0 {
		uv.NewStyledString(layout.headerView).Draw(canvas, uv.Rect(0, 0, m.width, layout.headerHeight))
	}
	if layout.footerHeight > 0 {
		footerRect := uv.Rect(0, m.height-layout.footerHeight, m.width, m.height)
		if strings.TrimSpace(layout.statusHelpView) != "" {
			uv.NewStyledString(layout.statusHelpView).Draw(canvas, footerRect)
		}
		if strings.TrimSpace(layout.statusInfoView) != "" {
			uv.NewStyledString(layout.statusInfoView).Draw(canvas, footerRect)
		}
	}

	chatWidth := layout.chatWidth(m.width)
	if m.chat != nil && layout.bodyBottom > layout.bodyTop {
		m.chat.Draw(canvas, uv.Rect(0, layout.bodyTop, chatWidth, layout.bodyBottom))
	} else if m.loading {
		uv.NewStyledString(m.styles.Base.Render("Loading terminal...")).Draw(canvas, uv.Rect(0, layout.bodyTop, chatWidth, layout.bodyBottom))
	} else if m.err != "" {
		uv.NewStyledString(m.styles.TagError.Render("Error: ")+m.styles.Base.Render(m.err)).Draw(canvas, uv.Rect(0, layout.bodyTop, chatWidth, layout.bodyBottom))
	} else {
		uv.NewStyledString(m.styles.Base.Render("No active session")).Draw(canvas, uv.Rect(0, layout.bodyTop, chatWidth, layout.bodyBottom))
	}
	if layout.planWidth > 0 && layout.bodyBottom > layout.bodyTop {
		uv.NewStyledString(m.planPanelView(layout.planWidth, layout.bodyHeight())).Draw(canvas, uv.Rect(chatWidth, layout.bodyTop, m.width, layout.bodyBottom))
	}
	if layout.inputHeight > 0 {
		uv.NewStyledString(layout.inputView).Draw(canvas, uv.Rect(0, layout.editorTop, m.width, layout.editorBottom))
	}
}

func (m *appModel) contentView(layout appLayout) string {
	sections := make([]string, 0, 4)
	if layout.headerHeight > 0 {
		sections = append(sections, renderRegion(m.width, layout.headerHeight, layout.headerView))
	}
	if bodyHeight := layout.bodyHeight(); bodyHeight > 0 {
		body := renderRegion(layout.chatWidth(m.width), bodyHeight, m.bodyView())
		if layout.planWidth > 0 {
			body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.planPanelView(layout.planWidth, bodyHeight))
		}
		sections = append(sections, renderRegion(m.width, bodyHeight, body))
	}
	if layout.inputHeight > 0 {
		sections = append(sections, renderRegion(m.width, layout.inputHeight, layout.inputView))
	}
	if layout.footerHeight > 0 {
		sections = append(sections, renderRegion(m.width, layout.footerHeight, layout.footerView()))
	}
	return strings.Join(sections, "\n")
}

func (m *appModel) bodyView() string {
	if m.chat != nil {
		return m.chat.View()
	}
	if m.loading {
		return m.styles.Base.Render("Loading terminal...")
	}
	if m.err != "" {
		return m.styles.TagError.Render("Error: ") + m.styles.Base.Render(m.err)
	}
	return m.styles.Base.Render("No active session")
}

func renderRegion(width int, height int, content string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, content)
}

func normalizeScreenContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}
