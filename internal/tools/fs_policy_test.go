package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemPathPolicyRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	work := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("MkdirAll(work) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(work, "link")); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}

	policy, err := ResolveFilesystemPath(work, "link/secret.txt")
	if err != nil {
		t.Fatalf("ResolveFilesystemPath() error = %v", err)
	}
	if !policy.BoundaryKnown {
		t.Fatalf("BoundaryKnown = false, want true for existing symlink target")
	}
	if !policy.EscapesWorkingDir || policy.WithinWorkingDir {
		t.Fatalf("policy = %#v, want symlink escape detected", policy)
	}

	args, _ := json.Marshal(ReadParams{FilePath: "link/secret.txt"})
	result, err := NewReadExecutor().Execute(context.Background(), Call{
		WorkingDir: work,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content, "outside working directory") {
		t.Fatalf("result = %#v, want outside-working-dir tool error", result)
	}
	if !strings.Contains(result.Content, "working directory: "+work) {
		t.Fatalf("result = %#v, want working directory hint", result)
	}
}

func TestFilesystemPathPolicyAllowsAbsolutePathUnderWorkingDir(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	path := filepath.Join(work, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	args, _ := json.Marshal(ReadParams{FilePath: path})
	result, err := NewReadExecutor().Execute(context.Background(), Call{
		WorkingDir: work,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError || !strings.Contains(result.Content, "hello") {
		t.Fatalf("result = %#v, want absolute path under working dir to read", result)
	}
}

func TestFilesystemPathPolicyAllowsAbsolutePathWithoutWorkingDir(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	path := filepath.Join(work, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	args, _ := json.Marshal(ReadParams{FilePath: path})
	result, err := NewReadExecutor().Execute(context.Background(), Call{
		Args: args,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError || !strings.Contains(result.Content, "hello") {
		t.Fatalf("result = %#v, want absolute path to read when working dir is unset", result)
	}
}

func TestMutationWriteRejectsSymlinkTarget(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	target := filepath.Join(work, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink("target.txt", filepath.Join(work, "link.txt")); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}

	args, _ := json.Marshal(WriteParams{FilePath: "link.txt", Content: "replacement"})
	_, err := NewWriteExecutor().Execute(context.Background(), Call{
		WorkingDir: work,
		Approved:   true,
		Args:       args,
	})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Execute() error = %v, want symbolic link validation error", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("target content = %q, want unchanged original", content)
	}
}

func TestWriteApprovalParamsUseCappedPreviews(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	path := filepath.Join(work, "large.txt")
	oldContent := strings.Repeat("o", approvalPreviewMaxBytes*2)
	newContent := strings.Repeat("n", approvalPreviewMaxBytes*2)
	if err := os.WriteFile(path, []byte(oldContent), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	args, _ := json.Marshal(WriteParams{FilePath: "large.txt", Content: newContent})
	needsApproval, err := NewWriteExecutor().Execute(context.Background(), Call{
		WorkingDir: work,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("pre-approval Execute() error = %v", err)
	}
	params, ok := needsApproval.Approval.Params.(WritePermissionsParams)
	if !ok {
		t.Fatalf("approval params type = %T, want WritePermissionsParams", needsApproval.Approval.Params)
	}
	if !params.OldContentTruncated || !params.NewContentTruncated {
		t.Fatalf("truncated flags = old:%v new:%v, want both true", params.OldContentTruncated, params.NewContentTruncated)
	}
	if len(params.OldContent) > approvalPreviewMaxBytes || len(params.NewContent) > approvalPreviewMaxBytes {
		t.Fatalf("preview lengths = old:%d new:%d, want <= %d", len(params.OldContent), len(params.NewContent), approvalPreviewMaxBytes)
	}
	if params.OldContentBytes != len(oldContent) || params.NewContentBytes != len(newContent) {
		t.Fatalf("content bytes = old:%d new:%d, want old:%d new:%d", params.OldContentBytes, params.NewContentBytes, len(oldContent), len(newContent))
	}

	approved, err := NewWriteExecutor().Execute(context.Background(), Call{
		WorkingDir: work,
		Approved:   true,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("approved Execute() error = %v", err)
	}
	if approved.FileVersion == nil || len(approved.FileVersion.OldContent) != len(oldContent) || len(approved.FileVersion.NewContent) != len(newContent) {
		t.Fatalf("approved file version lengths = %#v, want full old/new content", approved.FileVersion)
	}
}

func TestEditApprovalPreviewDoesNotRequireFullOldContent(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	prefix := strings.Repeat("a", approvalPreviewMaxBytes*2)
	fullContent := prefix + "\ntarget\n"
	if err := os.WriteFile(filepath.Join(work, "large.txt"), []byte(fullContent), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	args, _ := json.Marshal(EditParams{FilePath: "large.txt", OldString: "target", NewString: "replacement"})
	needsApproval, err := NewEditExecutor().Execute(context.Background(), Call{
		WorkingDir: work,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("pre-approval Execute() error = %v", err)
	}
	if needsApproval.IsError {
		t.Fatalf("pre-approval result is error: %#v", needsApproval)
	}
	params, ok := needsApproval.Approval.Params.(EditPermissionsParams)
	if !ok {
		t.Fatalf("approval params type = %T, want EditPermissionsParams", needsApproval.Approval.Params)
	}
	if !params.OldContentTruncated || !params.NewContentTruncated {
		t.Fatalf("truncated flags = old:%v new:%v, want both true", params.OldContentTruncated, params.NewContentTruncated)
	}
	if strings.Contains(params.NewContent, "replacement") {
		t.Fatalf("preview unexpectedly includes replacement outside capped window")
	}

	approved, err := NewEditExecutor().Execute(context.Background(), Call{
		WorkingDir: work,
		Approved:   true,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("approved Execute() error = %v", err)
	}
	if approved.FileVersion == nil || !strings.Contains(approved.FileVersion.NewContent, "replacement") {
		t.Fatalf("approved result = %#v, want full-content edit after approval", approved)
	}
}
