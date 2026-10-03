package runtime

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	surfaceheader "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/header"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/toolview"
)

func (m *appModel) headerView() string {
	if m.header == nil || m.width <= 0 {
		return ""
	}
	return m.header.View(m.width, m.contextUsageText())
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
	if m.notice.text != "" {
		data.Info = surfaceheader.StatusInfo{Type: m.notice.kind, Msg: m.notice.text}
	}
	return m.status.Views(m.width, data)
}

var workingSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *appModel) workingStatusView() string {
	if m.width <= 0 {
		return ""
	}
	if !m.input.busy {
		return m.waitingSubagentsStatusView()
	}
	run := m.state().Run()
	if !runIsActive(run) {
		return m.workingStatusLine(m.state(), nil, "Waiting for model", "")
	}
	snapshot := m.state()
	return m.workingStatusLine(snapshot, run, m.workingStatusPhase(), m.workingIdleElapsed())
}

func (m *appModel) workingStatusLine(snapshot *readmodel.Model, run *core.Run, phase string, idle string) string {
	_, model := m.currentSessionLLM()
	model = cmp.Or(model, "model")
	spinner := workingSpinnerFrames[m.spinnerFrame%len(workingSpinnerFrames)]
	elapsed := "0s"
	if run != nil && !run.StartedAt.IsZero() {
		elapsed = formatWorkingElapsed(m.now.Sub(run.StartedAt))
	}
	timing := elapsed
	if idle != "" {
		timing += ", idle " + idle
	}
	details := make([]string, 0, 2)
	if detail := pendingInputsStatusText(snapshot.PendingInputs()); detail != "" {
		details = append(details, detail)
	}
	if runIsActive(run) {
		if detail := activeSubagentsStatusTextForSnapshot(snapshot, phase); detail != "" {
			details = append(details, detail)
		}
	}
	detail := ""
	if len(details) > 0 {
		detail = " • " + strings.Join(details, " • ")
	}
	line := lipgloss.NewStyle().Foreground(m.styles.Primary).Render("[" + model + "] " + spinner + " " + phase + " (" + timing + " • esc to cancel" + detail + ")")
	return line
}

func (m *appModel) waitingSubagentsStatusView() string {
	snapshot := m.state()
	line := combinedWaitingStatusText(snapshot.PendingInputs(), snapshot.Subagents())
	if line == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(m.styles.Primary).Render(line)
}

func combinedWaitingStatusText(inputs []core.SessionInput, subagents []surfacemessage.Subagent) string {
	parts := make([]string, 0, 2)
	if text := pendingInputsStatusText(inputs); text != "" {
		parts = append(parts, text)
	}
	if text := activeSubagentsStatusText(subagents); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, " • ")
}

func pendingInputsStatusText(inputs []core.SessionInput) string {
	count := 0
	steers := 0
	interrupts := 0
	for _, input := range inputs {
		if input.Status != core.SessionInputStatusPending {
			continue
		}
		count++
		switch input.Mode {
		case core.BusyInputModeSteer:
			steers++
		case core.BusyInputModeInterrupt:
			interrupts++
		}
	}
	if count == 0 {
		return ""
	}
	switch {
	case interrupts > 0:
		if count == 1 {
			return "Interrupting with queued message"
		}
		return fmt.Sprintf("Pending inputs: %d, interrupting", count)
	case steers > 0:
		if count == 1 {
			return "Steer queued"
		}
		return fmt.Sprintf("Pending inputs: %d, steer queued", count)
	default:
		if count == 1 {
			return "Queued message pending"
		}
		return fmt.Sprintf("Queued messages pending: %d", count)
	}
}

func activeSubagentsStatusText(subagents []surfacemessage.Subagent) string {
	names := make([]string, 0, len(subagents))
	for _, sub := range subagents {
		if sub.State.Active() {
			names = append(names, sub.Name)
		}
	}
	return activeSubagentNamesStatusText(names)
}

func activeSubagentsStatusTextForSnapshot(snapshot *readmodel.Model, phase string) string {
	if strings.HasPrefix(strings.TrimSpace(phase), "Waiting for subagent:") {
		return ""
	}
	currentRunID := ""
	if run := snapshot.Run(); run != nil {
		currentRunID = strings.TrimSpace(run.ID)
	}
	names := make([]string, 0, len(snapshot.Subagents()))
	for _, sub := range snapshot.Subagents() {
		if !sub.State.Active() || (sub.Blocking && currentRunID != "" && sub.ParentRunID != currentRunID) {
			continue
		}
		names = append(names, sub.Name)
	}
	return activeSubagentNamesStatusText(names)
}

func activeSubagentNamesStatusText(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "Subagents: " + strings.Join(names, ", ")
}

