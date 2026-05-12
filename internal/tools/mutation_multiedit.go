package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func (e *multiEditExecutor) Spec() Spec {
	return Spec{
		ID:              multiEditToolName,
		Name:            "MultiEdit",
		Description:     "Apply several edits to one file",
		Risk:            RiskApproval,
		Namespace:       namespaceCoreFilesystem,
		Category:        CategoryFilesystem,
		Profiles:        []Profile{ProfileCoding},
		OutputKind:      OutputDiff,
		InputJSONSchema: multiEditInputSchema,
	}
}

func (e *multiEditExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params MultiEditParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(multiEditToolName, err)
	}
	if strings.TrimSpace(params.FilePath) == "" {
		return Result{Content: "file_path is required", IsError: true}, nil
	}
	if len(params.Edits) == 0 {
		return Result{Content: "edits is required", IsError: true}, nil
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
			return Result{}, fmt.Errorf("multiedit: read file preview: %w", err)
		}
		currentPreview := oldPreview.Content
		previewExact := !oldPreview.Truncated
		for _, edit := range params.Edits {
			next, errResult := applyEditPreview(currentPreview, !previewExact, edit.OldString, edit.NewString, edit.ReplaceAll)
			if errResult != nil {
				return *errResult, nil
			}
			if !previewExact && edit.OldString == "" {
				previewExact = true
			}
			currentPreview = next
		}
		newPreview := approvalPreviewString(currentPreview)
		newContentBytes := newPreview.Bytes
		if !previewExact {
			newContentBytes = 0
		}
		return approvalResult(multiEditToolName, "multiedit", path, "Apply multiple edits to "+path, MultiEditPermissionsParams{
			FilePath:            path,
			OldContent:          oldPreview.Content,
			NewContent:          newPreview.Content,
			OldContentTruncated: oldPreview.Truncated,
			NewContentTruncated: !previewExact || newPreview.Truncated,
			OldContentBytes:     oldPreview.Bytes,
			NewContentBytes:     newContentBytes,
		}), nil
	}

	oldContentBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Content: fmt.Sprintf("File not found: %s", path), IsError: true}, nil
		}
		return Result{}, fmt.Errorf("multiedit: read file: %w", err)
	}
	oldContent := string(oldContentBytes)
	current := oldContent
	applied := 0
	for _, edit := range params.Edits {
		next, errResult := applyEdit(current, edit.OldString, edit.NewString, edit.ReplaceAll)
		if errResult != nil {
			return *errResult, nil
		}
		current = next
		applied++
	}
	if err := ensureMutationWriteTarget(path, false); err != nil {
		return Result{}, fmt.Errorf("multiedit: validate target: %w", err)
	}
	if err := os.WriteFile(path, []byte(current), 0o644); err != nil {
		return Result{}, fmt.Errorf("multiedit: write file: %w", err)
	}

	displayPath := relativeDisplayPath(call.WorkingDir, path)
	diffText, additions, removals := buildUnifiedDiff(displayPath, oldContent, current)
	return Result{
		Content: fmt.Sprintf("Applied %d edits to %s", applied, path),
		Metadata: MultiEditResponseMetadata{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			Diff:                   diffText,
			Additions:              additions,
			Removals:               removals,
			EditsApplied:           applied,
			OldContent:             oldContent,
			NewContent:             current,
		},
		FileVersion: &FileVersion{
			Path:       path,
			OldContent: oldContent,
			NewContent: current,
			Diff:       diffText,
			Additions:  additions,
			Removals:   removals,
		},
	}, nil
}
