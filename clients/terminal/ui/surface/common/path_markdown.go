package common

import (
	"image/color"
	"regexp"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

var nonWhitespaceTokenPattern = regexp.MustCompile(`\S+`)
var bareFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var pathLocationSuffixPattern = regexp.MustCompile(`:\d+(?::\d+)?$`)

type pathTokenMatch struct {
	start int
	end   int
}

func HasMarkdownSyntax(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	markers := []string{
		"```", "`", "# ", "## ", "### ", "- ", "* ", "1. ", "> ", "|", "](",
	}
	for _, marker := range markers {
		if strings.Contains(trimmed, marker) {
			return true
		}
	}
	return false
}

func HighlightPlainPaths(content string, sty *styles.Styles) string {
	if strings.TrimSpace(content) == "" || sty == nil {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = highlightMatchedPathTokens(line, func(token string) string {
			return RenderANSIColoredText(token, fileTokenColor(sty))
		})
	}
	return strings.Join(lines, "\n")
}

func fileTokenColor(sty *styles.Styles) color.Color {
	if sty == nil {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	if sty.Blue != nil {
		return sty.Blue
	}
	if sty.BlueLight != nil {
		return sty.BlueLight
	}
	return sty.White
}

func highlightMatchedPathTokens(content string, render func(token string) string) string {
	matches := findPathTokenMatches(content)
	if len(matches) == 0 {
		return content
	}

	var b strings.Builder
	cursor := 0
	for _, match := range matches {
		if match.start < cursor || match.end > len(content) || match.start >= match.end {
			continue
		}
		b.WriteString(content[cursor:match.start])
		b.WriteString(render(content[match.start:match.end]))
		cursor = match.end
	}
	b.WriteString(content[cursor:])
	return b.String()
}

func findPathTokenMatches(content string) []pathTokenMatch {
	parts := nonWhitespaceTokenPattern.FindAllStringIndex(content, -1)
	if len(parts) == 0 {
		return nil
	}

	matches := make([]pathTokenMatch, 0, len(parts))
	for _, part := range parts {
		start, end, ok := pathTokenBounds(content[part[0]:part[1]])
		if !ok {
			continue
		}
		matches = append(matches, pathTokenMatch{
			start: part[0] + start,
			end:   part[0] + end,
		})
	}
	return matches
}

func pathTokenBounds(part string) (int, int, bool) {
	start := 0
	end := len(part)

	for start < end && isLeadingPathPunctuation(part[start]) {
		start++
	}
	for end > start && isTrailingPathPunctuation(part[end-1]) {
		end--
	}

	if start >= end {
		return 0, 0, false
	}
	core := part[start:end]
	if loc := pathLocationSuffixPattern.FindStringIndex(core); loc != nil && loc[1] == len(core) {
		end = start + loc[0]
		core = part[start:end]
	}
	if start >= end {
		return 0, 0, false
	}
	if !looksLikePathToken(core) && !looksLikeQuotedBareFilenameToken(part, start, end) {
		return 0, 0, false
	}
	return start, end, true
}

func isLeadingPathPunctuation(ch byte) bool {
	switch ch {
	case '(', '[', '{', '"', '\'', '`':
		return true
	default:
		return false
	}
}

func isTrailingPathPunctuation(ch byte) bool {
	switch ch {
	case ')', ']', '}', ',', '.', ':', ';', '!', '?', '"', '\'', '`':
		return true
	default:
		return false
	}
}

func looksLikePathToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "://") {
		return false
	}
	if strings.Contains(value, "/") {
		return !strings.HasPrefix(value, "//")
	}
	return looksLikeFilename(value)
}

func looksLikeFilename(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, " ") {
		return false
	}
	if strings.Contains(value, ".") {
		lower := strings.ToLower(value)
		for _, ext := range []string{
			".go", ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".sh", ".py",
			".js", ".ts", ".tsx", ".jsx", ".html", ".css", ".scss", ".sql", ".xml",
			".ini", ".conf", ".log", ".env", ".rs", ".java", ".c", ".cc", ".cpp",
			".h", ".hpp", ".swift", ".kt", ".rb", ".php",
		} {
			if strings.HasSuffix(lower, ext) {
				return true
			}
		}
		return false
	}
	return false
}

func looksLikeQuotedBareFilenameToken(part string, start, end int) bool {
	if start <= 0 || end >= len(part) {
		return false
	}
	lead := part[start-1]
	trail := part[end]
	if lead != trail {
		return false
	}
	switch lead {
	case '\'', '"', '`':
		return bareFilenamePattern.MatchString(part[start:end])
	default:
		return false
	}
}

func RenderANSIColoredText(text string, c color.Color) string {
	r, g, b := colorRGB(c)
	if shouldUseStandardCyan(r, g, b) {
		return "\x1b[36m" + text + "\x1b[0m"
	}
	return "\x1b[38;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)) + "m" + text + "\x1b[0m"
}

func shouldUseStandardCyan(r, g, b uint8) bool {
	return r < 80 && g > 100 && b > 180
}

func colorRGB(c color.Color) (uint8, uint8, uint8) {
	if c == nil {
		return 255, 255, 255
	}
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}
