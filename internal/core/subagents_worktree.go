package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func normalizeSubagentIsolation(value string) SubagentIsolation {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(SubagentIsolationShared):
		return SubagentIsolationShared
	case string(SubagentIsolationWorktree):
		return SubagentIsolationWorktree
	default:
		return SubagentIsolationShared
	}
}

func prepareSubagentWorktree(ctx context.Context, workingDir string, taskID string) (string, error) {
	workingDir = normalizeWorkingDir(workingDir)
	taskID = stableIDPart(taskID)
	if workingDir == "" {
		return "", fmt.Errorf("%w: working_dir is required for worktree isolation", ErrInvalidInput)
	}
	if taskID == "" {
		return "", fmt.Errorf("%w: task id is required for worktree isolation", ErrInvalidInput)
	}
	repoRoot, err := gitCommandOutput(ctx, workingDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: worktree isolation requires a git working tree: %v", ErrInvalidInput, err)
	}
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "", fmt.Errorf("%w: git root is empty for worktree isolation", ErrInvalidInput)
	}
	target := filepath.Join(os.TempDir(), "matrixclaw-subagents", stableIDPart(filepath.Base(repoRoot)), taskID)
	if target == "" || target == string(filepath.Separator) {
		return "", fmt.Errorf("%w: invalid worktree target", ErrInvalidInput)
	}
	if _, err := os.Stat(target); err == nil {
		if _, gitErr := gitCommandOutput(ctx, target, "rev-parse", "--show-toplevel"); gitErr == nil {
			return target, nil
		}
		if removeErr := os.RemoveAll(target); removeErr != nil {
			return "", removeErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if _, err := gitCommandOutput(ctx, repoRoot, "worktree", "add", "--detach", target, "HEAD"); err != nil {
		return "", fmt.Errorf("%w: create subagent worktree: %v", ErrExecutionUnavailable, err)
	}
	return target, nil
}

func gitCommandOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message != "" {
			return "", fmt.Errorf("%w: %s", err, message)
		}
		return "", err
	}
	return string(out), nil
}
