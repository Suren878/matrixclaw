package chat

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type BashToolMessageItem struct{ *baseToolMessageItem }

func NewBashToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &BashToolRenderContext{}, canceled)
}

type BashToolRenderContext struct{}

func (b *BashToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Run", opts.Anim, opts.Compact)
	}

	var params tools.BashParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		params.Command = "failed to parse command"
	}

	var meta tools.BashResponseMetadata
	if opts.HasResult() {
		_ = json.Unmarshal([]byte(opts.Result.Metadata), &meta)
	}

	if meta.Background {
		description := cmp.Or(meta.Description, params.Command)
		content := "Command: " + params.Command + "\n" + opts.Result.Content
		return renderTaskTool(sty, opts, cappedWidth, "Start", meta.TaskID, description, content)
	}

	cmd := strings.ReplaceAll(params.Command, "\n", " ")
	cmd = strings.ReplaceAll(cmd, "\t", "    ")
	toolParams := []string{cmd}
	if params.RunInBackground {
		toolParams = append(toolParams, "background", "true")
	}

	header := runHeader(sty, opts.Status, cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}
	if earlyState, ok := runEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() {
		return header
	}

	output := meta.Output
	if output == "" && opts.Result.Content != "no output" {
		output = opts.Result.Content
	}
	if output == "" {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, output, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

func runEarlyStateContent(sty *surfacestyles.Styles, opts *ToolRenderOpts, width int) (string, bool) {
	switch opts.Status {
	case ToolStatusError:
		if opts.Result == nil || strings.TrimSpace(opts.Result.Content) == "" {
			return "", false
		}
		return sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, width-toolBodyLeftPaddingTotal, opts.ExpandedContent)), true
	default:
		return toolEarlyStateContent(sty, opts, width)
	}
}

func runHeader(sty *surfacestyles.Styles, status ToolStatus, width int, nested bool, params ...string) string {
	return toolHeader(sty, status, "Run", width, nested, params...)
}

func isExpectedNeutralBashResult(toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult) bool {
	if result == nil || normalizedToolName(toolCall.Name) != "bash" {
		return false
	}
	var params tools.BashParams
	_ = json.Unmarshal([]byte(toolCall.Input), &params)
	var meta tools.BashResponseMetadata
	_ = json.Unmarshal([]byte(result.Metadata), &meta)
	return meta.ExitCode == 1 && strings.TrimSpace(meta.Output) == "" && tools.IsProcessProbeCommand(params.Command)
}

type TaskOutputToolMessageItem struct{ *baseToolMessageItem }

type TaskKillToolMessageItem struct{ *baseToolMessageItem }

func NewTaskOutputToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &TaskOutputToolRenderContext{}, canceled)
}

func NewTaskKillToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &TaskKillToolRenderContext{}, canceled)
}

type TaskOutputToolRenderContext struct{}

type TaskKillToolRenderContext struct{}

func (j *TaskOutputToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	return renderTaskToolCall(sty, width, opts, "Output")
}

func (j *TaskKillToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	return renderTaskToolCall(sty, width, opts, "Kill")
}

func renderTaskToolCall(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts, action string) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Task", opts.Anim, opts.Compact)
	}

	var params tools.TaskOutputParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	var description string
	if opts.HasResult() && opts.Result.Metadata != "" {
		var meta tools.TaskInfo
		if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err == nil {
			description = cmp.Or(meta.Description, meta.Command)
		}
	}

	content := ""
	if opts.HasResult() {
		content = opts.Result.Content
	}
	return renderTaskTool(sty, opts, cappedWidth, action, params.ID, description, content)
}

func renderTaskTool(sty *surfacestyles.Styles, opts *ToolRenderOpts, width int, action, taskID, description, content string) string {
	header := taskHeader(sty, opts.Status, action, taskID, description, width)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, width); ok {
		return joinToolParts(header, earlyState)
	}
	if content == "" {
		return header
	}

	bodyWidth := width - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

func taskHeader(sty *surfacestyles.Styles, status ToolStatus, action, taskID, description string, width int) string {
	icon := toolIcon(sty, status)
	taskPart := sty.Tool.JobToolName.Render("Task")
	actionPart := sty.Tool.JobAction.Render("(" + action + ")")
	idPart := sty.Tool.JobPID.Render(taskID)
	prefix := fmt.Sprintf("%s %s %s %s", icon, taskPart, actionPart, idPart)

	if description == "" {
		return prefix
	}

	prefixWidth := lipgloss.Width(prefix)
	availableWidth := width - prefixWidth - 1
	if availableWidth < 10 {
		return prefix
	}

	truncatedDesc := ansi.Truncate(description, availableWidth, "…")
	return prefix + " " + sty.Tool.JobDescription.Render(truncatedDesc)
}
