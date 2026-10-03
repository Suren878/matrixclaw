package header

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const (
	leftPadding  = 1
	rightPadding = 1
)

// Header renders the terminal header shell.
type Header struct {
	styles  *surfacestyles.Styles
	version string
}

// New creates a terminal header renderer.
func New(styles *surfacestyles.Styles, version string) *Header {
	if styles == nil {
		defaultStyles := surfacestyles.DefaultStyles()
		styles = &defaultStyles
	}
	return &Header{
		styles:  styles,
		version: version,
	}
}

// View renders the terminal header shell with the context usage text on the right.
func (h *Header) View(width int, usageText string) string {
	if h == nil || h.styles == nil || width <= 0 {
		return ""
	}
	t := h.styles
	innerWidth := max(0, width-leftPadding-rightPadding)
	metadata := renderHeaderMetadata(t, h.appTitle(), usageText, innerWidth)
	if strings.TrimSpace(metadata) == "" {
		return ""
	}
	return t.Base.Padding(0, rightPadding, 0, leftPadding).Render(metadata)
}

func (h *Header) appTitle() string {
	version := strings.TrimSpace(h.version)
	if version == "" {
		version = "0.1.0"
	}
	version = strings.TrimPrefix(version, "v")
	return "matrixclaw v" + version
}

func renderHeaderMetadata(styles *surfacestyles.Styles, title string, usageText string, availWidth int) string {
	title = styles.Header.Title.Render(title)
	meta := ""
	if usageText = strings.TrimSpace(usageText); usageText != "" {
		meta = styles.Header.Meta.Render(usageText)
	}
	line := title + " " + headerSlashFill(styles, max(0, availWidth-lipWidth(title)-lipWidth(meta)-2))
	if meta != "" {
		line += " " + meta
	}
	return ansi.Truncate(line, max(0, availWidth), "…")
}

func headerSlashFill(styles *surfacestyles.Styles, width int) string {
	if width <= 0 {
		return ""
	}
	return surfacestyles.ApplyForegroundGrad(styles, strings.Repeat("╱", width), styles.Primary, styles.Secondary)
}

func lipWidth(value string) int {
	return ansi.StringWidth(value)
}
