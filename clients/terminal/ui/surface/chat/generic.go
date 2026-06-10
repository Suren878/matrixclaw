package chat

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
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

	var params map[string]any
	paramsOK := true
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		paramsOK = false
	}
	toolParams := genericToolParams(opts.ToolCall.Name, params)

	if opts.IsPending() {
		if paramsOK && len(toolParams) > 0 {
			return pendingToolHeader(sty, name, cappedWidth, opts.Compact, opts.Anim, toolParams...)
		}
		return pendingTool(sty, name, opts.Anim, opts.Compact)
	}

	if !paramsOK {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

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

func pendingToolHeader(sty *surfacestyles.Styles, name string, width int, nested bool, anim *anim.Anim, params ...string) string {
	header := toolHeader(sty, ToolStatusRunning, name, width, nested, params...)
	if anim == nil {
		return header
	}
	if animView := anim.Render(); strings.TrimSpace(animView) != "" {
		return header + " " + animView
	}
	return header
}

func genericToolParams(name string, params map[string]any) []string {
	if len(params) == 0 {
		return nil
	}
	switch normalizedToolName(name) {
	case "create_reminder":
		return []string{compactScheduledToolParams(params, "text")}
	case "create_scheduled_ai_task":
		return []string{compactScheduledToolParams(params, "prompt")}
	case "web_search":
		return compactGenericToolParams(params, []string{"query"}, []string{"limit"})
	case "web_fetch":
		return compactGenericToolParams(params, []string{"url"}, []string{"task", "max_length"})
	case "web_research":
		return compactGenericToolParams(params, []string{"query", "task", "urls"}, []string{"depth", "max_sources", "browser", "freshness", "async"})
	case "web_research_ask":
		return compactGenericToolParams(params, []string{"question"}, []string{"research_id", "freshness", "browser"})
	case "web_research_status":
		return compactGenericToolParams(params, []string{"research_id"}, nil)
	case "session_search", "skill_search":
		return compactGenericToolParams(params, []string{"query"}, []string{"session_id", "limit"})
	case "skill_view", "skill_use":
		return compactGenericToolParams(params, []string{"id"}, nil)
	case "skill_manage", "memory":
		return compactGenericToolParams(params, []string{"action", "query", "id", "key", "content"}, []string{"scope", "working_dir", "limit"})
	default:
		if strings.HasPrefix(normalizedToolName(name), "mcp_browser_") {
			return compactGenericToolParams(params, []string{"url", "text", "selector", "element", "query", "name", "id"}, []string{"ref", "button", "key", "timeout", "x", "y"})
		}
		if compact := compactGenericToolParams(params, []string{"query", "url", "path", "file_path", "command", "action", "name", "id", "title", "text"}, []string{"limit", "mode", "type"}); len(compact) > 0 {
			return compact
		}
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

func compactGenericToolParams(params map[string]any, primaryKeys []string, secondaryKeys []string) []string {
	primary := firstParamValue(params, primaryKeys...)
	if primary == "" {
		return nil
	}
	out := []string{primary}
	for _, key := range secondaryKeys {
		value := paramValue(params, key)
		if value == "" {
			continue
		}
		out = append(out, key, value)
	}
	return out
}

func firstParamValue(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := paramValue(params, key); value != "" {
			return value
		}
	}
	return ""
}

func paramValue(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.Join(strings.Fields(typed), " ")
	case []any:
		return compactParamList(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return strings.Join(strings.Fields(fmt.Sprint(value)), " ")
	}
}

func compactParamList(values []any) string {
	if len(values) == 0 {
		return ""
	}
	first := strings.Join(strings.Fields(fmt.Sprint(values[0])), " ")
	if len(values) == 1 || first == "" {
		return first
	}
	return first + fmt.Sprintf(" (+%d)", len(values)-1)
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
	case "web_search":
		return "Search Web"
	case "web_fetch":
		return "Fetch Web Page"
	case "web_research":
		return "Research Web"
	case "web_research_ask":
		return "Ask Research"
	case "web_research_status":
		return "Check Research"
	case "session_search":
		return "Search Sessions"
	case "skill_search":
		return "Search Skills"
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
