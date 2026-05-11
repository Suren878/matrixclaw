package header

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const (
	leftPadding  = 1
	rightPadding = 1
)

// Data is the daemon-agnostic input required to render the terminal header shell.
type Data struct {
	LSPErrorCount int
	UsageText     string
}

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

// Draw renders the terminal header into the provided screen area.
func (h *Header) Draw(scr uv.Screen, area uv.Rectangle, data Data, compact bool, width int) {
	if scr == nil {
		return
	}
	if width <= 0 {
		width = area.Dx()
	}
	uv.NewStyledString(h.View(width, compact, data)).Draw(scr, area)
}

// View renders the terminal header shell.
func (h *Header) View(width int, compact bool, data Data) string {
	if h == nil || h.styles == nil || width <= 0 {
		return ""
	}
	t := h.styles
	innerWidth := max(0, width-leftPadding-rightPadding)
	metadata := renderHeaderMetadata(t, h.appTitle(), data, innerWidth)
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

func renderHeaderMetadata(styles *surfacestyles.Styles, title string, data Data, availWidth int) string {
	title = styles.Header.Title.Render(title)
	meta := renderHeaderMeta(styles, data)
	line := title + " " + headerSlashFill(styles, max(0, availWidth-lipWidth(title)-lipWidth(meta)-2))
	if meta != "" {
		line += " " + meta
	}
	return ansi.Truncate(line, max(0, availWidth), "…")
}

func renderHeaderMeta(styles *surfacestyles.Styles, data Data) string {
	var parts []string
	if data.LSPErrorCount > 0 {
		parts = append(parts, styles.LSP.ErrorDiagnostic.Render(fmt.Sprintf("%s%d", surfacestyles.LSPErrorIcon, data.LSPErrorCount)))
	}
	if usageText := strings.TrimSpace(data.UsageText); usageText != "" {
		parts = append(parts, styles.Header.Meta.Render(usageText))
	}

	dot := styles.Header.Separator.Render(" • ")
	return strings.Join(parts, dot)
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
