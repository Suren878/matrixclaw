package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/aymanbagabas/go-udiff"
)

const (
	// approvalPreviewMaxBytes bounds each side an approval shows; a file up to
	// that size is shown whole.
	approvalPreviewMaxBytes = 64 * 1024
	// resultChangeMaxBytes bounds each side a result keeps; a result keeps
	// only the changed regions, never a whole file.
	resultChangeMaxBytes = 16 * 1024
	// previewContextLines and previewContextBytes bound the unchanged text
	// kept around each change.
	previewContextLines = 3
	previewContextBytes = 512
	// snippetMaxLines and snippetLineRunes bound the numbered lines an
	// edit's result or a miss hint shows.
	snippetMaxLines  = 60
	snippetLineRunes = 300
)

// FileChange is what write, edit, multiedit or apply_patch does to one file,
// as their approval shows it and their result's metadata keeps it. A side over
// the size kept whole holds only the changes and their context, the unchanged
// runs between them replaced by the same note on both sides. MovedFrom is the
// file a patch moved here.
type FileChange struct {
	Path       string `json:"file_path"`
	MovedFrom  string `json:"moved_from,omitempty"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
}

// newFileChange is the change from old to next at path: sides up to whole
// bytes are kept whole, larger ones reduced to their changed regions, and
// each is cut to limit bytes.
func newFileChange(path string, old string, next string, diff udiff.UnifiedDiff, whole int, limit int) FileChange {
	change := FileChange{Path: path, OldContent: old, NewContent: next}
	change.Additions, change.Removals = diffCounts(diff)
	if len(old) > whole || len(next) > whole {
		change.OldContent, change.NewContent = changedRegions(old, next)
	}
	change.OldContent, change.NewContent = cutText(change.OldContent, limit), cutText(change.NewContent, limit)
	return change
}

// lineDiff is the line diff from old to next with line ends normalized.
func lineDiff(old string, next string) udiff.UnifiedDiff {
	old, next = strings.ReplaceAll(old, "\r\n", "\n"), strings.ReplaceAll(next, "\r\n", "\n")
	diff, err := udiff.ToUnifiedDiff("a", "b", old, udiff.Lines(old, next), previewContextLines)
	if err != nil {
		return udiff.UnifiedDiff{}
	}
	return diff
}

// diffCounts counts the lines diff adds and removes.
func diffCounts(diff udiff.UnifiedDiff) (int, int) {
	additions, removals := 0, 0
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case udiff.Insert:
				additions++
			case udiff.Delete:
				removals++
			}
		}
	}
	return additions, removals
}

// changeSnippet is the new side of each hunk of diff, numbered like read
// output, at most snippetMaxLines lines.
func changeSnippet(diff udiff.UnifiedDiff) string {
	var out strings.Builder
	shown, left := 0, 0
	for i, hunk := range diff.Hunks {
		number := hunk.ToLine
		if i > 0 && shown < snippetMaxLines {
			out.WriteString("     …\n")
		}
		for _, line := range hunk.Lines {
			if line.Kind == udiff.Delete {
				continue
			}
			if shown == snippetMaxLines {
				left++
				continue
			}
			fmt.Fprintf(&out, "%6d\t%s\n", number, clipLine(strings.TrimSuffix(line.Content, "\n")))
			shown++
			number++
		}
	}
	if left > 0 {
		fmt.Fprintf(&out, "(%d more changed lines not shown; read the file to see them)\n", left)
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// clipLine cuts a line shown in a snippet or hint to snippetLineRunes.
func clipLine(line string) string {
	return textutil.Clip(line, snippetLineRunes, "…")
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

// cutText is content cut to limit bytes, notice included.
func cutText(content string, limit int) string {
	if len(content) <= limit {
		return content
	}
	shown := limit
	var notice string
	for {
		notice = fmt.Sprintf("\n\n[truncated: showing first %d of %d bytes]", shown, len(content))
		fits := max(limit-len(notice), 0)
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
