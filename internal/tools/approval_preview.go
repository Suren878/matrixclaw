package tools

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

const approvalPreviewMaxBytes = 64 * 1024

type approvalContentPreview struct {
	Content   string
	Bytes     int
	Truncated bool
}

func readApprovalContentPreview(path string) (approvalContentPreview, error) {
	file, err := os.Open(path)
	if err != nil {
		return approvalContentPreview{}, err
	}
	defer func() { _ = file.Close() }()
	info, statErr := file.Stat()

	content, err := io.ReadAll(io.LimitReader(file, int64(approvalPreviewMaxBytes)+1))
	if err != nil {
		return approvalContentPreview{}, err
	}
	preview := approvalPreviewString(string(content))
	if statErr == nil && info.Size() > int64(approvalPreviewMaxBytes) {
		preview.Bytes = int(info.Size())
		preview.Truncated = true
		preview.Content = truncateApprovalPreview(string(content), preview.Bytes)
	}
	return preview, nil
}

func approvalPreviewString(content string) approvalContentPreview {
	preview := approvalContentPreview{
		Content: content,
		Bytes:   len(content),
	}
	if len(content) <= approvalPreviewMaxBytes {
		return preview
	}
	preview.Truncated = true
	preview.Content = truncateApprovalPreview(content, len(content))
	return preview
}

func truncateApprovalPreview(content string, fullBytes int) string {
	shownBytes := approvalPreviewMaxBytes
	notice := ""
	maxContentBytes := 0
	for {
		notice = fmt.Sprintf("\n\n[approval preview truncated: showing first %d of %d bytes]", shownBytes, fullBytes)
		maxContentBytes = approvalPreviewMaxBytes - len(notice)
		if maxContentBytes < 0 {
			maxContentBytes = 0
		}
		if maxContentBytes == shownBytes {
			break
		}
		shownBytes = maxContentBytes
	}
	truncated := content[:maxContentBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated + notice
}
