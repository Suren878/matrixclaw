package diffview

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/aymanbagabas/go-udiff"
)

// New creates a new DiffView with default settings.
func New() *DiffView {
	dv := &DiffView{
		layout:       layoutUnified,
		contextLines: udiff.DefaultContextLines,
		lineNumbers:  true,
		tabWidth:     8,
		syntaxCache:  make(map[string]string),
	}
	dv.style = DefaultDarkStyle()
	return dv
}

// Unified sets the layout of the DiffView to unified.
func (dv *DiffView) Unified() *DiffView {
	dv.layout = layoutUnified
	return dv
}

// Split sets the layout of the DiffView to split (side-by-side).
func (dv *DiffView) Split() *DiffView {
	dv.layout = layoutSplit
	return dv
}

// Before sets the "before" file for the DiffView.
func (dv *DiffView) Before(path, content string) *DiffView {
	dv.before = file{path: path, content: content}
	dv.clearCaches()
	return dv
}

// After sets the "after" file for the DiffView.
func (dv *DiffView) After(path, content string) *DiffView {
	dv.after = file{path: path, content: content}
	dv.clearCaches()
	return dv
}

// clearCaches clears all caches when content or major settings change.
func (dv *DiffView) clearCaches() {
	dv.cachedLexer = nil
	dv.clearSyntaxCache()
	dv.isComputed = false
}

// ContextLines sets the number of context lines for the DiffView.
func (dv *DiffView) ContextLines(contextLines int) *DiffView {
	dv.contextLines = contextLines
	return dv
}

// Style sets the style for the DiffView.
func (dv *DiffView) Style(style Style) *DiffView {
	dv.style = style
	return dv
}

// LineNumbers sets whether to display line numbers in the DiffView.
func (dv *DiffView) LineNumbers(lineNumbers bool) *DiffView {
	dv.lineNumbers = lineNumbers
	return dv
}

// Height sets the height of the DiffView.
func (dv *DiffView) Height(height int) *DiffView {
	dv.height = height
	return dv
}

// Width sets the width of the DiffView.
func (dv *DiffView) Width(width int) *DiffView {
	dv.width = width
	return dv
}

// XOffset sets the horizontal offset for the DiffView.
func (dv *DiffView) XOffset(xOffset int) *DiffView {
	dv.xOffset = xOffset
	return dv
}

// YOffset sets the vertical offset for the DiffView.
func (dv *DiffView) YOffset(yOffset int) *DiffView {
	dv.yOffset = yOffset
	return dv
}

// InfiniteYScroll allows the YOffset to scroll beyond the last line.
func (dv *DiffView) InfiniteYScroll(infiniteYScroll bool) *DiffView {
	dv.infiniteYScroll = infiniteYScroll
	return dv
}

// TabWidth sets the tab width. Only relevant for code that contains tabs.
func (dv *DiffView) TabWidth(tabWidth int) *DiffView {
	dv.tabWidth = tabWidth
	return dv
}

// ChromaStyle sets the chroma style for syntax highlighting.
func (dv *DiffView) ChromaStyle(style *chroma.Style) *DiffView {
	dv.chromaStyle = style
	dv.clearSyntaxCache()
	return dv
}

// clearSyntaxCache clears the syntax highlighting cache.
func (dv *DiffView) clearSyntaxCache() {
	if dv.syntaxCache != nil {
		for k := range dv.syntaxCache {
			delete(dv.syntaxCache, k)
		}
	}
}
