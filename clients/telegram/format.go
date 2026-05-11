package telegram

import (
	"html"
	"regexp"
	"strings"
)

var (
	telegramLinkPattern   = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
	telegramBoldPattern   = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	telegramItalicPattern = regexp.MustCompile(`(^|[^*])\*([^*\n]+)\*`)
)

type telegramFormattedText struct {
	Plain     string
	Text      string
	ParseMode string
}

func formatTelegramText(text string) telegramFormattedText {
	plain := clipTelegramText(text)
	if plain == "" {
		return telegramFormattedText{}
	}
	return telegramFormattedText{
		Plain:     plain,
		Text:      renderTelegramHTML(plain),
		ParseMode: defaultParseMode,
	}
}

func renderTelegramHTML(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	code := []string{}
	inCode := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				out = append(out, "<pre>"+html.EscapeString(strings.Join(code, "\n"))+"</pre>")
				code = code[:0]
			}
			inCode = !inCode
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		switch {
		case isMarkdownDivider(trimmed):
			out = append(out, "────────")
		case strings.HasPrefix(trimmed, ">"):
			out = append(out, "<blockquote>"+renderTelegramInline(strings.TrimSpace(strings.TrimPrefix(trimmed, ">")))+"</blockquote>")
		case strings.HasPrefix(trimmed, "- "):
			out = append(out, "• "+renderTelegramInline(strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))))
		default:
			out = append(out, renderTelegramInline(line))
		}
	}
	if inCode {
		out = append(out, "<pre>"+html.EscapeString(strings.Join(code, "\n"))+"</pre>")
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func renderTelegramInline(text string) string {
	var out strings.Builder
	for {
		start := strings.IndexByte(text, '`')
		if start < 0 {
			out.WriteString(renderTelegramInlineText(text))
			return out.String()
		}
		out.WriteString(renderTelegramInlineText(text[:start]))
		rest := text[start+1:]
		end := strings.IndexByte(rest, '`')
		if end < 0 {
			out.WriteString(html.EscapeString(text[start:]))
			return out.String()
		}
		out.WriteString("<code>")
		out.WriteString(html.EscapeString(rest[:end]))
		out.WriteString("</code>")
		text = rest[end+1:]
	}
}

func renderTelegramInlineText(text string) string {
	escaped := html.EscapeString(text)
	escaped = telegramLinkPattern.ReplaceAllString(escaped, `<a href="$2">$1</a>`)
	escaped = telegramBoldPattern.ReplaceAllString(escaped, `<b>$1</b>`)
	return telegramItalicPattern.ReplaceAllString(escaped, `$1<i>$2</i>`)
}

func isMarkdownDivider(line string) bool {
	if len(line) < 3 {
		return false
	}
	return strings.Trim(line, "-") == "" || strings.Trim(line, "*") == "" || strings.Trim(line, "_") == ""
}
