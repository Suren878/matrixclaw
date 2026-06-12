package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type ConfirmData struct {
	Message       string
	ConfirmLabel  string
	CancelLabel   string
	Selected      int
	ConfirmDanger bool
	CancelDanger  bool
	OnlyCancel    bool
}

func RenderConfirmCard(frame Frame, data ConfirmData) string {
	frame = frame.WithInnerWidth(0)
	body := confirmBody(frame, data)
	return frame.RenderCard(FrameData{
		Body:       body,
		Help:       confirmHelp(data),
		HideHeader: true,
	})
}

func confirmBody(frame Frame, data ConfirmData) []string {
	styles := frame.styles()
	width := frame.InnerWidth()
	lines := wrapText(strings.TrimSpace(data.Message), width)
	if len(lines) == 0 {
		lines = []string{""}
	}
	body := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		body = append(body, renderInputLine(styles.Muted.Align(lipgloss.Center), line, width))
	}
	body = append(body, "")
	body = append(body, renderButtonSpecs(styles, width, confirmButtonSpecs(data), data.Selected, true, false))
	return body
}

func confirmButtonSpecs(data ConfirmData) []ButtonSpec {
	if data.OnlyCancel {
		return []ButtonSpec{
			{Label: firstNonEmpty(data.CancelLabel, "Close"), Role: RoleCancel, Danger: data.CancelDanger},
		}
	}
	return []ButtonSpec{
		{Label: firstNonEmpty(data.ConfirmLabel, "Confirm"), Role: RoleSubmit, Danger: data.ConfirmDanger},
		{Label: firstNonEmpty(data.CancelLabel, "Close"), Role: RoleCancel, Danger: data.CancelDanger},
	}
}

func confirmHelp(data ConfirmData) string {
	if data.OnlyCancel {
		return "enter/esc close"
	}
	return "←/→ switch · enter confirm · esc close"
}

func wrapText(text string, width int) []string {
	width = max(1, width)
	rawLines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	out := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		raw = strings.TrimRight(raw, " ")
		if raw == "" {
			out = append(out, "")
			continue
		}
		for ansi.StringWidth(raw) > width {
			cut := wrapCut(raw, width)
			out = append(out, strings.TrimRight(ansi.Cut(raw, 0, cut), " "))
			raw = strings.TrimLeft(ansi.Cut(raw, cut, ansi.StringWidth(raw)), " ")
		}
		out = append(out, raw)
	}
	return out
}

func wrapCut(value string, width int) int {
	width = max(1, width)
	if ansi.StringWidth(value) <= width {
		return ansi.StringWidth(value)
	}
	sample := ansi.Cut(value, 0, width)
	if idx := strings.LastIndex(sample, " "); idx > 0 {
		return ansi.StringWidth(sample[:idx])
	}
	return width
}
