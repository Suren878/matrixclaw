package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func (e *editExecutor) Spec() Spec {
	return coreDefinitionSpec(editToolName)
}

func (e *editExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params EditParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(editToolName, err)
	}
	if strings.TrimSpace(params.FilePath) == "" {
		return Result{Content: "file_path is required", IsError: true}, nil
	}
	policy, pathErr := resolveMutationPath(call.WorkingDir, params.FilePath)
	if pathErr != nil {
		return *pathErr, nil
	}
	path := policy.Path

	if !call.Approved {
		oldPreview, err := readApprovalContentPreview(path)
		if err != nil {
			if os.IsNotExist(err) {
				return Result{Content: fmt.Sprintf("File not found: %s", path), IsError: true}, nil
			}
			return Result{}, fmt.Errorf("edit: read file preview: %w", err)
		}
		newPreviewContent, errResult := applyEditPreview(oldPreview.Content, oldPreview.Truncated, params.OldString, params.NewString, params.ReplaceAll)
		if errResult != nil {
			return *errResult, nil
		}
		newPreview := approvalPreviewString(newPreviewContent)
		newPreviewExact := !oldPreview.Truncated || params.OldString == ""
		newContentBytes := newPreview.Bytes
		if !newPreviewExact {
			newContentBytes = 0
		}
		return approvalResult(editToolName, "edit", path, "Edit "+path, EditPermissionsParams{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			FilePath:               path,
			OldContent:             oldPreview.Content,
			NewContent:             newPreview.Content,
			OldContentTruncated:    oldPreview.Truncated,
			NewContentTruncated:    !newPreviewExact || newPreview.Truncated,
			OldContentBytes:        oldPreview.Bytes,
			NewContentBytes:        newContentBytes,
		}), nil
	}

	oldContentBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Content: fmt.Sprintf("File not found: %s", path), IsError: true}, nil
		}
		return Result{}, fmt.Errorf("edit: read file: %w", err)
	}
	oldContent := string(oldContentBytes)
	newContent, errResult := applyEdit(oldContent, params.OldString, params.NewString, params.ReplaceAll)
	if errResult != nil {
		return *errResult, nil
	}
	if err := ensureMutationWriteTarget(path, false); err != nil {
		return Result{}, fmt.Errorf("edit: validate target: %w", err)
	}
	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return Result{}, fmt.Errorf("edit: write file: %w", err)
	}

	displayPath := relativeDisplayPath(call.WorkingDir, path)
	diffText, additions, removals := buildUnifiedDiff(displayPath, oldContent, newContent)
	return Result{
		Content: fmt.Sprintf("File edited: %s", path),
		Metadata: EditResponseMetadata{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			Diff:                   diffText,
			Additions:              additions,
			Removals:               removals,
			OldContent:             oldContent,
			NewContent:             newContent,
		},
		FileVersion: &FileVersion{
			Path:       path,
			OldContent: oldContent,
			NewContent: newContent,
			Diff:       diffText,
			Additions:  additions,
			Removals:   removals,
		},
	}, nil
}

func applyEdit(content string, oldString string, newString string, replaceAll bool) (string, *Result) {
	if oldString == "" {
		return newString, nil
	}
	if !strings.Contains(content, oldString) {
		result := Result{Content: "old_string not found in file", IsError: true}
		return "", &result
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldString, newString), nil
	}
	first := strings.Index(content, oldString)
	last := strings.LastIndex(content, oldString)
	if first != last {
		result := Result{Content: "old_string appears multiple times in the file; set replace_all to true or provide more context", IsError: true}
		return "", &result
	}
	return content[:first] + newString + content[first+len(oldString):], nil
}

func applyEditPreview(content string, truncated bool, oldString string, newString string, replaceAll bool) (string, *Result) {
	if !truncated {
		return applyEdit(content, oldString, newString, replaceAll)
	}
	if oldString == "" {
		return newString, nil
	}
	if !strings.Contains(content, oldString) {
		return content, nil
	}
	return applyEdit(content, oldString, newString, replaceAll)
}
