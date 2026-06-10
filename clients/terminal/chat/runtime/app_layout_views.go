package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
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
	if m.width <= 0 {
		return ""
	}
	if !m.busy {
		return m.waitingSubagentsStatusView()
	}
	run := m.currentRun()
	if !runIsActive(run) {
		return m.workingStatusLine(m.currentSnapshot(), nil, "Waiting for model", "")
	}
	snapshot := m.currentSnapshot()
	return m.workingStatusLine(snapshot, run, m.workingStatusPhase(), m.workingIdleElapsed())
}

func (m *appModel) workingStatusLine(snapshot viewmodel.Snapshot, run *core.Run, phase string, idle string) string {
	model := m.currentModelLabel()
	if model == "" {
		model = "model"
	}
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
	if detail := pendingInputsStatusText(snapshot.PendingInputs); detail != "" {
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
	line := lipgloss.NewStyle().Foreground(lipgloss.Color(colorToHex(m.styles.Primary))).Render("[" + model + "] " + spinner + " " + phase + " (" + timing + " • esc to cancel" + detail + ")")
	return line
}

func (m *appModel) waitingSubagentsStatusView() string {
	snapshot := m.currentSnapshot()
	line := combinedWaitingStatusText(snapshot.PendingInputs, snapshot.Subagents)
	if line == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colorToHex(m.styles.Primary))).Render(line)
}

func combinedWaitingStatusText(inputs []core.SessionInput, tasks []core.SubagentTask) string {
	parts := make([]string, 0, 2)
	if text := pendingInputsStatusText(inputs); text != "" {
		parts = append(parts, text)
	}
	if text := activeSubagentsStatusText(tasks); text != "" {
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

func activeSubagentsStatusText(tasks []core.SubagentTask) string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if !subagentTaskActive(task) {
			continue
		}
		if name := subagentTaskDisplayName(task); name != "" {
			names = append(names, name)
		}
	}
	return activeSubagentNamesStatusText(names)
}

func activeSubagentsStatusTextForSnapshot(snapshot viewmodel.Snapshot, phase string) string {
	if strings.HasPrefix(strings.TrimSpace(phase), "Waiting for subagent:") {
		return ""
	}
	currentRunID := ""
	if snapshot.Run != nil {
		currentRunID = strings.TrimSpace(snapshot.Run.ID)
	}
	names := make([]string, 0, len(snapshot.Subagents))
	for _, task := range snapshot.Subagents {
		if !subagentTaskActive(task) {
			continue
		}
		if task.Mode == core.SubagentTaskModeBlocking && currentRunID != "" && strings.TrimSpace(task.ParentRunID) != currentRunID {
			continue
		}
		if name := subagentTaskDisplayName(task); name != "" {
			names = append(names, name)
		}
	}
	return activeSubagentNamesStatusText(names)
}

func activeSubagentNamesStatusText(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "Subagents: " + strings.Join(names, ", ")
}

func subagentTaskActive(task core.SubagentTask) bool {
	switch task.Status {
	case core.SubagentTaskStatusPending, core.SubagentTaskStatusRunning, core.SubagentTaskStatusWaitingApproval:
		return true
	default:
		return false
	}
}

func (m *appModel) workingStatusPhase() string {
	if m.read == nil {
		return "Waiting for model"
	}
	snapshot := m.currentSnapshot()
	if activeRunWaitingForPermission(snapshot) {
		return "Waiting for permission"
	}
	if update, ok := latestActiveToolUpdate(snapshot, core.ToolLifecycleRequested); ok {
		if isSubagentToolName(update.ToolName) {
			if phase := activeSubagentPhase(snapshot); phase != "" {
				return phase
			}
		}
		return workingToolPhaseWithDetail(snapshot.Messages, update)
	}
	if snapshot.Run != nil && snapshot.Run.Status == core.RunStatusAccepted {
		return "Waiting for model"
	}
	if phase := modelOutputPhase(snapshot.Messages); phase != "" {
		return phase
	}
	if phase := activeSubagentPhase(snapshot); phase != "" {
		return phase
	}
	return "Waiting for model"
}

func activeRunWaitingForPermission(snapshot viewmodel.Snapshot) bool {
	if update, ok := latestActiveToolUpdate(snapshot, core.ToolLifecycleWaitingApproval); ok && strings.TrimSpace(update.ToolCallID) != "" {
		return true
	}
	return snapshot.Run != nil && snapshot.Run.Status == core.RunStatusWaitingApproval && len(snapshot.Approvals) > 0
}

func activeSubagentPhase(snapshot viewmodel.Snapshot) string {
	if snapshot.Run == nil {
		return ""
	}
	runID := strings.TrimSpace(snapshot.Run.ID)
	if runID == "" {
		return ""
	}
	for i := len(snapshot.Subagents) - 1; i >= 0; i-- {
		task := snapshot.Subagents[i]
		if task.Mode != core.SubagentTaskModeBlocking {
			continue
		}
		if strings.TrimSpace(task.ParentRunID) != runID {
			continue
		}
		name := subagentTaskDisplayName(task)
		switch task.Status {
		case core.SubagentTaskStatusPending:
			return "Starting subagent: " + name
		case core.SubagentTaskStatusRunning:
			return "Waiting for subagent: " + name
		case core.SubagentTaskStatusWaitingApproval:
			return "Subagent waiting for permission: " + name
		}
	}
	return ""
}

