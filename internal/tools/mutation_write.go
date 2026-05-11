package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (e *writeExecutor) Spec() Spec {
	return Spec{
		ID:              writeToolName,
		Name:            "Write",
		Description:     "Create or replace a file",
		Risk:            RiskApproval,
		InputJSONSchema: writeInputSchema,
	}
}

func (e *writeExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params WriteParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(writeToolName, err)
	}
	if strings.TrimSpace(params.FilePath) == "" {
		return Result{Content: "file_path is required", IsError: true}, nil
	}
	policy, errResult := resolvePathUnderWorkingDir(call.WorkingDir, params.FilePath)
	if errResult != nil {
		return *errResult, nil
	}
	path := policy.Path
	if !call.Approved {
		oldPreview := approvalContentPreview{}
		if preview, err := readApprovalContentPreview(path); err == nil {
			oldPreview = preview
		} else if !os.IsNotExist(err) {
			return Result{}, fmt.Errorf("write: read old file preview: %w", err)
		}
		newPreview := approvalPreviewString(params.Content)
		return approvalResult(writeToolName, "write", path, "Create or replace "+path, WritePermissionsParams{
			FilePath:            path,
			OldContent:          oldPreview.Content,
			NewContent:          newPreview.Content,
			OldContentTruncated: oldPreview.Truncated,
			NewContentTruncated: newPreview.Truncated,
			OldContentBytes:     oldPreview.Bytes,
			NewContentBytes:     newPreview.Bytes,
		}), nil
	}
	oldContent := ""
	if content, err := os.ReadFile(path); err == nil {
		oldContent = string(content)
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("write: read old file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("write: create dir: %w", err)
	}
	if err := ensureMutationWriteTarget(path, true); err != nil {
		return Result{}, fmt.Errorf("write: validate target: %w", err)
	}
	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return Result{}, fmt.Errorf("write: write file: %w", err)
	}

	displayPath := relativeDisplayPath(call.WorkingDir, path)
	diffText, additions, removals := buildUnifiedDiff(displayPath, oldContent, params.Content)
	return Result{
		Content: fmt.Sprintf("File written: %s", path),
		Metadata: WriteResponseMetadata{
			Diff:       diffText,
			Additions:  additions,
			Removals:   removals,
			OldContent: oldContent,
			NewContent: params.Content,
		},
		FileVersion: &FileVersion{
			Path:       path,
			OldContent: oldContent,
			NewContent: params.Content,
			Diff:       diffText,
			Additions:  additions,
			Removals:   removals,
		},
	}, nil
}
