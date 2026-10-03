package chat

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/toolview"
)

var numberedReadLinePrefix = regexp.MustCompile(`^\s*\d+\s`)

type filesystemPathMetadata struct {
	FilePath      string `json:"file_path"`
	RequestedPath string `json:"requested_path"`
	ResolvedPath  string `json:"resolved_path"`
	WorkingDir    string `json:"working_dir"`
}

func metadataDisplayPath(raw string, fallback string) string {
	var meta filesystemPathMetadata
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return prettyPath(fallback)
	}
	switch {
	case strings.TrimSpace(meta.ResolvedPath) != "":
		return prettyPath(meta.ResolvedPath)
	case strings.TrimSpace(meta.FilePath) != "":
		return prettyPath(meta.FilePath)
	default:
		return prettyPath(fallback)
	}
}

func resultDisplayPath(result *surfacemessage.ToolResult, fallback string) string {
	if result == nil {
		return prettyPath(fallback)
	}
	return metadataDisplayPath(result.Metadata, fallback)
}

func renderRead(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return toolHeader(sty, "Read", cappedWidth, "")
	}

	var params tools.ReadParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	file := resultDisplayPath(opts.Result, params.FilePath)
	toolParams := []string{file}
	if params.Offset != 0 {
		toolParams = append(toolParams, "offset", fmt.Sprintf("%d", params.Offset))
	}

	header := toolHeader(sty, "Read", cappedWidth, toolParams...)
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

// renderFileChange draws write, edit and multiedit: the file and, once done,
// its line delta and the hint to open the diff.
func renderFileChange(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	width = cappedMessageWidth(width)
	title := toolview.Describe(opts.ToolCall.Name, "").Title
	if opts.IsPending() {
		return pendingTool(sty, title, opts.Anim)
	}
	metadata := ""
	if opts.Result != nil {
		metadata = opts.Result.Metadata
	}
	change, ok := toolview.FileChangeOf(opts.ToolCall.Name, opts.ToolCall.Input, metadata)
	if !ok {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, width)
	}

	file := resultDisplayPath(opts.Result, change.Path)
	params := []string{file}
	if change.Edits > 0 {
		params = append(params, "edits", strconv.Itoa(change.Edits))
	}
	header := toolHeader(sty, title, width, params...)
	if earlyState, ok := toolEarlyStateContent(sty, opts, width); ok {
		return joinToolParts(header, earlyState)
	}
	if !change.Done {
		return header
	}
	hint := "press enter for diff"
	switch {
	case change.Edits > 0:
		hint = fmt.Sprintf("%d edits, press enter for diff", change.Edits)
	}
	return toolDiffSummaryHeader(sty, title, file, change.Additions, change.Removals, hint, width)
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

func resolveReadResult(path string, result surfacemessage.ToolResult) (string, string) {
	content := strings.TrimSpace(result.Content)
	path = metadataDisplayPath(result.Metadata, path)

	var meta tools.ReadResponseMetadata
	if err := json.Unmarshal([]byte(result.Metadata), &meta); err == nil {
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
