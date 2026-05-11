package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

func approvalResult(toolID string, action string, path string, description string, params any) Result {
	normalizedPath := normalizeApprovalPath(path)
	return Result{
		Content: "Approval required",
		Approval: &ApprovalRequest{
			ToolID:      toolID,
			Action:      action,
			Path:        normalizedPath,
			Description: description,
			Params:      params,
		},
	}
}

func normalizeApprovalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return filepath.Clean(path)
		}
		return filepath.Dir(filepath.Clean(path))
	}
	return filepath.Dir(filepath.Clean(path))
}

func relativeDisplayPath(workingDir string, path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}
	if strings.TrimSpace(workingDir) == "" {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(workingDir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func buildUnifiedDiff(displayPath string, oldContent string, newContent string) (string, int, int) {
	oldContent = strings.ReplaceAll(oldContent, "\r\n", "\n")
	newContent = strings.ReplaceAll(newContent, "\r\n", "\n")
	edits := udiff.Lines(oldContent, newContent)
	unified, err := udiff.ToUnifiedDiff(displayPath, displayPath, oldContent, edits, 3)
	if err != nil {
		return "", 0, 0
	}
	additions := 0
	removals := 0
	for _, hunk := range unified.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case udiff.Insert:
				additions++
			case udiff.Delete:
				removals++
			}
		}
	}
	return strings.TrimRight(unified.String(), "\n"), additions, removals
}
