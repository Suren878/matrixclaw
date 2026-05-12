package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

func (e *globExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params GlobParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(globToolName, err)
	}
	if strings.TrimSpace(params.Pattern) == "" {
		return Result{Content: "pattern is required", IsError: true}, nil
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	root := policy.Path
	files, truncated, err := globFiles(ctx, params.Pattern, root, defaultSearchLimit)
	if err != nil {
		return Result{}, fmt.Errorf("glob: %w", err)
	}

	content := "No files found"
	if len(files) > 0 {
		content = strings.Join(files, "\n")
		if truncated {
			content += "\n\n(Results are truncated. Consider using a more specific path or pattern.)"
		}
	}
	return Result{
		Content: content,
		Metadata: GlobResponseMetadata{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			NumberOfFiles:          len(files),
			Truncated:              truncated,
		},
	}, nil
}

func globFiles(_ context.Context, pattern string, root string, limit int) ([]string, bool, error) {
	regex, err := globToRegexp(pattern)
	if err != nil {
		return nil, false, err
	}
	matches := make([]string, 0, limit)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if shouldSkipHidden(path, root) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if regex.MatchString(rel) {
			matches = append(matches, filepath.ToSlash(path))
			if limit > 0 && len(matches) >= limit {
				return errStopWalk
			}
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return nil, false, err
	}
	sort.Strings(matches)
	return matches, limit > 0 && len(matches) >= limit, nil
}
