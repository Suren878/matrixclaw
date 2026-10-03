package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aymanbagabas/go-udiff"
)

const (
	// approvalPreviewMaxBytes bounds each text an approval shows.
	approvalPreviewMaxBytes = 64 * 1024
	// previewContextLines and previewContextBytes bound the unchanged text
	// shown around each change of a file too large to show whole.
	previewContextLines = 3
	previewContextBytes = 512
)

// preview is the change as an approval shows it. A side too large to show
// whole keeps only the changes and their context, the unchanged runs between
// them replaced by the same note on both sides, so every change stays in view.
func (c FileChange) preview() FileChange {
	if len(c.OldContent) > approvalPreviewMaxBytes || len(c.NewContent) > approvalPreviewMaxBytes {
		c.OldContent, c.NewContent = changedRegions(c.OldContent, c.NewContent)
	}
	c.OldContent, c.NewContent = cutPreview(c.OldContent), cutPreview(c.NewContent)
	return c
}

// changedRegions is both sides reduced to the windows around their edits.
func changedRegions(oldContent string, newContent string) (string, string) {
	edits := udiff.Strings(oldContent, newContent)
	if len(edits) == 0 {
		return oldContent, newContent
	}
	var oldOut, newOut strings.Builder
	shown := 0
	for i := 0; i < len(edits); {
		start, end := contextStart(oldContent, edits[i].Start), contextEnd(oldContent, edits[i].End)
		j := i
		for j < len(edits) && contextStart(oldContent, edits[j].Start) <= end {
			end = max(end, contextEnd(oldContent, edits[j].End))
			j++
		}
		note := elidedNote(oldContent[shown:start])
		oldOut.WriteString(note)
		newOut.WriteString(note)
		oldOut.WriteString(oldContent[start:end])
		at := start
		for _, edit := range edits[i:j] {
			newOut.WriteString(oldContent[at:edit.Start])
			newOut.WriteString(edit.New)
			at = edit.End
		}
		newOut.WriteString(oldContent[at:end])
		shown, i = end, j
	}
	note := elidedNote(oldContent[shown:])
	oldOut.WriteString(note)
	newOut.WriteString(note)
	return oldOut.String(), newOut.String()
}

// contextStart is where the context before offset begins: previewContextLines
// whole lines back, but never more than previewContextBytes.
func contextStart(content string, offset int) int {
	start := strings.LastIndexByte(content[:offset], '\n') + 1
	for range previewContextLines {
		if start == 0 {
			break
		}
		start = strings.LastIndexByte(content[:start-1], '\n') + 1
	}
	if offset-start > previewContextBytes {
		start = offset - previewContextBytes
		for !utf8.RuneStart(content[start]) {
			start++
		}
	}
	return start
}

// contextEnd is where the context after offset ends, bounded like contextStart.
func contextEnd(content string, offset int) int {
	end, lines := offset, previewContextLines
	if end > 0 && content[end-1] != '\n' {
		lines++
	}
	for range lines {
		next := strings.IndexByte(content[end:], '\n')
		if next < 0 {
			end = len(content)
			break
		}
		end += next + 1
	}
	if end-offset > previewContextBytes {
		end = offset + previewContextBytes
		for !utf8.RuneStart(content[end]) {
			end--
		}
	}
	return end
}

// elidedNote stands for an unchanged run left out of a preview.
func elidedNote(run string) string {
	switch lines := strings.Count(run, "\n"); {
	case run == "":
		return ""
	case strings.HasSuffix(run, "\n") && lines > 0:
		return fmt.Sprintf("[… %d unchanged lines …]\n", lines)
	default:
		return fmt.Sprintf("[… %d unchanged bytes …]", len(run))
	}
}

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
