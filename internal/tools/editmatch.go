package tools

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// errNotFound is an old_string that no strategy found.
var errNotFound = errors.New("old_string not found in file")

// errAlreadyApplied is an edit whose new_string is in the file already.
var errAlreadyApplied = errors.New("already applied")

// applyEdits applies edits in order, each to the result of the one before;
// any that does not apply fails them all. An edit that seems applied already
// is skipped with a note; when all are, nothing changes.
func applyEdits(content string, edits []EditOperation) (string, []string, error) {
	text, ends := splitLineEnds(content)
	var notes []string
	applied := 0
	for i, edit := range edits {
		label := "The edit"
		if len(edits) > 1 {
			label = fmt.Sprintf("Edit %d", i+1)
		}
		next, how, err := applyEdit(text, ends.prepare(edit))
		switch {
		case errors.Is(err, errAlreadyApplied):
			notes = append(notes, label+" seems applied already (new_string is in the file, old_string is not); skipped")
			continue
		case err != nil && len(edits) > 1:
			return "", nil, fmt.Errorf("edit %d: %w", i+1, err)
		case err != nil:
			return "", nil, err
		}
		if how != "" {
			notes = append(notes, label+" matched "+how)
		}
		text = next
		applied++
	}
	if applied == 0 {
		return "", nil, unchangedError{reason: "new_string is already in the file and old_string is not, so the change seems applied already; nothing changed."}
	}
	return ends.restore(text), notes, nil
}

func applyEdit(content string, edit EditOperation) (string, string, error) {
	switch {
	case edit.OldString == edit.NewString:
		return "", "", errors.New("old_string and new_string are the same; nothing would change")
	case edit.OldString == "" && content != "":
		return "", "", errors.New("old_string is empty; quote the text to replace, or use write to replace the whole file")
	case edit.OldString == "":
		return edit.NewString, "", nil
	}
	spans, how, err := locate(content, edit)
	switch {
	case errors.Is(err, errNotFound) && alreadyApplied(content, edit):
		return "", "", errAlreadyApplied
	case errors.Is(err, errNotFound):
		return "", "", fmt.Errorf("%w. %s", errNotFound, missHint(content, edit.OldString))
	case err != nil:
		return "", "", err
	}
	var out strings.Builder
	at := 0
	for _, span := range spans {
		out.WriteString(content[at:span.start])
		out.WriteString(span.replacement)
		at = span.end
	}
	out.WriteString(content[at:])
	return out.String(), how, nil
}

// lineEnds is what applyEdits takes off a file before matching and puts back
// after: a byte order mark, and CRLF line ends when every line has one (read
// shows lines without them).
type lineEnds struct{ bom, crlf bool }

func splitLineEnds(content string) (string, lineEnds) {
	var ends lineEnds
	if rest, ok := strings.CutPrefix(content, "\uFEFF"); ok {
		content, ends.bom = rest, true
	}
	if n := strings.Count(content, "\r\n"); n > 0 && n == strings.Count(content, "\n") {
		content, ends.crlf = strings.ReplaceAll(content, "\r\n", "\n"), true
	}
	return content, ends
}

func (l lineEnds) prepare(edit EditOperation) EditOperation {
	if l.crlf {
		edit.OldString = strings.ReplaceAll(edit.OldString, "\r\n", "\n")
		edit.NewString = strings.ReplaceAll(edit.NewString, "\r\n", "\n")
	}
	return edit
}

func (l lineEnds) restore(text string) string {
	if l.crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if l.bom {
		text = "\uFEFF" + text
	}
	return text
}

// editSpan is a byte range of the content an edit replaces, and its new text.
type editSpan struct {
	start, end  int
	replacement string
}

// lineStrategy compares a window of the file's lines with old_string's lines
// more loosely than byte for byte; the first strategy that finds old_string
// decides, and it must find exactly one place.
type lineStrategy struct {
	how  string
	same func(fileLines, oldLines []string) bool
}

var lineStrategies = []lineStrategy{
	{"ignoring indentation", func(file, old []string) bool {
		return equalBy(file, old, strings.TrimSpace) && slices.Equal(dedent(file), dedent(old))
	}},
	{"ignoring leading and trailing whitespace", func(file, old []string) bool { return equalBy(file, old, strings.TrimSpace) }},
	{"ignoring whitespace and quote or dash style", func(file, old []string) bool { return equalBy(file, old, looseLine) }},
}

