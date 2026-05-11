package chat

import (
	"encoding/json"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type GlobToolMessageItem struct{ *baseToolMessageItem }
type GrepToolMessageItem struct{ *baseToolMessageItem }
type LSToolMessageItem struct{ *baseToolMessageItem }

func NewGlobToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &GlobToolRenderContext{}, canceled)
}

func NewGrepToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &GrepToolRenderContext{}, canceled)
}

func NewLSToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &LSToolRenderContext{}, canceled)
}

type GlobToolRenderContext struct{}
type GrepToolRenderContext struct{}
type LSToolRenderContext struct{}

func (g *GlobToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Glob", opts.Anim, opts.Compact)
	}

	var params tools.GlobParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	toolParams := []string{params.Pattern}
	if params.Path != "" {
		toolParams = append(toolParams, "path", prettyPath(params.Path))
	}

	header := toolHeader(sty, opts.Status, "Glob", cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() || opts.Result.Content == "" {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

func (g *GrepToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Grep", opts.Anim, opts.Compact)
	}

	var params tools.GrepParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	toolParams := []string{params.Pattern}
	if params.Path != "" {
		toolParams = append(toolParams, "path", prettyPath(params.Path))
	}
	if params.Include != "" {
		toolParams = append(toolParams, "include", params.Include)
	}
	if params.LiteralText {
		toolParams = append(toolParams, "literal", "true")
	}

	header := toolHeader(sty, opts.Status, "Grep", cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if opts.HasEmptyResult() {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}

func (l *LSToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "List", opts.Anim, opts.Compact)
	}

	var params tools.LSParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	header := toolHeader(sty, opts.Status, "List", cappedWidth, opts.Compact, prettyPath(path))
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if opts.HasEmptyResult() {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}
