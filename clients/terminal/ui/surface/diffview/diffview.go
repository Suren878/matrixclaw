package diffview

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/aymanbagabas/go-udiff"
)

const (
	leadingSymbolsSize = 2
	lineNumPadding     = 1
)

type file struct {
	path    string
	content string
}

type layout int

const (
	layoutUnified layout = iota + 1
	layoutSplit
)

// DiffView represents a view for displaying differences between two files.
type DiffView struct {
	layout          layout
	before          file
	after           file
	contextLines    int
	lineNumbers     bool
	height          int
	width           int
	xOffset         int
	yOffset         int
	infiniteYScroll bool
	style           Style
	tabWidth        int
	chromaStyle     *chroma.Style

	isComputed bool
	err        error
	unified    udiff.UnifiedDiff
	edits      []udiff.Edit

	splitHunks []splitHunk

	totalLines      int
	codeWidth       int
	fullCodeWidth   int
	extraColOnAfter bool
	beforeNumDigits int
	afterNumDigits  int

	cachedLexer chroma.Lexer
	syntaxCache map[string]string
}

const errUnknownDiffViewLayout = "unknown diffview layout"

// String returns the string representation of the DiffView.
func (dv *DiffView) String() string {
	dv.normalizeLineEndings()
	dv.replaceTabs()
	if err := dv.computeDiff(); err != nil {
		return err.Error()
	}
	dv.convertDiffToSplit()
	dv.adjustStyles()
	dv.detectNumDigits()
	dv.detectTotalLines()
	dv.preventInfiniteYScroll()

	if dv.width <= 0 {
		dv.detectCodeWidth()
	} else {
		dv.resizeCodeWidth()
	}

	style := lipgloss.NewStyle()
	if dv.width > 0 {
		style = style.MaxWidth(dv.width)
	}
	if dv.height > 0 {
		style = style.MaxHeight(dv.height)
	}

	switch dv.layout {
	case layoutUnified:
		return style.Render(strings.TrimSuffix(dv.renderUnified(), "\n"))
	case layoutSplit:
		return style.Render(strings.TrimSuffix(dv.renderSplit(), "\n"))
	default:
		return style.Render(errUnknownDiffViewLayout)
	}
}
