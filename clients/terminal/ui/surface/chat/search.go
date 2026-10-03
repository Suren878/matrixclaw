package chat

import (
	"encoding/json"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/toolview"
)

// renderListing draws glob, grep and ls: the toolview header and the listing
// as plain lines.
func renderListing(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	view := toolview.Describe(opts.ToolCall.Name, opts.ToolCall.Input)
	if opts.IsPending() {
		return pendingTool(sty, view.Title, opts.Anim)
	}
	if !json.Valid([]byte(opts.ToolCall.Input)) {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}
	header := toolHeader(sty, view.Title, cappedWidth, headerParams(view, nil)...)
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if opts.HasEmptyResult() {
		return header
	}
	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	return joinToolParts(header, sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent)))
}
