package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	minInnerWidth       = 28
	preferredFrameWidth = 82
)

type Frame struct {
	Styles Styles
	Width  int
	Height int
}

type FrameData struct {
	Title      string
	Subtitle   string
	Meta       string
	Body       []string
	Help       string
	Error      string
	HideHeader bool
}

func NewFrame(width int, height int) Frame {
	return Frame{Styles: DefaultStyles(), Width: width, Height: height}
}

func (f Frame) RenderCard(data FrameData) string {
	styles := f.styles()
	innerWidth := f.InnerWidth()
	content := make([]string, 0, len(data.Body)+6)
	if !data.HideHeader {
		content = append(content, f.header(data.Title, data.Meta))
		if strings.TrimSpace(data.Subtitle) != "" {
			content = append(content, renderTruncated(styles.Subtitle, data.Subtitle, innerWidth))
		}
		content = append(content, f.TitleDivider())
		content = append(content, "")
	}
	for _, line := range data.Body {
		content = append(content, truncateLine(line, innerWidth))
	}
	if strings.TrimSpace(data.Error) != "" {
		content = append(content, "", renderTruncated(styles.Error, data.Error, innerWidth))
	}
	if strings.TrimSpace(data.Help) != "" {
		content = append(content, "", renderTruncated(styles.Footer, data.Help, innerWidth))
	}
	return styles.Card.Render(strings.Join(content, "\n"))
}

func (f Frame) InnerWidth() int {
	width := f.styles().Card.GetWidth()
	if width <= 0 {
		return f.targetInnerWidth()
	}
	width -= f.styles().Card.GetHorizontalFrameSize()
	return max(minInnerWidth, width)
}

func (f Frame) WithInnerWidth(width int) Frame {
	styles := f.styles()
	if width <= 0 {
		width = f.targetInnerWidth()
	} else {
		width = f.clampInnerWidth(width)
	}
	styles.Card = styles.Card.Width(width + styles.Card.GetHorizontalFrameSize())
	f.Styles = styles
	return f
}

func (f Frame) TitleDivider() string {
	return f.styles().Divider.Render(strings.Repeat("─", f.InnerWidth()))
}

func (f Frame) header(title string, meta string) string {
	styles := f.styles()
	title = strings.TrimSpace(title)
	meta = strings.TrimSpace(meta)
	if title == "" {
		title = "matrixclaw"
	}
	if meta == "" {
		return renderTruncated(styles.Title, title, f.InnerWidth())
	}
	width := f.InnerWidth()
	metaWidth := min(styledWidth(styles.Footer, meta), max(0, width/2))
	if metaWidth > width-6 {
		metaWidth = max(0, width-6)
	}
	if metaWidth == 0 {
		return renderTruncated(styles.Title, title, width)
	}
	leftWidth := max(0, width-metaWidth-2)
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		renderTruncated(styles.Title.Width(leftWidth), title, leftWidth),
		renderTruncated(styles.Footer, meta, metaWidth),
	)
}

func (f Frame) styles() Styles {
	if f.Styles.Card.GetWidth() == 0 {
		return DefaultStyles()
	}
	return f.Styles
}

func (f Frame) clampInnerWidth(width int) int {
	styles := f.styles()
	width = max(minInnerWidth, width)
	if f.Width <= 0 {
		return max(width, preferredFrameWidth-styles.Card.GetHorizontalFrameSize())
	}
	viewportWidth := f.Width
	if viewportWidth > 40 {
		viewportWidth -= 4
	}
	maxWidth := viewportWidth - styles.Card.GetHorizontalFrameSize()
	if maxWidth < 12 {
		maxWidth = 12
	}
	if width > maxWidth {
		return maxWidth
	}
	return width
}

func (f Frame) targetInnerWidth() int {
	styles := f.styles()
	return f.clampInnerWidth(preferredFrameWidth - styles.Card.GetHorizontalFrameSize())
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func renderTruncated(style lipgloss.Style, value string, width int) string {
	value = strings.TrimSpace(value)
	return renderStyledLine(style, value, width)
}

func renderStyledLine(style lipgloss.Style, value string, width int) string {
	if value == "" || width <= 0 {
		return ""
	}
	contentWidth := max(0, width-style.GetHorizontalFrameSize())
	value = ansi.Truncate(value, contentWidth, "…")
	return style.Width(width).Render(value)
}

func truncateLine(line string, width int) string {
	if strings.TrimSpace(line) == "" || width <= 0 {
		return line
	}
	return ansi.Truncate(line, width, "…")
}

func styledWidth(style lipgloss.Style, value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	return lipgloss.Width(style.Render(value))
}
