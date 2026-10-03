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

	getContent := func(in string) (content string, leadingEllipsis bool) {
		content = strings.TrimSuffix(in, "\n")
		content = ansi.GraphemeWidth.Cut(content, dv.xOffset, len(content))
		content = ansi.Truncate(content, dv.codeWidth, "…")
		leadingEllipsis = dv.xOffset > 0 && strings.TrimSpace(content) != ""
		return content, leadingEllipsis
	}

	for _, h := range dv.unified.Hunks {
		beforeLine := h.FromLine
		afterLine := h.ToLine

		for _, l := range h.Lines {
			switch l.Kind {
			case udiff.Equal:
				content, leadingEllipsis := getContent(l.Content)
				b.WriteString(dv.renderUnifiedLine(dv.style.EqualLine, afterLine, " ", ternary(leadingEllipsis, "…"+content, content)))
				beforeLine++
				afterLine++
			case udiff.Insert:
				content, leadingEllipsis := getContent(l.Content)
				b.WriteString(dv.renderUnifiedLine(dv.style.InsertLine, afterLine, "+", ternary(leadingEllipsis, "…"+content, content)))
				afterLine++
			case udiff.Delete:
				content, leadingEllipsis := getContent(l.Content)
				b.WriteString(dv.renderUnifiedLine(dv.style.DeleteLine, beforeLine, "-", ternary(leadingEllipsis, "…"+content, content)))
				beforeLine++
			}
			b.WriteRune('\n')
		}
	}

	return b.String()
}

func (dv *DiffView) renderUnifiedLine(ls LineStyle, lineNumber any, symbol, content string) string {
	digits := max(dv.beforeNumDigits, dv.afterNumDigits)
	prefix := fmt.Sprintf("%*v %s ", digits, lineNumber, symbol)
	return ls.Code.Width(ansi.StringWidth(prefix) + dv.codeWidth).Render(prefix + content)
}

// renderSplit renders the split (side-by-side) diff view as a string.
func (dv *DiffView) renderSplit() string {
	var b strings.Builder

	beforeFullContentStyle := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth)
	afterFullContentStyle := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth + btoi(dv.extraColOnAfter))

	getContent := func(in string) (content string, leadingEllipsis bool) {
		content = strings.TrimSuffix(in, "\n")
		content = ansi.GraphemeWidth.Cut(content, dv.xOffset, len(content))
		content = ansi.Truncate(content, dv.codeWidth, "…")
		leadingEllipsis = dv.xOffset > 0 && strings.TrimSpace(content) != ""
		return content, leadingEllipsis
	}

	for i, h := range dv.splitHunks {
		ls := dv.style.DividerLine
		b.WriteString(ls.LineNumber.Render(pad("…", dv.beforeNumDigits)))
		content := ansi.Truncate(dv.hunkLineFor(dv.unified.Hunks[i]), dv.fullCodeWidth, "…")
		b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render(content))
		b.WriteString(ls.LineNumber.Render(pad("…", dv.afterNumDigits)))
		b.WriteString(ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render(" "))
		b.WriteRune('\n')

		beforeLine := h.fromLine
		afterLine := h.toLine

		for _, l := range h.lines {
			switch {
			case l.before == nil:
				ls := dv.style.MissingLine
				b.WriteString(ls.LineNumber.Render(pad(" ", dv.beforeNumDigits)))
				b.WriteString(beforeFullContentStyle.Render(
					ls.Code.Width(dv.fullCodeWidth).Render("  "),
				))
			case l.before.Kind == udiff.Equal:
				ls := dv.style.EqualLine
				content, leadingEllipsis := getContent(l.before.Content)
				b.WriteString(ls.LineNumber.Render(pad(beforeLine, dv.beforeNumDigits)))
				b.WriteString(beforeFullContentStyle.Render(
					ls.Code.Width(dv.fullCodeWidth).Render(ternary(leadingEllipsis, " …", "  ") + content),
				))
				beforeLine++
			case l.before.Kind == udiff.Delete:
				ls := dv.style.DeleteLine
				content, leadingEllipsis := getContent(l.before.Content)
				b.WriteString(ls.LineNumber.Render(pad(beforeLine, dv.beforeNumDigits)))
				b.WriteString(beforeFullContentStyle.Render(
					ls.Symbol.Render(ternary(leadingEllipsis, "-…", "- ")) +
						ls.Code.Width(dv.codeWidth).Render(content),
				))
				beforeLine++
			}

			switch {
			case l.after == nil:
				ls := dv.style.MissingLine
				b.WriteString(ls.LineNumber.Render(pad(" ", dv.afterNumDigits)))
				b.WriteString(afterFullContentStyle.Render(
					ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render("  "),
				))
			case l.after.Kind == udiff.Equal:
				ls := dv.style.EqualLine
				content, leadingEllipsis := getContent(l.after.Content)
				b.WriteString(ls.LineNumber.Render(pad(afterLine, dv.afterNumDigits)))
				b.WriteString(afterFullContentStyle.Render(
					ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render(ternary(leadingEllipsis, " …", "  ") + content),
				))
				afterLine++
			case l.after.Kind == udiff.Insert:
				ls := dv.style.InsertLine
				content, leadingEllipsis := getContent(l.after.Content)
				b.WriteString(ls.LineNumber.Render(pad(afterLine, dv.afterNumDigits)))
				b.WriteString(afterFullContentStyle.Render(
					ls.Symbol.Render(ternary(leadingEllipsis, "+…", "+ ")) +
						ls.Code.Width(dv.codeWidth+btoi(dv.extraColOnAfter)).Render(content),
				))
				afterLine++
			}

			b.WriteRune('\n')
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
