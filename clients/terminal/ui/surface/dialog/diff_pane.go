package dialog

import (
	"strings"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/diffview"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const horizontalScrollStep = 5

// FileDiff is one file of a diff dialog.
type FileDiff struct {
	Path       string
	OldContent string
	NewContent string
}

// fileDiffs are the files of tool file changes; a moved file is named by both paths.
func fileDiffs(changes ...tools.FileChange) []FileDiff {
	out := make([]FileDiff, 0, len(changes))
	for _, change := range changes {
		path := prettyPath(change.Path)
		if change.MovedFrom != "" {
			path = prettyPath(change.MovedFrom) + " → " + path
		}
		out = append(out, FileDiff{Path: path, OldContent: change.OldContent, NewContent: change.NewContent})
	}
	return out
}

// diffPane is the diff of some files, each under its path when there are
// several, unified or split and scrolled sideways; its render is cached until
// the width, the mode or the offset changes.
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

func (d *diffPane) render(sty *surfacestyles.Styles, files []FileDiff, width int) string {
	split := 0
	if d.split {
		split = 1
	}
	key := [3]int{width, split, d.xOffset}
	if d.cachedValid && d.cachedKey == key {
		return d.cached
	}
	rendered := make([]string, 0, len(files))
	for _, file := range files {
		formatter := diffview.New().Style(sty.Diff).TabWidth(4).
			Before(file.Path, file.OldContent).
			After(file.Path, file.NewContent).
			XOffset(d.xOffset).
			Width(width)
		if d.split {
			formatter = formatter.Split()
		} else {
			formatter = formatter.Unified()
		}
		if len(files) > 1 {
			rendered = append(rendered, sty.Muted.Render(file.Path)+"\n"+formatter.String())
			continue
		}
		rendered = append(rendered, formatter.String())
	}
	d.cached, d.cachedKey, d.cachedValid = strings.Join(rendered, "\n\n"), key, true
	return d.cached
}