// locate finds where edit's old_string sits in content: byte for byte, then by
// the line strategies, then all of that again with literal escapes such as \n
// in old_string and new_string unescaped. how says which strategy matched; ""
// is byte for byte. replace_all matches byte for byte only.
func locate(content string, edit EditOperation) ([]editSpan, string, error) {
	spans, how, err := locateAs(content, edit.OldString, edit.NewString, edit.ReplaceAll)
	if !errors.Is(err, errNotFound) || edit.ReplaceAll {
		return spans, how, err
	}
	old, oldChanged := unescapeLiteral(edit.OldString)
	if !oldChanged {
		return nil, "", errNotFound
	}
	replacement, _ := unescapeLiteral(edit.NewString)
	spans, how, err = locateAs(content, old, replacement, false)
	if err != nil {
		return nil, "", err
	}
	return spans, strings.TrimSpace("after unescaping \\n, \\t and quotes " + how), nil
}

func locateAs(content string, old string, replacement string, all bool) ([]editSpan, string, error) {
	if count := strings.Count(content, old); count > 0 {
		if count > 1 && !all {
			return nil, "", fmt.Errorf("old_string matches %d places (lines %s); add surrounding lines to make it unique, or set replace_all to change every one", count, matchLines(content, old))
		}
		var spans []editSpan
		for at := 0; ; {
			i := strings.Index(content[at:], old)
			if i < 0 {
				break
			}
			spans = append(spans, editSpan{start: at + i, end: at + i + len(old), replacement: replacement})
			at += i + len(old)
		}
		return spans, "", nil
	}
	if all {
		return nil, "", errNotFound
	}
	oldLines, oldEndsLine := splitBlock(old)
	if strings.TrimSpace(old) == "" {
		return nil, "", errNotFound
	}
	lines := indexLines(content)
	for _, strategy := range lineStrategies {
		var found []int
		for i := 0; i+len(oldLines) <= len(lines); i++ {
			if strategy.same(lines.text(content, i, len(oldLines)), oldLines) {
				found = append(found, i)
			}
		}
		switch {
		case len(found) == 0:
			continue
		case len(found) > 1:
			numbers := make([]string, len(found))
			for k, i := range found {
				numbers[k] = fmt.Sprint(i + 1)
			}
			return nil, "", fmt.Errorf("old_string matches %d places %s (lines %s); add surrounding lines to make it unique", len(found), strategy.how, strings.Join(numbers, ", "))
		}
		i := found[0]
		span := editSpan{start: lines[i].start, end: lines[i+len(oldLines)-1].end}
		switch {
		case oldEndsLine && span.end < len(content):
			span.end++
		case !oldEndsLine && strings.HasSuffix(content[span.start:span.end], "\r"):
			span.end--
		}
		if matched := content[span.start:span.end]; len(matched) > max(len(old)+500, 4*len(old)) {
			return nil, "", errors.New("the closest match spans far more text than old_string; read the file again and quote the text exactly")
		}
		span.replacement = reindent(replacement, oldLines, lines.text(content, i, len(oldLines)))
		return []editSpan{span}, strategy.how, nil
	}
	return nil, "", errNotFound
}

// lineSpan is one line of content without its line end.
type lineSpan struct{ start, end int }

type lineIndex []lineSpan

func indexLines(content string) lineIndex {
	var lines lineIndex
	for start := 0; start < len(content); {
		end := strings.IndexByte(content[start:], '\n')
		if end < 0 {
			lines = append(lines, lineSpan{start, len(content)})
			break
		}
		lines = append(lines, lineSpan{start, start + end})
		start += end + 1
	}
	return lines
}

func (l lineIndex) text(content string, from int, n int) []string {
	out := make([]string, n)
	for k := range n {
		out[k] = strings.TrimSuffix(content[l[from+k].start:l[from+k].end], "\r")
	}
	return out
}

// splitBlock is text's lines, and whether it ends with a line end.
func splitBlock(text string) ([]string, bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	endsLine := strings.HasSuffix(text, "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), endsLine
}

func equalBy(a, b []string, norm func(string) string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if norm(a[i]) != norm(b[i]) {
			return false
		}
	}
	return true
}

// dedent is lines without the leading whitespace they all share; blank lines
// count as empty.
func dedent(lines []string) []string {
	prefix, first := "", true
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingSpace(line)
		if first {
			prefix, first = indent, false
			continue
		}
		for !strings.HasPrefix(indent, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			out[i] = strings.TrimRightFunc(line[len(prefix):], unicode.IsSpace)
		}
	}
	return out
}

func leadingSpace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

var typographic = strings.NewReplacer(
	"\u2018", "'", "\u2019", "'", "\u201a", "'", "\u201b", "'",
	"\u201c", `"`, "\u201d", `"`, "\u201e", `"`, "\u201f", `"`,
	"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-", "\u2015", "-", "\u2212", "-",
	"\u00a0", " ", "\u2007", " ", "\u202f", " ", "\u3000", " ",
)