func (m *appModel) workingStatusPhase() string {
	snapshot := m.state()
	if activeRunWaitingForPermission(snapshot) {
		return "Waiting for permission"
	}
	if snapshot.Run() != nil && snapshot.Run().Status == core.RunStatusWaitingEvents {
		return "Waiting for background tasks"
	}
	if update, ok := latestActiveToolUpdate(snapshot, core.ToolLifecycleRequested); ok {
		if isSubagentToolName(update.ToolName) {
			if phase := activeSubagentPhase(snapshot); phase != "" {
				return phase
			}
		}
		return workingToolPhaseWithDetail(snapshot.Messages(), update)
	}
	if snapshot.Run() != nil && snapshot.Run().Status == core.RunStatusAccepted {
		return "Waiting for model"
	}
	if phase := modelOutputPhase(snapshot.Messages()); phase != "" {
		return phase
	}
	if phase := activeSubagentPhase(snapshot); phase != "" {
		return phase
	}
	return "Waiting for model"
}

func activeRunWaitingForPermission(snapshot *readmodel.Model) bool {
	if update, ok := latestActiveToolUpdate(snapshot, core.ToolLifecycleWaitingApproval); ok && strings.TrimSpace(update.ToolCallID) != "" {
		return true
	}
	return snapshot.Run() != nil && snapshot.Run().Status == core.RunStatusWaitingApproval && len(snapshot.Approvals()) > 0
}

func activeSubagentPhase(snapshot *readmodel.Model) string {
	run := snapshot.Run()
	if run == nil || strings.TrimSpace(run.ID) == "" {
		return ""
	}
	subagents := snapshot.Subagents()
	for i := len(subagents) - 1; i >= 0; i-- {
		sub := subagents[i]
		if !sub.Blocking || sub.ParentRunID != strings.TrimSpace(run.ID) {
			continue
		}
		switch sub.State {
		case surfacemessage.SubagentPending:
			return "Starting subagent: " + sub.Name
		case surfacemessage.SubagentRunning:
			return "Waiting for subagent: " + sub.Name
		case surfacemessage.SubagentWaitingApproval:
			return "Subagent waiting for permission: " + sub.Name
		}
	}
	return ""
}

func latestActiveToolUpdate(snapshot *readmodel.Model, state core.ToolLifecycleState) (core.ToolUpdate, bool) {
	for i := len(snapshot.ToolUpdates()) - 1; i >= 0; i-- {
		update := snapshot.ToolUpdates()[i]
		if update.State != state {
			continue
		}
		if !toolUpdateBelongsToCurrentRun(snapshot, update) {
			continue
		}
		return update, true
	}
	return core.ToolUpdate{}, false
}

func toolUpdateBelongsToCurrentRun(snapshot *readmodel.Model, update core.ToolUpdate) bool {
	if snapshot.Run() == nil {
		return true
	}
	currentRunID := strings.TrimSpace(snapshot.Run().ID)
	updateRunID := strings.TrimSpace(update.RunID)
	if currentRunID == "" {
		return true
	}
	if updateRunID != "" {
		return updateRunID == currentRunID
	}
	return toolCallMessageRunID(snapshot.Messages(), update.ToolCallID) == currentRunID
}

func toolCallMessageRunID(messages []surfacemessage.Message, toolCallID string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		for _, call := range messages[i].ToolCalls() {
			if strings.TrimSpace(call.ID) == toolCallID {
				return strings.TrimSpace(messages[i].RunID)
			}
		}
	}
	return ""
}

func modelOutputPhase(messages []surfacemessage.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role == surfacemessage.User {
			return ""
		}
		if message.Role != surfacemessage.Assistant && message.Role != surfacemessage.System {
			continue
		}
		if message.IsThinking() {
			return "Thinking"
		}
		if strings.TrimSpace(message.Content().Text) != "" && !message.IsFinished() {
			return "Writing response"
		}
		if strings.TrimSpace(message.Content().Text) != "" || len(message.ToolCalls()) > 0 || message.IsFinished() {
			return ""
		}
	}
	return ""
}

func (m *appModel) workingIdleElapsed() string {
	timing := m.state().Timing()
	if timing == nil || timing.LastEventAt.IsZero() {
		return ""
	}
	idle := m.now.Sub(timing.LastEventAt)
	if idle < 10*time.Second {
		return ""
	}
	return formatWorkingElapsed(idle)
}

// workingToolPhaseWithDetail is the working line for a running tool: its verb
// and main parameter.
func workingToolPhaseWithDetail(messages []surfacemessage.Message, update core.ToolUpdate) string {
	input := ""
	if call, ok := activeToolCall(messages, update.ToolCallID); ok {
		input = call.Input
	}
	call := toolview.Describe(update.ToolName, input)
	if call.Detail == "" {
		return call.Verb
	}
	return call.Verb + ": " + toolview.Shorten(call.Detail, 80)
}

func activeToolCall(messages []surfacemessage.Message, toolCallID string) (surfacemessage.ToolCall, bool) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return surfacemessage.ToolCall{}, false
	}
	for i := len(messages) - 1; i >= 0; i-- {
		for _, call := range messages[i].ToolCalls() {
			if strings.TrimSpace(call.ID) == toolCallID {
				return call, true
			}
		}
	}
	return surfacemessage.ToolCall{}, false
}

func isSubagentToolName(name string) bool {
	return strings.ToLower(strings.TrimSpace(name)) == "agent"
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
