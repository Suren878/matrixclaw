package diffview

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestUnifiedDiffOmitsHunkHeadersAndAlignsChangeContent(t *testing.T) {
	t.Parallel()

	rendered := New().
		Before("notes.txt", "one\ntwo\nthree\n").
		After("notes.txt", "one\nTWO\nthree\nfour\n").
		Width(80).
		Unified().
		String()

	plain := ansi.Strip(rendered)
	if strings.Contains(plain, "@@") {
		t.Fatalf("unified diff should not render hunk headers, got:\n%s", plain)
	}

	lines := strings.Split(plain, "\n")
	deleteContentCol := -1
	insertContentCol := -1
	deleteLine := ""
	insertLine := ""
	for _, line := range lines {
		if idx := strings.Index(line, "- two"); idx >= 0 {
			deleteContentCol = idx + 2
			deleteLine = line
		}
		if idx := strings.Index(line, "+ TWO"); idx >= 0 {
			insertContentCol = idx + 2
			insertLine = line
		}
	}
	if deleteContentCol < 0 {
		t.Fatalf("delete line not found in:\n%s", plain)
	}
	if insertContentCol < 0 {
		t.Fatalf("insert line not found in:\n%s", plain)
	}
	if deleteContentCol != insertContentCol {
		t.Fatalf("changed content columns differ: delete=%d insert=%d\n%s", deleteContentCol, insertContentCol, plain)
	}
	if strings.Contains(deleteLine[:deleteContentCol], "  ") || strings.Contains(insertLine[:insertContentCol], "  ") {
		t.Fatalf("unified diff should use one line-number column, got delete=%q insert=%q", deleteLine, insertLine)
	}
}

func TestUnifiedDiffPreservesDeleteBackgroundInStyledCells(t *testing.T) {
	t.Parallel()

	lipgloss.Writer.Profile = colorprofile.TrueColor

	deleteBg := lipgloss.Color("#6b2028")
	rendered := New().
		Style(Style{
			EqualLine: LineStyle{
				Code: lipgloss.NewStyle(),
			},
			InsertLine: LineStyle{
				Code: lipgloss.NewStyle().Foreground(lipgloss.Color("#b7f7b0")).Background(lipgloss.Color("#1f3a24")),
			},
			DeleteLine: LineStyle{
				Code: lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd7da")).Background(deleteBg),
			},
		}).
		Before("notes.txt", "old\n").
		After("notes.txt", "new\n").
		Width(40).
		Unified().
		String()

	buf := uv.NewScreenBuffer(40, 4)
	uv.NewStyledString(rendered).Draw(buf, buf.Bounds())

	cell := buf.CellAt(0, 0)
	if cell == nil || cell.Style.Bg == nil {
		t.Fatalf("delete line should preserve background style, rendered=%q", rendered)
	}
	if !sameRGB(cell.Style.Bg, deleteBg) {
		t.Fatalf("delete background = %#v, want %s", cell.Style.Bg, deleteBg)
	}
}

func sameRGB(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}
