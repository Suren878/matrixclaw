package header

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

// DefaultStatusTTL matches the surface status message TTL.
const DefaultStatusTTL = 5 * time.Second

// StatusInfoType mirrors the surface status indicator kinds.
type StatusInfoType int

const (
	StatusInfoTypeInfo StatusInfoType = iota
	StatusInfoTypeSuccess
	StatusInfoTypeWarn
	StatusInfoTypeError
	StatusInfoTypeUpdate
)

// StatusInfo is the daemon-facing data boundary for status messages.
type StatusInfo struct {
	Type StatusInfoType
	Msg  string
	TTL  time.Duration
}

// IsEmpty reports whether the status message is empty.
func (m StatusInfo) IsEmpty() bool {
	var zero StatusInfo
	return m == zero
}

// StatusData is the daemon-facing data boundary for the status/help shell.
type StatusData struct {
	HideHelp bool
	HelpView string
	Info     StatusInfo
}

// Status renders the status/help shell.
type Status struct {
	styles *surfacestyles.Styles
}

// NewStatus creates a status/help renderer.
func NewStatus(styles *surfacestyles.Styles) *Status {
	if styles == nil {
		defaultStyles := surfacestyles.DefaultStyles()
		styles = &defaultStyles
	}
	return &Status{styles: styles}
}

// Draw renders the status/help shell into the provided screen area.
func (s *Status) Draw(scr uv.Screen, area uv.Rectangle, data StatusData) {
	if scr == nil {
		return
	}
	helpView, infoView := s.Views(area.Dx(), data)
	if helpView != "" {
		uv.NewStyledString(helpView).Draw(scr, area)
	}
	if infoView != "" {
		uv.NewStyledString(infoView).Draw(scr, area)
	}
}

// Views renders the help layer and optional overlaid info line.
func (s *Status) Views(width int, data StatusData) (helpView string, infoView string) {
	if s == nil || s.styles == nil || width <= 0 {
		return "", ""
	}

	if !data.HideHelp {
		helpView = s.styles.Status.Help.Render(data.HelpView)
	}

	if data.Info.IsEmpty() {
		return helpView, ""
	}

	var indStyle lipgloss.Style
	var msgStyle lipgloss.Style
	switch data.Info.Type {
	case StatusInfoTypeError:
		indStyle = s.styles.Status.ErrorIndicator
		msgStyle = s.styles.Status.ErrorMessage
	case StatusInfoTypeWarn:
		indStyle = s.styles.Status.WarnIndicator
		msgStyle = s.styles.Status.WarnMessage
	case StatusInfoTypeUpdate:
		indStyle = s.styles.Status.UpdateIndicator
		msgStyle = s.styles.Status.UpdateMessage
	case StatusInfoTypeInfo:
		indStyle = s.styles.Status.InfoIndicator
		msgStyle = s.styles.Status.InfoMessage
	case StatusInfoTypeSuccess:
		indStyle = s.styles.Status.SuccessIndicator
		msgStyle = s.styles.Status.SuccessMessage
	default:
		indStyle = s.styles.Status.InfoIndicator
		msgStyle = s.styles.Status.InfoMessage
	}

	ind := indStyle.String()
	indWidth := lipgloss.Width(ind)
	msg := strings.Join(strings.Split(data.Info.Msg, "\n"), " ")
	msgWidth := lipgloss.Width(msg)
	msg = ansi.Truncate(msg, width-indWidth-msgWidth, "…")
	padWidth := max(0, width-indWidth-msgWidth)
	msg += strings.Repeat(" ", padWidth)
	infoView = ind + msgStyle.Render(msg)
	return helpView, infoView
}
