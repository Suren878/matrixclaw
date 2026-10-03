package tools

import (
	"fmt"
	"unicode/utf8"
)

// approvalPreviewMaxBytes bounds each text an approval shows.
const approvalPreviewMaxBytes = 64 * 1024

// cutPreview is content cut to approvalPreviewMaxBytes, notice included.
func cutPreview(content string) string {
	if len(content) <= approvalPreviewMaxBytes {
		return content
	}
	shown := approvalPreviewMaxBytes
	var notice string
	for {
		notice = fmt.Sprintf("\n\n[approval preview truncated: showing first %d of %d bytes]", shown, len(content))
		fits := max(approvalPreviewMaxBytes-len(notice), 0)
		if fits == shown {
			break
		}
		shown = fits
	}
	cut := content[:shown]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + notice
}
