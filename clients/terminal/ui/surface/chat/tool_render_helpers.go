package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/stringext"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func pendingTool(sty *surfacestyles.Styles, name string, anim *anim.Anim, nested bool) string {
	toolName := toolNameStyle(sty, nested).Render(name)

	var animView string
	if anim != nil {
		animView = anim.Render()
	}
	if animView != "" {
		return fmt.Sprintf("%s %s", toolName, animView)
	}
	return toolName
}

func toolNameStyle(sty *surfacestyles.Styles, nested bool) lipgloss.Style {
	if nested {
		return sty.Tool.NameNested
	}
	return sty.Tool.NameNormal
}

func toolEarlyStateContent(sty *surfacestyles.Styles, opts *ToolRenderOpts, width int) (string, bool) {
	var msg string
	switch opts.Status {
	case ToolStatusError:
		msg = toolErrorContent(sty, opts.Result, width)
	case ToolStatusCanceled:
		msg = sty.Tool.StateCancelled.Render("Canceled.")
	case ToolStatusAwaitingPermission:
		msg = sty.Tool.StateWaiting.Render("Requesting permission...")
	case ToolStatusRunning:
		msg = sty.Tool.StateWaiting.Render("Waiting for tool response...")
	default:
		return "", false
	}
	return msg, true
}

func toolErrorContent(sty *surfacestyles.Styles, result *surfacemessage.ToolResult, width int) string {
	if result == nil {
		return ""
	}
	errContent := strings.ReplaceAll(result.Content, "\n", " ")
	errTag := sty.Tool.ErrorTag.Render("ERROR")
	tagWidth := lipgloss.Width(errTag)
	errContent = ansi.Truncate(errContent, width-tagWidth-3, "…")
	return fmt.Sprintf("%s %s", errTag, sty.Tool.ErrorMessage.Render(errContent))
}

func toolIcon(sty *surfacestyles.Styles, status ToolStatus) string {
	switch status {
	case ToolStatusSuccess:
		return sty.Tool.IconSuccess.String()
	case ToolStatusError:
		return sty.Tool.IconError.String()
	case ToolStatusCanceled:
		return sty.Tool.IconCancelled.String()
	default:
		return sty.Tool.IconPending.String()
	}
}

func toolStatusMarkerStyle(sty *surfacestyles.Styles, status ToolStatus) lipgloss.Style {
	switch status {
	case ToolStatusSuccess:
		return sty.Tool.IconSuccess.UnsetString()
	case ToolStatusError:
		return sty.Tool.IconError.UnsetString()
	case ToolStatusCanceled:
		return sty.Tool.IconCancelled.UnsetString()
	default:
		return sty.Tool.IconPending.UnsetString()
	}
}

func toolParamList(sty *surfacestyles.Styles, params []string, width int) string {
	const minSpaceForMainParam = 30
	if len(params) == 0 {
		return ""
	}
	mainParam := params[0]
	var kvPairs []string
	for i := 1; i+1 < len(params); i += 2 {
		if params[i+1] != "" {
			kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", params[i], params[i+1]))
		}
	}
	output := mainParam
	if len(kvPairs) > 0 {
		partsStr := strings.Join(kvPairs, ", ")
		if remaining := width - lipgloss.Width(partsStr) - 3; remaining >= minSpaceForMainParam {
			output = fmt.Sprintf("%s (%s)", mainParam, partsStr)
		}
	}
	if width >= 0 {
		output = ansi.Truncate(output, width, "…")
	}
	return sty.Tool.ParamMain.Render(output)
}

func toolHeader(sty *surfacestyles.Styles, _ ToolStatus, name string, width int, nested bool, params ...string) string {
	toolName := toolNameStyle(sty, nested).Render(name)
	prefix := fmt.Sprintf("%s ", toolName)
	prefixWidth := lipgloss.Width(prefix)
	remainingWidth := width - prefixWidth
	paramsStr := toolParamList(sty, params, remainingWidth)
	return prefix + paramsStr
}

func toolDiffSummaryHeader(sty *surfacestyles.Styles, _ ToolStatus, name, file string, additions, removals int, hint string, width int, nested bool) string {
	toolName := toolNameStyle(sty, nested).Render(name)
	path := sty.Tool.ParamMain.Render(file)
	diff := fmt.Sprintf(
		"(%s %s)",
		sty.Files.Additions.Render(fmt.Sprintf("+%d", additions)),
		sty.Files.Deletions.Render(fmt.Sprintf("-%d", removals)),
	)
	parts := []string{toolName, path, diff}
	if trimmed := strings.TrimSpace(hint); trimmed != "" {
		parts = append(parts, sty.Muted.Render(trimmed))
	}

	line := strings.Join(parts, " ")
	if width >= 0 {
		line = ansi.Truncate(line, width, "…")
	}
	return line
}

