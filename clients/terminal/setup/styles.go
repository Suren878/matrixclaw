package setup

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/theme"
)

const (
	colBright     = theme.Bright
	colHead       = theme.Head
	colTail       = theme.Tail
	colDim        = theme.Dim
	colBorder     = theme.Border
	colText       = theme.Text
	colMuted      = theme.Muted
	colSelectedFg = theme.SelectedFg
)

var (
	bgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colText))

	setupSubtitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colText))

	setupFooterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colMuted))

	logoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colBright)).
			Bold(true)

	splashPopupStyle = lipgloss.NewStyle().
				Width(68).
				Padding(2, 4).
				Border(lipgloss.DoubleBorder()).
				BorderForeground(lipgloss.Color(colBorder))

	splashEnterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colSelectedFg)).
				Background(lipgloss.Color(colBright)).
				Bold(true).
				Padding(0, 2)

	rainHeadStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colHead)).
			Bold(true)

	rainBrightTailStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colBright))

	rainTailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colTail))

	rainDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colDim))
)

var matrixClawLogoLines = []string{
	"███╗   ███╗ █████╗ ████████╗██████╗ ██╗██╗  ██╗",
	"████╗ ████║██╔══██╗╚══██╔══╝██╔══██╗██║╚██╗██╔╝",
	"██╔████╔██║███████║   ██║   ██████╔╝██║ ╚███╔╝ ",
	"██║╚██╔╝██║██╔══██║   ██║   ██╔══██╗██║ ██╔██╗ ",
	"██║ ╚═╝ ██║██║  ██║   ██║   ██║  ██║██║██╔╝ ██╗",
	"╚═╝     ╚═╝╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚═╝╚═╝  ╚═╝",
	"",
	"      ██████╗██╗      █████╗ ██╗    ██╗ ",
	"     ██╔════╝██║     ██╔══██╗██║    ██║",
	"     ██║     ██║     ███████║██║ █╗ ██║",
	"     ██║     ██║     ██╔══██║██║███╗██║",
	"     ╚██████╗███████╗██║  ██║╚███╔███╔╝",
	"      ╚═════╝╚══════╝╚═╝  ╚═╝ ╚══╝╚══╝ ",
}

func centerBlock(width int, text string) string {
	return lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Render(text)
}

func logoWordmark(width int) string {
	if width < 56 {
		return logoStyle.Render("MATRIXCLAW")
	}
	return lipgloss.NewStyle().
		PaddingLeft(6).
		Render(logoStyle.Render(strings.Join(matrixClawLogoLines, "\n")))
}

func styleTextArea(in *textarea.Model) {
	styles := textarea.DefaultDarkStyles()
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(colText))
	placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color(colMuted))
	styles.Focused.Text = text
	styles.Focused.Placeholder = placeholder
	styles.Focused.Prompt = lipgloss.NewStyle()
	styles.Blurred = styles.Focused
	styles.Cursor.Color = lipgloss.Color(colHead)
	in.SetStyles(styles)
}

func overlayAtRect(background, foreground string, width, height int, box rect) string {
	bgLines := strings.Split(background, "\n")
	emptyLine := lipgloss.NewStyle().Width(max(1, width)).Render("")
	for len(bgLines) < height {
		bgLines = append(bgLines, emptyLine)
	}
	fgLines := strings.Split(foreground, "\n")
	for i, line := range fgLines {
		row := box.y + i
		if row >= len(bgLines) {
			break
		}
		fgWidth := lipgloss.Width(line)
		left := ansi.Cut(bgLines[row], 0, box.x)
		right := ansi.Cut(bgLines[row], box.x+fgWidth, width)
		bgLines[row] = left + line + right
	}
	return strings.Join(bgLines[:height], "\n")
}
