package runtime

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	surfaceheader "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/header"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) headerView() string {
	if m.header == nil || m.width <= 0 {
		return ""
	}
	return m.header.View(m.width, m.isCompactLayout(), surfaceheader.Data{
		LSPErrorCount: 0,
		UsageText:     m.contextUsageText(),
	})
}

func (m *appModel) footerView() string {
	if m.width > 0 {
		m.help.SetWidth(m.width)
	}
	return m.help.View(m)
}

func (m *appModel) statusViews() (string, string) {
	if m.status == nil || m.width <= 0 {
		return m.footerView(), ""
	}
	data := surfaceheader.StatusData{
		HelpView: m.footerView(),
	}
	if strings.TrimSpace(m.err) != "" {
		data.Info = surfaceheader.StatusInfo{
			Type: surfaceheader.StatusInfoTypeError,
			Msg:  m.err,
		}
	}
	return m.status.Views(m.width, data)
}

var workingSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *appModel) workingStatusView() string {
	if !m.busy || m.width <= 0 {
		return ""
	}
	run := m.currentRun()
	if !runIsActive(run) {
		return ""
	}
	model := m.currentModelLabel()
	if model == "" {
		model = "model"
	}
	spinner := workingSpinnerFrames[m.spinnerFrame%len(workingSpinnerFrames)]
	elapsed := "0s"
	if run != nil && !run.StartedAt.IsZero() {
		elapsed = formatWorkingElapsed(m.now.Sub(run.StartedAt))
	}
	phase := m.workingStatusPhase()
	timing := elapsed
	if idle := m.workingIdleElapsed(); idle != "" {
		timing += ", idle " + idle
	}
	line := lipgloss.NewStyle().Foreground(lipgloss.Color(colorToHex(m.styles.Primary))).Render("[" + model + "] " + spinner + " " + phase + " (" + timing + " • esc to cancel)")
	return line
}

func (m *appModel) workingStatusPhase() string {
	if m.read == nil {
		return "Waiting for model"
	}
	snapshot := m.read.Snapshot()
	if len(snapshot.Approvals) > 0 {
		return "Waiting for permission"
	}
	for i := len(snapshot.ToolUpdates) - 1; i >= 0; i-- {
		update := snapshot.ToolUpdates[i]
		switch update.State {
		case core.ToolLifecycleRequested:
			return "Running " + displayToolName(update.ToolName)
		case core.ToolLifecycleWaitingApproval:
			return "Waiting for permission"
		}
	}
	if snapshot.Run != nil && snapshot.Run.Status == core.RunStatusAccepted {
		return "Queued"
	}
	if m.modelIsThinking(snapshot.Messages) {
		return "Thinking"
	}
	return "Waiting for model"
}

func (m *appModel) modelIsThinking(messages []surfacemessage.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role == surfacemessage.User {
			return false
		}
		if message.Role != surfacemessage.Assistant && message.Role != surfacemessage.System {
			continue
		}
		if message.IsThinking() {
			return true
		}
		if strings.TrimSpace(message.Content().Text) != "" || len(message.ToolCalls()) > 0 || message.IsFinished() {
			return false
		}
	}
	return false
}

func (m *appModel) workingIdleElapsed() string {
	if m.read == nil {
		return ""
	}
	timing := m.read.Snapshot().Timing
	if timing == nil || timing.LastEventAt.IsZero() {
		return ""
	}
	idle := m.now.Sub(timing.LastEventAt)
	if idle < 10*time.Second {
		return ""
	}
	return formatWorkingElapsed(idle)
}

func displayToolName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash":
		return "Run"
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit":
		return "Edit"
	case "multiedit":
		return "MultiEdit"
	case "grep":
		return "Grep"
	case "glob":
		return "Glob"
	case "ls":
		return "List"
	case "":
		return "tool"
	default:
		return strings.TrimSpace(name)
	}
}

func formatWorkingElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second).Seconds())
	minutes := total / 60
	seconds := total % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (m *appModel) inputSeparatorView() string {
	if m.width <= 0 {
		return ""
	}
	return m.styles.Section.Line.Render(strings.Repeat(surfacestyles.SectionSeparator, m.width))
}

func (m *appModel) editorView() string {
	if m.width <= 0 {
		return ""
	}
	return m.input.Render(m.editorWidth())
}

func (m *appModel) inputSectionView() string {
	if m.width <= 0 {
		return ""
	}
	parts := make([]string, 0, 6)
	if working := strings.TrimRight(m.workingStatusView(), "\n"); strings.TrimSpace(working) != "" {
		parts = append(parts, "", working, "")
	}
	if separator := strings.TrimRight(m.inputSeparatorView(), "\n"); strings.TrimSpace(separator) != "" {
		parts = append(parts, separator)
	}
	if editor := strings.TrimRight(m.editorView(), "\n"); strings.TrimSpace(editor) != "" {
		parts = append(parts, editor)
	}
	if len(parts) == 0 {
		return ""
	}
	parts = append([]string{""}, parts...)
	return strings.Join(parts, "\n")
}

func colorToHex(c interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
