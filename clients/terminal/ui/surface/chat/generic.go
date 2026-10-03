package chat

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/toolview"
)

// renderGeneric draws a tool by its toolview header and its result as JSON,
// markdown or plain text.
func renderGeneric(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	view := toolview.Describe(opts.ToolCall.Name, opts.ToolCall.Input)
	name := view.Title

	var params map[string]any
	paramsOK := json.Unmarshal([]byte(opts.ToolCall.Input), &params) == nil
	toolParams := headerParams(view, params)

	if opts.IsPending() {
		if paramsOK && len(toolParams) > 0 {
			return pendingToolHeader(sty, name, cappedWidth, opts.Anim, toolParams...)
		}
		return pendingTool(sty, name, opts.Anim)
	}

	if !paramsOK {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	header := toolHeader(sty, name, cappedWidth, toolParams...)

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if !opts.HasResult() || opts.Result.Content == "" {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	if imageContent, ok := toolResultImageContent(sty, opts.Result); ok {
		body := sty.Tool.Body.Render(imageContent)
		return joinToolParts(header, body)
	}

	var result json.RawMessage
	var body string
	if err := json.Unmarshal([]byte(opts.Result.Content), &result); err == nil {
		prettyResult, err := json.MarshalIndent(result, "", "  ")
		if err == nil {
			body = sty.Tool.Body.Render(toolOutputCodeContent(sty, "result.json", string(prettyResult), 0, bodyWidth, opts.ExpandedContent))
		} else {
			body = sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
		}
	} else if looksLikeMarkdown(opts.Result.Content) {
		body = sty.Tool.Body.Render(toolOutputCodeContent(sty, "result.md", opts.Result.Content, 0, bodyWidth, opts.ExpandedContent))
	} else {
		body = sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
	}

	return joinToolParts(header, body)
}

func pendingToolHeader(sty *surfacestyles.Styles, name string, width int, anim *anim.Anim, params ...string) string {
	header := toolHeader(sty, name, width, params...)
	if anim == nil {
		return header
	}
	if animView := anim.Render(); strings.TrimSpace(animView) != "" {
		return header + " " + animView
	}
	return header
}

// headerParams is the header of a call: its main parameter, then key/value
// pairs; an unknown tool without a main parameter shows its raw input.
func headerParams(view toolview.Call, params map[string]any) []string {
	if view.Detail == "" {
		if view.Known || len(params) == 0 {
			return nil
		}
		raw, _ := json.Marshal(params)
		return []string{string(raw)}
	}
	out := []string{view.Detail}
	for _, param := range view.Params {
		out = append(out, param.Key, param.Value)
	}
	return out
}

func looksLikeMarkdown(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.Contains(trimmed, "# ") || strings.Contains(trimmed, "## ") || strings.Contains(trimmed, "```")
}

func joinToolParts(header, body string) string {
	return strings.Join([]string{header, "", body}, "\n")
}
