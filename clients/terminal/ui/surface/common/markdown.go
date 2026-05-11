package common

import (
	"strings"

	"charm.land/glamour/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

// MarkdownRenderer returns a glamour renderer configured with the given styles.
func MarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer {
	r, _ := glamour.NewTermRenderer(
		glamour.WithStyles(sty.Markdown),
		glamour.WithWordWrap(width),
	)
	return r
}

// PlainMarkdownRenderer returns a renderer for thinking/plain sections.
func PlainMarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer {
	r, _ := glamour.NewTermRenderer(
		glamour.WithStyles(sty.PlainMarkdown),
		glamour.WithWordWrap(width),
	)
	return r
}

// RenderMessageText renders shared chat text outside the assistant reply renderer.
func RenderMessageText(content string, sty *styles.Styles, width int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if !HasMarkdownSyntax(content) {
		return HighlightPlainPaths(content, sty)
	}
	renderer := MarkdownRenderer(sty, width)
	result, err := renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSuffix(result, "\n")
}
