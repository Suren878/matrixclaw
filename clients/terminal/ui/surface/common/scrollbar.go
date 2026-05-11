package common

import (
	"strings"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

// Scrollbar renders a vertical scrollbar based on content and viewport size.
func Scrollbar(s *styles.Styles, height, contentSize, viewportSize, offset int) string {
	if height <= 0 || contentSize <= viewportSize {
		return ""
	}

	thumbSize := max(1, height*viewportSize/contentSize)
	maxOffset := contentSize - viewportSize
	if maxOffset <= 0 {
		return ""
	}

	trackSpace := height - thumbSize
	thumbPos := 0
	if trackSpace > 0 && maxOffset > 0 {
		thumbPos = min(trackSpace, offset*trackSpace/maxOffset)
	}

	var sb strings.Builder
	for i := range height {
		if i > 0 {
			sb.WriteString("\n")
		}
		if i >= thumbPos && i < thumbPos+thumbSize {
			sb.WriteString(s.Dialog.ScrollbarThumb.Render(styles.ScrollbarThumb))
		} else {
			sb.WriteString(s.Dialog.ScrollbarTrack.Render(styles.ScrollbarTrack))
		}
	}

	return sb.String()
}
