package dialog

import (
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/diffview"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const horizontalScrollStep = 5

// diffPane is the diff of one file, unified or split and scrolled sideways; its
// render is cached until the width, the mode or the offset changes.
type diffPane struct {
	split   bool
	xOffset int

	cached      string
	cachedKey   [3]int
	cachedValid bool
}

func (d *diffPane) toggleMode()  { d.split = !d.split }
func (d *diffPane) scrollLeft()  { d.xOffset = max(0, d.xOffset-horizontalScrollStep) }
func (d *diffPane) scrollRight() { d.xOffset += horizontalScrollStep }

func (d *diffPane) render(sty *surfacestyles.Styles, path string, oldContent string, newContent string, width int) string {
	split := 0
	if d.split {
		split = 1
	}
	key := [3]int{width, split, d.xOffset}
	if d.cachedValid && d.cachedKey == key {
		return d.cached
	}
	formatter := diffview.New().Style(sty.Diff).TabWidth(4).
		Before(prettyPath(path), oldContent).
		After(prettyPath(path), newContent).
		XOffset(d.xOffset).
		Width(width)
	if d.split {
		formatter = formatter.Split()
	} else {
		formatter = formatter.Unified()
	}
	d.cached, d.cachedKey, d.cachedValid = formatter.String(), key, true
	return d.cached
}
