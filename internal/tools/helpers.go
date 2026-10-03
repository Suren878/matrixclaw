package tools

import (
	"path/filepath"
	"strings"
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
	return filepath.Clean(path)
}
