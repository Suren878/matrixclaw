package chat

import (
	"encoding/json"
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/stringext"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

type agentRenderParams struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Runtime     string `json:"runtime"`
}

func renderAgent(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	params := parseAgentParams(opts.ToolCall.Input)
	return renderSubagentTool(sty, cappedWidth, opts, params)
}

func parseAgentParams(input string) agentRenderParams {
	var params agentRenderParams
	_ = json.Unmarshal([]byte(input), &params)
	return params
}

type subagentTaskMetadata struct {
	AgentName   string `json:"agent_name"`
	DisplayName string `json:"display_name"`
	Goal        string `json:"goal"`
	Runtime     string `json:"runtime"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	Error       string `json:"error"`
}

func renderSubagentTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts, params agentRenderParams) string {
	metadata := parseSubagentTaskMetadata(opts.Result)
	agentName := strings.Join(strings.Fields(firstNonEmptyLocal(metadata.AgentName, metadata.DisplayName, params.Description, agentRuntimeLabel(params.Runtime))), " ")
	taskLabel := strings.Join(strings.Fields(firstNonEmptyLocal(metadata.DisplayName, params.Description)), " ")
	goal := strings.Join(strings.Fields(firstNonEmptyLocal(metadata.Goal, params.Prompt)), " ")
	status := subagentRenderStatus(metadata.Status, opts)
	header := toolHeader(sty, subagentRenderLabel(agentName, status), width, subagentTaskPreview(taskLabel, goal))
	bodyText := subagentBodyText(opts, metadata, taskLabel, goal, status)
	if bodyText == "" {
		return header
	}
	bodyWidth := width - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, bodyText, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

func subagentRenderStatus(metadataStatus string, opts *ToolRenderOpts) string {
	status := strings.ToLower(strings.TrimSpace(metadataStatus))
	if status != "" {
		return status
	}
	if opts != nil && opts.IsPending() {
		return "running"
	}
	if opts == nil {
		return ""
	}
	switch opts.Status {
	case ToolStatusError:
		return "failed"
	case ToolStatusCanceled:
		return "canceled"
	case ToolStatusAwaitingPermission:
		return "waiting_approval"
	case ToolStatusSuccess:
		return "completed"
	default:
		return "running"
	}
}

func subagentRenderLabel(agentName string, status string) string {
	if strings.TrimSpace(agentName) == "" {
		agentName = "subagent"
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "✓ " + agentName + " completed"
	case "failed":
		return "✕ " + agentName + " failed"
	case "canceled":
		return "✕ " + agentName + " canceled"
	case "waiting_approval":
		return "◇ " + agentName + " waiting for permission..."
	case "pending":
		return "◇ " + agentName + " starting..."
	default:
		return "◇ " + agentName + " working..."
	}
}

func subagentTaskPreview(taskLabel string, goal string) string {
	taskLabel = strings.Join(strings.Fields(taskLabel), " ")
	goal = strings.Join(strings.Fields(goal), " ")
	switch {
	case taskLabel != "" && goal != "" && !strings.EqualFold(taskLabel, goal):
		return taskLabel + " - " + goal
	case taskLabel != "":
		return taskLabel
	default:
		return goal
	}
}

func subagentBodyText(opts *ToolRenderOpts, metadata subagentTaskMetadata, taskLabel string, goal string, status string) string {
	lines := make([]string, 0, 5)
	if opts != nil && opts.ExpandedContent {
		if taskLabel != "" {
			lines = append(lines, "Task: "+taskLabel)
		}
		if goal != "" && !strings.EqualFold(goal, taskLabel) {
			lines = append(lines, "Goal: "+goal)
		}
	}
	detail := firstNonEmptyLocal(metadata.Summary, metadata.Error)
	if detail == "" && opts != nil && opts.HasResult() && subagentTerminalRenderStatus(status) {
		detail = strings.TrimSpace(opts.Result.Content)
	}
	if detail != "" {
		lines = append(lines, detail)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func subagentTerminalRenderStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func subagentToolStatusFromResult(result *surfacemessage.ToolResult) (ToolStatus, bool) {
	metadata := parseSubagentTaskMetadata(result)
	switch strings.ToLower(strings.TrimSpace(metadata.Status)) {
	case "pending", "running":
		return ToolStatusRunning, true
	case "waiting_approval":
		return ToolStatusAwaitingPermission, true
	case "completed":
		return ToolStatusSuccess, true
	case "failed":
		return ToolStatusError, true
	case "canceled":
		return ToolStatusCanceled, true
	default:
		return ToolStatusRunning, false
	}
}

func isSubagentToolNameLocal(name string) bool {
	return strings.ToLower(strings.TrimSpace(name)) == "agent"
}

func parseSubagentTaskMetadata(result *surfacemessage.ToolResult) subagentTaskMetadata {
	if result == nil || strings.TrimSpace(result.Metadata) == "" {
		return subagentTaskMetadata{}
	}
	var metadata subagentTaskMetadata
	_ = json.Unmarshal([]byte(result.Metadata), &metadata)
	return metadata
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func agentRuntimeLabel(runtime string) string {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "", "matrixclaw", "auto":
		return "MatrixClaw"
	case "codex", "codex-app", "openai-codex":
		return "Codex"
	case "claude", "claude-code", "claudecode":
		return "Claude Code"
	default:
		runtime = strings.ReplaceAll(runtime, "_", " ")
		runtime = strings.ReplaceAll(runtime, "-", " ")
		return stringext.Capitalize(runtime)
	}
}