func subagentTaskDisplayName(task core.SubagentTask) string {
	if name := strings.Join(strings.Fields(task.AgentName), " "); name != "" {
		return name
	}
	if name := strings.Join(strings.Fields(task.DisplayName), " "); name != "" {
		return name
	}
	if runtime := strings.TrimSpace(task.Runtime); runtime != "" {
		return runtime
	}
	if id := strings.TrimSpace(task.ID); id != "" {
		return id
	}
	return "subagent"
}

func latestActiveToolUpdate(snapshot viewmodel.Snapshot, state core.ToolLifecycleState) (core.ToolUpdate, bool) {
	for i := len(snapshot.ToolUpdates) - 1; i >= 0; i-- {
		update := snapshot.ToolUpdates[i]
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

func toolUpdateBelongsToCurrentRun(snapshot viewmodel.Snapshot, update core.ToolUpdate) bool {
	if snapshot.Run == nil {
		return true
	}
	currentRunID := strings.TrimSpace(snapshot.Run.ID)
	updateRunID := strings.TrimSpace(update.RunID)
	if currentRunID == "" {
		return true
	}
	if updateRunID != "" {
		return updateRunID == currentRunID
	}
	return toolCallMessageRunID(snapshot.Messages, update.ToolCallID) == currentRunID
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
	if m.read == nil {
		return ""
	}
	timing := m.currentSnapshot().Timing
	if timing == nil || timing.LastEventAt.IsZero() {
		return ""
	}
	idle := m.now.Sub(timing.LastEventAt)
	if idle < 10*time.Second {
		return ""
	}
	return formatWorkingElapsed(idle)
}

func workingToolPhase(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash":
		return "Executing command"
	case "read":
		return "Reading file"
	case "write":
		return "Writing file"
	case "edit", "multiedit":
		return "Editing file"
	case "grep", "glob":
		return "Searching files"
	case "ls":
		return "Listing files"
	case "web_fetch":
		return "Fetching web page"
	case "web_search":
		return "Searching web"
	case "web_research":
		return "Researching web"
	case "web_research_ask":
		return "Checking research"
	case "web_research_status":
		return "Checking research status"
	case "":
		return "Running tool"
	default:
		return "Running " + strings.TrimSpace(name)
	}
}

func workingToolPhaseWithDetail(messages []surfacemessage.Message, update core.ToolUpdate) string {
	phase := workingToolPhase(update.ToolName)
	call, ok := activeToolCall(messages, update.ToolCallID)
	if !ok {
		return phase
	}
	if detail := workingToolDetail(update.ToolName, call.Input); detail != "" {
		return phase + ": " + detail
	}
	return phase
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

func workingToolDetail(toolName string, input string) string {
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch name {
	case "web_search", "session_search", "skill_search":
		return compactWorkingToolParam(params, "query")
	case "web_fetch":
		return compactWorkingToolParam(params, "url")
	case "web_research":
		return firstNonEmptyRuntime(compactWorkingToolParam(params, "query"), compactWorkingToolParam(params, "task"), compactWorkingToolParam(params, "urls"))
	case "web_research_ask":
		return compactWorkingToolParam(params, "question")
	case "web_research_status":
		return compactWorkingToolParam(params, "research_id")
	}
	if strings.HasPrefix(name, "mcp_browser_") {
		return firstNonEmptyRuntime(compactWorkingToolParam(params, "url"), compactWorkingToolParam(params, "text"), compactWorkingToolParam(params, "selector"), compactWorkingToolParam(params, "element"), compactWorkingToolParam(params, "query"), compactWorkingToolParam(params, "ref"))
	}
	return ""
}

func compactWorkingToolParam(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return compactWorkingToolText(typed)
	case []any:
		if len(typed) == 0 {
			return ""
		}
		first := compactWorkingToolText(fmt.Sprint(typed[0]))
		if first != "" && len(typed) > 1 {
			return fmt.Sprintf("%s (+%d)", first, len(typed)-1)
		}
		return first
	default:
		return compactWorkingToolText(fmt.Sprint(value))
	}
}

func compactWorkingToolText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= 80 {
		return value
	}
	return strings.TrimSpace(string(runes[:79])) + "…"
}

func isSubagentToolName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "delegate_task" || name == "spawn_subagent"
}

func delegateRuntimeLabel(input string) string {
	runtime := delegateRuntimeFromInput(input)
	switch normalizeRuntimeToken(runtime) {
	case "", "auto", "matrixclaw", "matrix_claw", "matrix-claw":
		return "MatrixClaw"
	case "codex", "codex_app", "codex-app", "openai_codex", "openai-codex":
		return "Codex"
	case "claude", "claude_code", "claude-code", "claudecode":
		return "Claude Code"
	default:
		return titleCaseRuntime(runtime)
	}
}

func delegateRuntimeFromInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	var payload struct {
		Runtime string `json:"runtime"`
	}
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Runtime)
}

func normalizeRuntimeToken(runtime string) string {
	return strings.ToLower(strings.TrimSpace(runtime))
}

func titleCaseRuntime(runtime string) string {
	runtime = strings.TrimSpace(runtime)
	if runtime == "" {
		return "MatrixClaw"
	}
	parts := strings.FieldsFunc(runtime, func(r rune) bool {
		switch r {
		case '-', '_', ' ', '.', '/':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return "MatrixClaw"
	}
	for i, part := range parts {
		parts[i] = titleCaseWord(part)
	}
	return strings.Join(parts, " ")
}

func titleCaseWord(word string) string {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return ""
	}
	if len(word) == 1 {
		return strings.ToUpper(word)
	}
	return strings.ToUpper(word[:1]) + word[1:]
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