// looseLine is line with runs of whitespace as one space and typographic
// quotes, dashes and spaces as their ASCII forms.
func looseLine(line string) string {
	return strings.Join(strings.Fields(typographic.Replace(line)), " ")
}

// reindent moves replacement to the indentation the file uses where old_string
// matched: each indentation of old_string's lines maps to the one of the file
// line it matched, and replacement's lines are moved by that map. When the
// map is not consistent replacement is kept as written.
func reindent(replacement string, oldLines []string, fileLines []string) string {
	moves := map[string]string{}
	moved := false
	for i, line := range oldLines {
		if strings.TrimSpace(line) == "" || strings.TrimSpace(fileLines[i]) == "" {
			continue
		}
		from, to := leadingSpace(line), leadingSpace(fileLines[i])
		if prev, ok := moves[from]; ok && prev != to {
			return replacement
		}
		moves[from] = to
		moved = moved || from != to
	}
	if !moved {
		return replacement
	}
	lines := strings.Split(replacement, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent, best := leadingSpace(line), ""
		found := false
		for from := range moves {
			if strings.HasPrefix(indent, from) && (!found || len(from) > len(best)) {
				best, found = from, true
			}
		}
		if found {
			lines[i] = moves[best] + line[len(best):]
		}
	}
	return strings.Join(lines, "\n")
}

var literalEscapes = strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", `\t`, "\t", `\"`, `"`, `\'`, "'", `\\`, `\`)

// unescapeLiteral is text with the escapes a model sometimes doubles (\n, \t,
// \", \', \\) turned into what they stand for; ok is false when it has none.
func unescapeLiteral(text string) (string, bool) {
	out := literalEscapes.Replace(text)
	return out, out != text
}

// matchLines lists the line numbers where old starts in content, at most ten.
func matchLines(content string, old string) string {
	var numbers []string
	line, at := 1, 0
	for len(numbers) < 10 {
		i := strings.Index(content[at:], old)
		if i < 0 {
			break
		}
		line += strings.Count(content[at:at+i], "\n")
		numbers = append(numbers, fmt.Sprint(line))
		line += strings.Count(old, "\n")
		at += i + len(old)
	}
	return strings.Join(numbers, ", ")
}

// alreadyApplied reports whether an edit that did not match seems done
// already: new_string is in the file, is not part of old_string, and is long
// enough not to be there by chance.
func alreadyApplied(content string, edit EditOperation) bool {
	return len(strings.TrimSpace(edit.NewString)) >= 12 && !strings.Contains(edit.OldString, edit.NewString) && strings.Contains(content, edit.NewString)
}

var readPrefix = regexp.MustCompile(`^\s*\d+\t`)

// missHint is what the model reads when old_string was not found: the window
// of the file most like it, with line numbers, or why nothing is close.
func missHint(content string, old string) string {
	oldLines, _ := splitBlock(old)
	prefixed := 0
	for _, line := range oldLines {
		if readPrefix.MatchString(line) {
			prefixed++
		}
	}
	if prefixed == len(oldLines) {
		return "old_string seems to include the line-number prefixes of read output; leave them out and quote only the file's text."
	}
	lines := indexLines(content)
	if len(oldLines) > len(lines) || len(lines)*len(oldLines) > 2_000_000 {
		return "Read the file again and quote its current text exactly."
	}
	loose := make([]string, len(oldLines))
	for i, line := range oldLines {
		loose[i] = looseLine(line)
	}
	fileLoose := make([]string, len(lines))
	for i := range lines {
		fileLoose[i] = looseLine(content[lines[i].start:lines[i].end])
	}
	best, bestScore := -1, 0.0
	for i := 0; i+len(oldLines) <= len(lines); i++ {
		score := 0.0
		for k := range oldLines {
			score += lineSimilarity(fileLoose[i+k], loose[k])
		}
		if score /= float64(len(oldLines)); score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 || bestScore < 0.5 {
		return "Nothing in the file is close to it; read the file again and quote its current text exactly."
	}
	shown := min(len(oldLines), 20)
	var out strings.Builder
	fmt.Fprintf(&out, "Closest text is at lines %d-%d (%d%% similar):\n", best+1, best+len(oldLines), int(bestScore*100))
	for k, line := range lines.text(content, best, shown) {
		fmt.Fprintf(&out, "%6d\t%s\n", best+k+1, clipLine(line))
	}
	if shown < len(oldLines) {
		fmt.Fprintf(&out, "… %d more lines\n", len(oldLines)-shown)
	}
	out.WriteString("Quote the text exactly as it is there, or read those lines again.")
	return out.String()
}

// lineSimilarity is how much of the longer line the two share at their start
// and end, from 0 to 1.
func lineSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	longest := max(len(a), len(b))
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return float64(prefix+suffix) / float64(longest)
}