func toolOutputPlainContent(sty *surfacestyles.Styles, content string, width int, expanded bool) string {
	content = stringext.NormalizeSpace(content)
	lines := strings.Split(content, "\n")
	maxLines := responseContextHeight
	if expanded {
		maxLines = len(lines)
	}

	var out []string
	for i, ln := range lines {
		if i >= maxLines {
			break
		}
		ln = " " + ln
		if lipgloss.Width(ln) > width {
			ln = ansi.Truncate(ln, width, "…")
		}
		out = append(out, sty.Tool.ContentLine.Width(width).Render(ln))
	}

	wasTruncated := len(lines) > responseContextHeight
	if !expanded && wasTruncated {
		out = append(out, sty.Tool.ContentTruncation.Width(width).Render(
			fmt.Sprintf(assistantMessageTruncateFormat, len(lines)-responseContextHeight),
		))
	}
	return strings.Join(out, "\n")
}

func toolOutputCodeContent(sty *surfacestyles.Styles, path, content string, offset, width int, expanded bool) string {
	content = stringext.NormalizeSpace(content)
	lines := strings.Split(content, "\n")
	maxLines := responseContextHeight
	if expanded {
		maxLines = len(lines)
	}
	displayLines := lines
	if len(lines) > maxLines {
		displayLines = lines[:maxLines]
	}

	bg := sty.Tool.ContentCodeBg
	highlighted, _ := common.SyntaxHighlight(sty, strings.Join(displayLines, "\n"), path, bg)
	highlightedLines := strings.Split(highlighted, "\n")
	maxLineNumber := len(displayLines) + offset
	maxDigits := getDigits(maxLineNumber)
	numFmt := fmt.Sprintf("%%%dd", maxDigits)

	bodyWidth := width - toolBodyLeftPaddingTotal
	codeWidth := bodyWidth - maxDigits

	var out []string
	for i, ln := range highlightedLines {
		lineNum := sty.LineNumber.Render(fmt.Sprintf(numFmt, i+1+offset))
		ln = ansi.Truncate(ln, codeWidth-sty.Tool.ContentCodeLine.GetHorizontalPadding(), "…")
		codeLine := sty.Tool.ContentCodeLine.Width(codeWidth).Render(ln)
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Left, lineNum, codeLine))
	}

	if len(lines) > maxLines && !expanded {
		out = append(out, sty.Tool.ContentCodeTruncation.Width(width).Render(
			fmt.Sprintf(assistantMessageTruncateFormat, len(lines)-maxLines),
		))
	}
	return sty.Tool.Body.Render(strings.Join(out, "\n"))
}

func toolOutputImageContent(sty *surfacestyles.Styles, data, mediaType string) string {
	dataSize := len(data) * 3 / 4
	sizeStr := formatSize(dataSize)
	return sty.Tool.Body.Render(fmt.Sprintf(
		"%s %s %s %s",
		sty.Tool.ResourceLoadedText.Render("Loaded Image"),
		sty.Tool.ResourceLoadedIndicator.Render(surfacestyles.ArrowRightIcon),
		sty.Tool.MediaType.Render(mediaType),
		sty.Tool.ResourceSize.Render(sizeStr),
	))
}

func toolResultImageContent(sty *surfacestyles.Styles, result *surfacemessage.ToolResult) (string, bool) {
	data, mediaType, ok := toolResultImagePayload(result)
	if !ok {
		return "", false
	}
	return toolOutputImageContent(sty, data, mediaType), true
}

func toolResultImagePayload(result *surfacemessage.ToolResult) (string, string, bool) {
	if result == nil {
		return "", "", false
	}
	if data, mediaType, ok := toolResultImagePayloadFromJSON(result.Metadata); ok {
		return data, mediaType, true
	}
	if data, mediaType, ok := toolResultImagePayloadFromJSON(result.Content); ok {
		return data, mediaType, true
	}
	mediaType := strings.TrimSpace(result.MIMEType)
	data := strings.TrimSpace(result.Content)
	if strings.HasPrefix(mediaType, "image/") && data != "" {
		return data, mediaType, true
	}
	return "", "", false
}

func toolResultImagePayloadFromJSON(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return "", "", false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", "", false
	}
	mediaType := firstStringField(payload, "mime_type", "media_type", "mimeType", "mediaType", "type")
	data := firstStringField(payload, "content_base64", "data", "base64", "content")
	if strings.HasPrefix(mediaType, "image/") && data != "" {
		return data, mediaType, true
	}
	return "", "", false
}

func firstStringField(payload map[string]any, names ...string) string {
	for _, name := range names {
		value, ok := payload[name]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}

func getDigits(n int) int {
	if n == 0 {
		return 1
	}
	if n < 0 {
		n = -n
	}
	digits := 0
	for n > 0 {
		n /= 10
		digits++
	}
	return digits
}

func formatSize(bytes int) string {
	const (
		kb = 1024
		mb = kb * 1024
	)
	switch {
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
