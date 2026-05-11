package diffview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/x/ansi"
)

// renderUnified renders the unified diff view as a string.
func (dv *DiffView) renderUnified() string {
	var b strings.Builder

	printedLines := -dv.yOffset
	shouldWrite := func() bool { return printedLines >= 0 }

	getContent := func(in string) (content string, leadingEllipsis bool) {
		content = strings.TrimSuffix(in, "\n")
		content = ansi.GraphemeWidth.Cut(content, dv.xOffset, len(content))
		content = ansi.Truncate(content, dv.codeWidth, "…")
		leadingEllipsis = dv.xOffset > 0 && strings.TrimSpace(content) != ""
		return content, leadingEllipsis
	}

outer:
	for i, h := range dv.unified.Hunks {
		beforeLine := h.FromLine
		afterLine := h.ToLine

		for j, l := range h.Lines {
			hasReachedHeight := dv.height > 0 && printedLines+1 == dv.height
			isLastHunk := i+1 == len(dv.unified.Hunks)
			isLastLine := j+1 == len(h.Lines)
			if hasReachedHeight && (!isLastHunk || !isLastLine) {
				if shouldWrite() {
					ls := dv.lineStyleForType(l.Kind)
					b.WriteString(dv.renderUnifiedLine(ls, "…", " ", "…"))
					b.WriteRune('\n')
				}
				break outer
			}

			switch l.Kind {
			case udiff.Equal:
				if shouldWrite() {
					ls := dv.style.EqualLine
					content, leadingEllipsis := getContent(l.Content)
					b.WriteString(dv.renderUnifiedLine(ls, afterLine, " ", ternary(leadingEllipsis, "…"+content, content)))
				}
				beforeLine++
				afterLine++
			case udiff.Insert:
				if shouldWrite() {
					ls := dv.style.InsertLine
					content, leadingEllipsis := getContent(l.Content)
					b.WriteString(dv.renderUnifiedLine(ls, afterLine, "+", ternary(leadingEllipsis, "…"+content, content)))
				}
				afterLine++
			case udiff.Delete:
				if shouldWrite() {
					ls := dv.style.DeleteLine
					content, leadingEllipsis := getContent(l.Content)
					b.WriteString(dv.renderUnifiedLine(ls, beforeLine, "-", ternary(leadingEllipsis, "…"+content, content)))
				}
				beforeLine++
			}
			if shouldWrite() {
				b.WriteRune('\n')
			}

			printedLines++
		}
	}

	return b.String()
}

func (dv *DiffView) renderUnifiedLine(ls LineStyle, lineNumber any, symbol, content string) string {
	if dv.lineNumbers {
		digits := max(dv.beforeNumDigits, dv.afterNumDigits)
		prefix := fmt.Sprintf("%*v %s ", digits, lineNumber, symbol)
		return ls.Code.Width(ansi.StringWidth(prefix) + dv.codeWidth).Render(prefix + content)
	}
	return ls.Code.Width(leadingSymbolsSize + dv.codeWidth).Render(symbol + " " + content)
}

// renderSplit renders the split (side-by-side) diff view as a string.
func (dv *DiffView) renderSplit() string {
	var b strings.Builder

	beforeFullContentStyle := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth)
	afterFullContentStyle := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth + btoi(dv.extraColOnAfter))
	printedLines := -dv.yOffset
	shouldWrite := func() bool { return printedLines >= 0 }

	getContent := func(in string, ls LineStyle) (content string, leadingEllipsis bool) {
		content = strings.TrimSuffix(in, "\n")
		content = dv.hightlightCode(content, ls.Code.GetBackground())
		content = ansi.GraphemeWidth.Cut(content, dv.xOffset, len(content))
		content = ansi.Truncate(content, dv.codeWidth, "…")
		leadingEllipsis = dv.xOffset > 0 && strings.TrimSpace(content) != ""
		return content, leadingEllipsis
	}

