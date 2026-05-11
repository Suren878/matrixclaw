package chat

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
)

var numberedReadLinePrefix = regexp.MustCompile(`^\s*\d+\s`)

type ReadToolMessageItem struct{ *baseToolMessageItem }
type WriteToolMessageItem struct{ *baseToolMessageItem }
type EditToolMessageItem struct{ *baseToolMessageItem }
type MultiEditToolMessageItem struct{ *baseToolMessageItem }

func NewReadToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &ReadToolRenderContext{}, canceled)
}

func NewWriteToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &WriteToolRenderContext{}, canceled)
}

func NewEditToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &EditToolRenderContext{}, canceled)
}

func NewMultiEditToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &MultiEditToolRenderContext{}, canceled)
}

type ReadToolRenderContext struct{}
type WriteToolRenderContext struct{}
type EditToolRenderContext struct{}
type MultiEditToolRenderContext struct{}

func (v *ReadToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return readHeader(sty, ToolStatusRunning, cappedWidth, opts.Compact, "")
	}

	var params tools.ReadParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	file := prettyPath(params.FilePath)
	toolParams := []string{file}
	if params.Offset != 0 {
		toolParams = append(toolParams, "offset", fmt.Sprintf("%d", params.Offset))
	}

	header := readHeader(sty, opts.Status, cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() {
		return header
	}

	if body, ok := toolResultImageContent(sty, opts.Result); ok {
		return joinToolParts(header, body)
	}
	return header
}

func (w *WriteToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Write", opts.Anim, opts.Compact)
	}

	var params tools.WriteParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	file := prettyPath(params.FilePath)
	header := toolHeader(sty, opts.Status, "Write", cappedWidth, opts.Compact, file)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() || params.Content == "" {
		return header
	}

	var meta tools.WriteResponseMetadata
	if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err != nil {
		return header
	}

	return toolDiffSummaryHeader(sty, opts.Status, "Write", file, meta.Additions, meta.Removals, "press enter for diff", cappedWidth, opts.Compact)
}

func (e *EditToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Edit", opts.Anim, opts.Compact)
	}

	var params tools.EditParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	file := prettyPath(params.FilePath)
	header := toolHeader(sty, opts.Status, "Edit", cappedWidth, opts.Compact, file)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() {
		return header
	}

	var meta tools.EditResponseMetadata
	if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err != nil {
		return header
	}

	return toolDiffSummaryHeader(sty, opts.Status, "Edit", file, meta.Additions, meta.Removals, "press enter for diff", cappedWidth, opts.Compact)
}

func (m *MultiEditToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	if opts.IsPending() {
		return pendingTool(sty, "Multi-Edit", opts.Anim, opts.Compact)
	}

	var params tools.MultiEditParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, width)
	}

	file := prettyPath(params.FilePath)
	toolParams := []string{file}
	if len(params.Edits) > 0 {
		toolParams = append(toolParams, "edits", fmt.Sprintf("%d", len(params.Edits)))
	}

	header := toolHeader(sty, opts.Status, "Multi-Edit", width, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, width); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() {
		return header
	}

	var meta tools.MultiEditResponseMetadata
	if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err != nil {
		return header
	}

	editCount := len(params.Edits)
	hint := "press enter for diff"
	if editCount > 0 {
		hint = fmt.Sprintf("%d edits, press enter for diff", editCount)
	}
	if len(meta.EditsFailed) > 0 {
		hint = fmt.Sprintf("%d/%d edits applied, press enter for diff", meta.EditsApplied, len(params.Edits))
	}
	return toolDiffSummaryHeader(sty, opts.Status, "Multi-Edit", file, meta.Additions, meta.Removals, hint, width, opts.Compact)
}

func prettyPath(path string) string {
	return strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
}

func renderReadPathsBlock(sty *surfacestyles.Styles, width int, paths ...string) string {
	bodyWidth := width - toolBodyLeftPaddingTotal
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if path = strings.TrimSpace(path); path != "" {
			filtered = append(filtered, path)
		}
	}
	if len(filtered) == 0 {
		return ""
	}

	lines := make([]string, 0, len(filtered))
	if len(filtered) == 1 {
		lines = append(lines, filtered[0])
	} else {
		lines = strings.Split(surfacecommon.RenderPathTree(filtered...), "\n")
	}

	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = ansi.Truncate(line, bodyWidth, "…")
		rendered = append(rendered, sty.Tool.ContentLine.Render(line))
	}
	return strings.Join(rendered, "\n")
}

func readHeader(sty *surfacestyles.Styles, _ ToolStatus, width int, nested bool, params ...string) string {
	nameStyle := sty.Tool.NameNormal
	if nested {
		nameStyle = sty.Tool.NameNested
	}
	prefix := nameStyle.Render("Read") + " "
	remainingWidth := width - lipgloss.Width(prefix)
	return prefix + toolParamList(sty, params, remainingWidth)
}

func resolveReadResult(path string, result surfacemessage.ToolResult) (string, string) {
	content := strings.TrimSpace(result.Content)

	var meta tools.ReadResponseMetadata
	if err := json.Unmarshal([]byte(result.Metadata), &meta); err == nil {
		if strings.TrimSpace(meta.FilePath) != "" {
			path = prettyPath(meta.FilePath)
		}
		if strings.TrimSpace(meta.Content) != "" {
			content = meta.Content
		}
	}

	if wrapped := unwrapTaggedFileContent(content); wrapped != "" {
		content = wrapped
	}

	return path, content
}

func unwrapTaggedFileContent(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "<file>") || !strings.HasSuffix(content, "</file>") {
		return content
	}

	content = strings.TrimPrefix(content, "<file>")
	content = strings.TrimSuffix(content, "</file>")
	content = strings.TrimSpace(content)
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		lines[i] = numberedReadLinePrefix.ReplaceAllString(line, "")
	}

	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "(File has more lines.") {
		lines = lines[:len(lines)-1]
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func commonReadRoot(paths []string) string {
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if path = prettyPath(path); path != "" {
			filtered = append(filtered, path)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) == 1 {
		return filtered[0]
	}

	parts := strings.Split(filtered[0], "/")
	last := len(parts)
	for _, path := range filtered[1:] {
		cur := strings.Split(path, "/")
		i := 0
		for i < last && i < len(cur) && parts[i] == cur[i] {
			i++
		}
		last = i
		if last == 0 {
			return ""
		}
	}
	if last <= 0 {
		return ""
	}
	return strings.Join(parts[:last], "/")
}

func relativeReadPaths(root string, paths []string) []string {
	root = strings.TrimSuffix(prettyPath(root), "/")
	if root == "" {
		return append([]string(nil), paths...)
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = prettyPath(path)
		rel := strings.TrimPrefix(path, root)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			rel = path
		}
		out = append(out, rel)
	}
	return out
}
