package chat

import (
	"encoding/json"
	"strings"
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/stringext"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

type GenericToolMessageItem struct {
	*baseToolMessageItem
}

func NewGenericToolMessageItem(
	sty *surfacestyles.Styles,
	toolCall surfacemessage.ToolCall,
	result *surfacemessage.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &GenericToolRenderContext{}, canceled)
}

type GenericToolRenderContext struct{}

func (g *GenericToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	name := genericPrettyName(opts.ToolCall.Name)

	if opts.IsPending() {
		return pendingTool(sty, name, opts.Anim, opts.Compact)
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	toolParams := genericToolParams(opts.ToolCall.Name, params)

	header := toolHeader(sty, opts.Status, name, cappedWidth, opts.Compact, toolParams...)
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

func genericToolParams(name string, params map[string]any) []string {
	if len(params) == 0 {
		return nil
	}
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "create_reminder":
		return []string{compactScheduledToolParams(params, "text")}
	case "create_scheduled_ai_task":
		return []string{compactScheduledToolParams(params, "prompt")}
	default:
		parsed, _ := json.Marshal(params)
		return []string{string(parsed)}
	}
}

func compactScheduledToolParams(params map[string]any, textKey string) string {
	var parts []string
	if title := stringMapValue(params, "title"); title != "" {
		parts = append(parts, title)
	}
	if runAt := formatRFC3339Param(stringMapValue(params, "run_at")); runAt != "" {
		parts = append(parts, runAt)
	}
	if text := stringMapValue(params, textKey); text != "" && len(parts) == 0 {
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		parsed, _ := json.Marshal(params)
		return string(parsed)
	}
	return strings.Join(parts, " · ")
}

func stringMapValue(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.Join(strings.Fields(text), " ")
}

func formatRFC3339Param(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return parsed.Format("2006-01-02 15:04 -07:00")
}

func genericPrettyName(name string) string {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "create_reminder":
		return "⏰ Reminder"
	case "create_scheduled_ai_task":
		return "🗓 Scheduled Task"
	}
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return stringext.Capitalize(name)
}

func looksLikeMarkdown(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.Contains(trimmed, "# ") || strings.Contains(trimmed, "## ") || strings.Contains(trimmed, "```")
}

func joinToolParts(header, body string) string {
	return strings.Join([]string{header, "", body}, "\n")
}