outer:
	for i, h := range dv.splitHunks {
		if shouldWrite() {
			ls := dv.style.DividerLine
			if dv.lineNumbers {
				b.WriteString(ls.LineNumber.Render(pad("…", dv.beforeNumDigits)))
			}
			content := ansi.Truncate(dv.hunkLineFor(dv.unified.Hunks[i]), dv.fullCodeWidth, "…")
			b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render(content))
			if dv.lineNumbers {
				b.WriteString(ls.LineNumber.Render(pad("…", dv.afterNumDigits)))
			}
			b.WriteString(ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render(" "))
			b.WriteRune('\n')
		}
		printedLines++

		beforeLine := h.fromLine
		afterLine := h.toLine

		for j, l := range h.lines {
			hasReachedHeight := dv.height > 0 && printedLines+1 == dv.height
			isLastHunk := i+1 == len(dv.unified.Hunks)
			isLastLine := j+1 == len(h.lines)
			if hasReachedHeight && (!isLastHunk || !isLastLine) {
				if shouldWrite() {
					ls := dv.style.MissingLine
					if l.before != nil {
						ls = dv.lineStyleForType(l.before.Kind)
					}
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad("…", dv.beforeNumDigits)))
					}
					b.WriteString(beforeFullContentStyle.Render(
						ls.Code.Width(dv.fullCodeWidth).Render("  …"),
					))
					ls = dv.style.MissingLine
					if l.after != nil {
						ls = dv.lineStyleForType(l.after.Kind)
					}
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad("…", dv.afterNumDigits)))
					}
					b.WriteString(afterFullContentStyle.Render(
						ls.Code.Width(dv.fullCodeWidth).Render("  …"),
					))
					b.WriteRune('\n')
				}
				break outer
			}

			switch {
			case l.before == nil:
				if shouldWrite() {
					ls := dv.style.MissingLine
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad(" ", dv.beforeNumDigits)))
					}
					b.WriteString(beforeFullContentStyle.Render(
						ls.Code.Width(dv.fullCodeWidth).Render("  "),
					))
				}
			case l.before.Kind == udiff.Equal:
				if shouldWrite() {
					ls := dv.style.EqualLine
					content, leadingEllipsis := getContent(l.before.Content, ls)
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad(beforeLine, dv.beforeNumDigits)))
					}
					b.WriteString(beforeFullContentStyle.Render(
						ls.Code.Width(dv.fullCodeWidth).Render(ternary(leadingEllipsis, " …", "  ") + content),
					))
				}
				beforeLine++
			case l.before.Kind == udiff.Delete:
				if shouldWrite() {
					ls := dv.style.DeleteLine
					content, leadingEllipsis := getContent(l.before.Content, ls)
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad(beforeLine, dv.beforeNumDigits)))
					}
					b.WriteString(beforeFullContentStyle.Render(
						ls.Symbol.Render(ternary(leadingEllipsis, "-…", "- ")) +
							ls.Code.Width(dv.codeWidth).Render(content),
					))
				}
				beforeLine++
			}

			switch {
			case l.after == nil:
				if shouldWrite() {
					ls := dv.style.MissingLine
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad(" ", dv.afterNumDigits)))
					}
					b.WriteString(afterFullContentStyle.Render(
						ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render("  "),
					))
				}
			case l.after.Kind == udiff.Equal:
				if shouldWrite() {
					ls := dv.style.EqualLine
					content, leadingEllipsis := getContent(l.after.Content, ls)
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad(afterLine, dv.afterNumDigits)))
					}
					b.WriteString(afterFullContentStyle.Render(
						ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render(ternary(leadingEllipsis, " …", "  ") + content),
					))
				}
				afterLine++
			case l.after.Kind == udiff.Insert:
				if shouldWrite() {
					ls := dv.style.InsertLine
					content, leadingEllipsis := getContent(l.after.Content, ls)
					if dv.lineNumbers {
						b.WriteString(ls.LineNumber.Render(pad(afterLine, dv.afterNumDigits)))
					}
					b.WriteString(afterFullContentStyle.Render(
						ls.Symbol.Render(ternary(leadingEllipsis, "+…", "+ ")) +
							ls.Code.Width(dv.codeWidth+btoi(dv.extraColOnAfter)).Render(content),
					))
				}
				afterLine++
			}

			if shouldWrite() {
				b.WriteRune('\n')
			}

			printedLines++
		}
	}

	return b.String()
}

// hunkLineFor formats the header line for a hunk in the unified diff view.
func (dv *DiffView) hunkLineFor(h *udiff.Hunk) string {
	beforeShownLines, afterShownLines := dv.hunkShownLines(h)

	return fmt.Sprintf(
		"  @@ -%d,%d +%d,%d @@ ",
		h.FromLine,
		beforeShownLines,
		h.ToLine,
		afterShownLines,
	)
}

func (dv *DiffView) hunkShownLines(h *udiff.Hunk) (before, after int) {
	for _, l := range h.Lines {
		switch l.Kind {
		case udiff.Equal:
			before++
			after++
		case udiff.Insert:
			after++
		case udiff.Delete:
			before++
		}
	}
	return before, after
}

func (dv *DiffView) lineStyleForType(t udiff.OpKind) LineStyle {
	switch t {
	case udiff.Equal:
		return dv.style.EqualLine
	case udiff.Insert:
		return dv.style.InsertLine
	case udiff.Delete:
		return dv.style.DeleteLine
	default:
		return dv.style.MissingLine
	}
}
