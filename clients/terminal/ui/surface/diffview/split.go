package diffview

import (
	"slices"

	"github.com/aymanbagabas/go-udiff"
)

type splitHunk struct {
	fromLine int
	toLine   int
	lines    []*splitLine
}

type splitLine struct {
	before *udiff.Line
	after  *udiff.Line
}

func hunkToSplit(h *udiff.Hunk) (sh splitHunk) {
	lines := slices.Clone(h.Lines)
	sh = splitHunk{
		fromLine: h.FromLine,
		toLine:   h.ToLine,
		lines:    make([]*splitLine, 0, len(lines)),
	}

	for {
		var ul udiff.Line
		var ok bool
		ul, lines, ok = shiftLine(lines)
		if !ok {
			break
		}

		var sl splitLine

		switch ul.Kind {
		// For equal lines, add as is
		case udiff.Equal:
			sl.before = &ul
			sl.after = &ul

		// For inserted lines, set after and keep before as nil
		case udiff.Insert:
			sl.before = nil
			sl.after = &ul

		// For deleted lines, set before and loop over the next lines
		// searching for the equivalent after line.
		case udiff.Delete:
			sl.before = &ul

		inner:
			for i, l := range lines {
				switch l.Kind {
				case udiff.Insert:
					var ll udiff.Line
					ll, lines, _ = deleteAt(lines, i)
					sl.after = &ll
					break inner
				case udiff.Equal:
					break inner
				}
			}
		}

		sh.lines = append(sh.lines, &sl)
	}

	return sh
}

func shiftLine(lines []udiff.Line) (udiff.Line, []udiff.Line, bool) {
	if len(lines) == 0 {
		return udiff.Line{}, nil, false
	}
	return lines[0], lines[1:], true
}

func deleteAt(lines []udiff.Line, i int) (udiff.Line, []udiff.Line, bool) {
	if i < 0 || i >= len(lines) {
		return udiff.Line{}, lines, false
	}
	value := lines[i]
	return value, append(lines[:i], lines[i+1:]...), true
}
