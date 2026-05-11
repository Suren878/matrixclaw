package diffview

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/x/ansi"
)

// normalizeLineEndings ensures the file contents use Unix-style line endings.
func (dv *DiffView) normalizeLineEndings() {
	dv.before.content = strings.ReplaceAll(dv.before.content, "\r\n", "\n")
	dv.after.content = strings.ReplaceAll(dv.after.content, "\r\n", "\n")
}

// replaceTabs replaces tabs in the before and after file contents with spaces.
func (dv *DiffView) replaceTabs() {
	spaces := strings.Repeat(" ", dv.tabWidth)
	dv.before.content = strings.ReplaceAll(dv.before.content, "\t", spaces)
	dv.after.content = strings.ReplaceAll(dv.after.content, "\t", spaces)
}

// computeDiff computes the differences between the "before" and "after" files.
func (dv *DiffView) computeDiff() error {
	if dv.isComputed {
		return dv.err
	}
	dv.isComputed = true
	dv.edits = udiff.Lines(dv.before.content, dv.after.content)
	dv.unified, dv.err = udiff.ToUnifiedDiff(
		dv.before.path,
		dv.after.path,
		dv.before.content,
		dv.edits,
		dv.contextLines,
	)
	return dv.err
}

// convertDiffToSplit converts the unified diff to a split diff if requested.
func (dv *DiffView) convertDiffToSplit() {
	if dv.layout != layoutSplit {
		return
	}

	dv.splitHunks = make([]splitHunk, len(dv.unified.Hunks))
	for i, h := range dv.unified.Hunks {
		dv.splitHunks[i] = hunkToSplit(h)
	}
}

// adjustStyles adds padding and alignment to the line number styles.
func (dv *DiffView) adjustStyles() {
	setPadding := func(s lipgloss.Style) lipgloss.Style {
		return s.Padding(0, lineNumPadding).Align(lipgloss.Right)
	}
	dv.style.MissingLine.LineNumber = setPadding(dv.style.MissingLine.LineNumber)
	dv.style.DividerLine.LineNumber = setPadding(dv.style.DividerLine.LineNumber)
	dv.style.EqualLine.LineNumber = setPadding(dv.style.EqualLine.LineNumber)
	dv.style.InsertLine.LineNumber = setPadding(dv.style.InsertLine.LineNumber)
	dv.style.DeleteLine.LineNumber = setPadding(dv.style.DeleteLine.LineNumber)
}

// detectNumDigits calculates the max width for before and after line numbers.
func (dv *DiffView) detectNumDigits() {
	dv.beforeNumDigits = 0
	dv.afterNumDigits = 0

	for _, h := range dv.unified.Hunks {
		dv.beforeNumDigits = max(dv.beforeNumDigits, len(strconv.Itoa(h.FromLine+len(h.Lines))))
		dv.afterNumDigits = max(dv.afterNumDigits, len(strconv.Itoa(h.ToLine+len(h.Lines))))
	}
}

func (dv *DiffView) detectTotalLines() {
	dv.totalLines = 0

	switch dv.layout {
	case layoutUnified:
		for _, h := range dv.unified.Hunks {
			dv.totalLines += len(h.Lines)
		}
	case layoutSplit:
		for _, h := range dv.splitHunks {
			dv.totalLines += 1 + len(h.lines)
		}
	}
}

func (dv *DiffView) preventInfiniteYScroll() {
	if dv.infiniteYScroll {
		return
	}

	if dv.height > 0 {
		maxYOffset := max(0, dv.totalLines-dv.height)
		dv.yOffset = min(dv.yOffset, maxYOffset)
	} else {
		dv.yOffset = min(dv.yOffset, max(0, dv.totalLines-1))
	}
	dv.yOffset = max(0, dv.yOffset)
}

// detectCodeWidth calculates the maximum width of code lines in the diff view.
func (dv *DiffView) detectCodeWidth() {
	switch dv.layout {
	case layoutUnified:
		dv.detectUnifiedCodeWidth()
	case layoutSplit:
		dv.detectSplitCodeWidth()
	}
	dv.fullCodeWidth = dv.codeWidth + leadingSymbolsSize
}

func (dv *DiffView) detectUnifiedCodeWidth() {
	dv.codeWidth = 0

	for _, h := range dv.unified.Hunks {
		for _, l := range h.Lines {
			lineWidth := ansi.StringWidth(strings.TrimSuffix(l.Content, "\n"))
			dv.codeWidth = max(dv.codeWidth, lineWidth)
		}
	}
}

func (dv *DiffView) detectSplitCodeWidth() {
	dv.codeWidth = 0

	for i, h := range dv.splitHunks {
		shownLines := ansi.StringWidth(dv.hunkLineFor(dv.unified.Hunks[i]))

		for _, l := range h.lines {
			if l.before != nil {
				codeWidth := ansi.StringWidth(strings.TrimSuffix(l.before.Content, "\n")) + 1
				dv.codeWidth = max(dv.codeWidth, codeWidth, shownLines)
			}
			if l.after != nil {
				codeWidth := ansi.StringWidth(strings.TrimSuffix(l.after.Content, "\n")) + 1
				dv.codeWidth = max(dv.codeWidth, codeWidth, shownLines)
			}
		}
	}
}

// resizeCodeWidth resizes the code width to fit within the specified width.
func (dv *DiffView) resizeCodeWidth() {
	fullNumWidth := dv.beforeNumDigits + dv.afterNumDigits
	fullNumWidth += lineNumPadding * 4

	switch dv.layout {
	case layoutUnified:
		prefixWidth := leadingSymbolsSize
		if dv.lineNumbers {
			prefixWidth = max(dv.beforeNumDigits, dv.afterNumDigits) + leadingSymbolsSize
		}
		dv.codeWidth = max(0, dv.width-prefixWidth)
	case layoutSplit:
		remainingWidth := dv.width - fullNumWidth - leadingSymbolsSize*2
		dv.codeWidth = remainingWidth / 2
		dv.extraColOnAfter = isOdd(remainingWidth)
	}

	dv.fullCodeWidth = dv.codeWidth + leadingSymbolsSize
}
