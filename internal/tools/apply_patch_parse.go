package tools

import (
	"fmt"
	"strings"
)

// The markers of the patch format apply_patch takes (Codex's V4A format).
const (
	beginPatchMarker    = "*** Begin Patch"
	endPatchMarker      = "*** End Patch"
	addFileMarker       = "*** Add File: "
	deleteFileMarker    = "*** Delete File: "
	updateFileMarker    = "*** Update File: "
	moveToMarker        = "*** Move to: "
	endOfFileMarker     = "*** End of File"
	emptyContextMarker  = "@@"
	changeContextMarker = "@@ "
)

type hunkKind int

const (
	hunkAdd hunkKind = iota
	hunkDelete
	hunkUpdate
)

// patchHunk is one file section of a patch: the content of an added file, a
// deletion, or the chunks of an update, which may also move the file.
type patchHunk struct {
	kind    hunkKind
	path    string
	moveTo  string
	content string
	chunks  []patchChunk
}

// patchChunk is one change of an update: old lines become new lines, found
// after the line context names (when set); eof pins old to the file's end.
type patchChunk struct {
	context    string
	hasContext bool
	old, new   []string
	eof        bool
}

// patchError is a patch that does not parse, with the line it failed at.
type patchError struct {
	line    int
	message string
}

func (e patchError) Error() string {
	if e.line == 0 {
		return "invalid patch: " + e.message
	}
	return fmt.Sprintf("invalid patch at line %d: %s", e.line, e.message)
}

// parsePatch reads a patch leniently: whitespace around the markers is
// ignored, a heredoc wrapper (<<EOF … EOF) is dropped and blank lines between
// sections are skipped.
func parsePatch(text string) ([]patchHunk, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	lines, err := patchBody(lines)
	if err != nil {
		return nil, err
	}
	var hunks []patchHunk
	for at := 1; at < len(lines)-1; {
		if strings.TrimSpace(lines[at]) == "" {
			at++
			continue
		}
		hunk, used, err := parseHunk(lines[at:len(lines)-1], at+1)
		if err != nil {
			return nil, err
		}
		hunks = append(hunks, hunk)
		at += used
	}
	return hunks, nil
}

// patchBody checks that lines open and close the patch, inside a heredoc
// wrapper or not, and returns them from the opening to the closing marker.
func patchBody(lines []string) ([]string, error) {
	err := checkPatchBoundaries(lines)
	if err == nil {
		return lines, nil
	}
	if len(lines) >= 4 {
		first, last := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[len(lines)-1])
		if (first == "<<EOF" || first == "<<'EOF'" || first == `<<"EOF"`) && strings.HasSuffix(last, "EOF") {
			inner := lines[1 : len(lines)-1]
			return inner, checkPatchBoundaries(inner)
		}
	}
	return nil, err
}

func checkPatchBoundaries(lines []string) error {
	switch {
	case strings.TrimSpace(lines[0]) != beginPatchMarker:
		return patchError{message: "the first line of the patch must be '" + beginPatchMarker + "'"}
	case len(lines) < 2 || strings.TrimSpace(lines[len(lines)-1]) != endPatchMarker:
		return patchError{message: "the last line of the patch must be '" + endPatchMarker + "'"}
	}
	return nil
}

// parseHunk reads the section starting at lines[0] (patch line number) and
// returns it with the number of lines it took.
func parseHunk(lines []string, number int) (patchHunk, int, error) {
	header := strings.TrimSpace(lines[0])
	if path, ok := strings.CutPrefix(header, addFileMarker); ok {
		var content strings.Builder
		used := 1
		for ; used < len(lines); used++ {
			line, ok := strings.CutPrefix(lines[used], "+")
			if !ok {
				break
			}
			content.WriteString(line + "\n")
		}
		return patchHunk{kind: hunkAdd, path: strings.TrimSpace(path), content: content.String()}, used, nil
	}
	if path, ok := strings.CutPrefix(header, deleteFileMarker); ok {
		return patchHunk{kind: hunkDelete, path: strings.TrimSpace(path)}, 1, nil
	}
	path, ok := strings.CutPrefix(header, updateFileMarker)
	if !ok {
		return patchHunk{}, 0, patchError{line: number, message: fmt.Sprintf("'%s' is not a valid section header; valid headers are '%s<path>', '%s<path>' and '%s<path>'", header, addFileMarker, deleteFileMarker, updateFileMarker)}
	}
	hunk := patchHunk{kind: hunkUpdate, path: strings.TrimSpace(path)}
	used := 1
	if used < len(lines) {
		if moveTo, ok := strings.CutPrefix(strings.TrimSpace(lines[used]), moveToMarker); ok {
			hunk.moveTo = strings.TrimSpace(moveTo)
			used++
		}
	}
	for used < len(lines) {
		if strings.TrimSpace(lines[used]) == "" {
			used++
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(lines[used]), "***") {
			break
		}
		chunk, n, err := parseChunk(lines[used:], number+used, len(hunk.chunks) == 0)
		if err != nil {
			return patchHunk{}, 0, err
		}
		hunk.chunks = append(hunk.chunks, chunk)
		used += n
	}
	if len(hunk.chunks) == 0 {
		return patchHunk{}, 0, patchError{line: number, message: fmt.Sprintf("the update of '%s' changes nothing", hunk.path)}
	}
	return hunk, used, nil
}

// sectionHeader reports whether line, whitespace around it ignored, starts a
// section; it ends the chunk before it even when indented like a context line.
func sectionHeader(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, addFileMarker) || strings.HasPrefix(line, deleteFileMarker) || strings.HasPrefix(line, updateFileMarker)
}

const emptyChunk = "the update has a chunk without lines"

// parseChunk reads one chunk of an update: an optional @@ line (required but
// for the first chunk), then context, removed and added lines, and an optional
// end-of-file marker. It returns the lines it took.
func parseChunk(lines []string, number int, contextOptional bool) (patchChunk, int, error) {
	var chunk patchChunk
	start := 0
	switch first := strings.TrimRight(lines[0], " \t"); {
	case first == emptyContextMarker:
		start = 1
	case strings.HasPrefix(first, changeContextMarker):
		chunk.context, chunk.hasContext, start = first[len(changeContextMarker):], true, 1
	case !contextOptional:
		return patchChunk{}, 0, patchError{line: number, message: fmt.Sprintf("expected the chunk to start with a @@ line, got '%s'", lines[0])}
	}
	if start >= len(lines) {
		return patchChunk{}, 0, patchError{line: number, message: emptyChunk}
	}
	taken := 0
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == endOfFileMarker {
			if taken == 0 {
				return patchChunk{}, 0, patchError{line: number, message: emptyChunk}
			}
			chunk.eof = true
			taken++
			break
		}
		if taken > 0 && sectionHeader(line) {
			break
		}
		if line == "" {
			chunk.old, chunk.new = append(chunk.old, ""), append(chunk.new, "")
			taken++
			continue
		}
		switch line[0] {
		case ' ':
			chunk.old, chunk.new = append(chunk.old, line[1:]), append(chunk.new, line[1:])
		case '+':
			chunk.new = append(chunk.new, line[1:])
		case '-':
			chunk.old = append(chunk.old, line[1:])
		default:
			if taken == 0 {
				return patchChunk{}, 0, patchError{line: number + start, message: fmt.Sprintf("unexpected line '%s' in the update; every line starts with ' ' (context), '+' (added) or '-' (removed)", line)}
			}
			return chunk, taken + start, nil
		}
		taken++
	}
	return chunk, taken + start, nil
}
