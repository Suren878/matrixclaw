package chat

import (
	"encoding/json"
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/stringext"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

type DelegateTaskToolMessageItem struct{ *baseToolMessageItem }

func NewDelegateTaskToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &DelegateTaskToolRenderContext{}, canceled)
}

type DelegateTaskToolRenderContext struct{}

type delegateTaskRenderParams struct {
	Goal    string `json:"goal"`
	Runtime string `json:"runtime"`
}

func (d *DelegateTaskToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	params := parseDelegateTaskParams(opts.ToolCall.Input)
	runtime := delegateRuntimeLabel(params.Runtime)
	if opts.IsPending() {
		return pendingTool(sty, "Subagent "+runtime+" is working", opts.Anim, opts.Compact)
	}

	headerParams := []string{}
	if goal := strings.Join(strings.Fields(params.Goal), " "); goal != "" {
		headerParams = append(headerParams, goal)
	}
	header := toolHeader(sty, opts.Status, "Subagent "+runtime, cappedWidth, opts.Compact, headerParams...)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() || strings.TrimSpace(opts.Result.Content) == "" {
		return header
	}
	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

func parseDelegateTaskParams(input string) delegateTaskRenderParams {
	var params delegateTaskRenderParams
	_ = json.Unmarshal([]byte(input), &params)
	return params
}

func delegateRuntimeLabel(runtime string) string {
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
