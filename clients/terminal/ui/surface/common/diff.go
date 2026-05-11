package common

import (
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/diffview"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

// DiffFormatter returns a diff formatter with the given styles.
func DiffFormatter(s *styles.Styles) *diffview.DiffView {
	formatDiff := diffview.New()
	return formatDiff.Style(s.Diff).TabWidth(4)
}
