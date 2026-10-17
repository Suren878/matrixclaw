package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/glob"
)

const (
	globToolName   = "glob"
	globMatchLimit = 100
)

type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type GlobResponseMetadata struct {
	FilesystemPathMetadata
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type globExecutor struct{}

func NewGlobExecutor() Executor { return &globExecutor{} }

func (e *globExecutor) Spec() Spec {
	return coreDefinitionSpec(globToolName)
}

func (e *globExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params GlobParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(globToolName, err)
	}
	pattern := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(params.Pattern)), "./")
	switch {
	case pattern == "":
		return Result{Content: "pattern is required", Status: ResultStatusError}, nil
	case path.IsAbs(pattern) || slices.Contains(strings.Split(pattern, "/"), ".."):
		return Result{Content: "pattern is matched against paths relative to path: put the directory in path and keep the pattern relative, e.g. path \"/src\" and pattern \"**/*.go\"", Status: ResultStatusError}, nil
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	if info, err := os.Stat(policy.Path); err != nil || !info.IsDir() {
		return Result{Content: "path is not a directory: " + policy.Path, Metadata: filesystemPathMetadata(policy), Status: ResultStatusError}, nil
	}
	scope := newSearchScope(policy, call)
	files, total, err := globFiles(ctx, scope, pattern)
	if err != nil {
		return Result{}, fmt.Errorf("glob: %w", err)
	}

	content := "No files found"
	if len(files) > 0 {
		paths := make([]string, len(files))
		for i, rel := range files {
			paths[i] = filepath.ToSlash(filepath.Join(policy.Path, rel))
		}
		content = strings.Join(paths, "\n")
		if total > len(files) {
			content += fmt.Sprintf("\n\n(Showing the %d most recently modified of %d files. Narrow path or pattern to see the rest.)", len(files), total)
		}
	}
	return Result{
		Content: content + scope.leftNote(),
		Metadata: GlobResponseMetadata{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			NumberOfFiles:          total,
			Truncated:              total > len(files),
		},
	}, nil
}

// globFiles finds the files matching pattern below the scope's root, most
// recently modified first, and returns at most globMatchLimit of them with the
// total. The walk starts at the pattern's literal base, which counts as named.
func globFiles(ctx context.Context, scope *searchScope, pattern string) ([]string, int, error) {
	compiled := glob.Compile(pattern)
	base := glob.Base(pattern)
	walk := scope
	if base != "" {
		if info, err := os.Stat(filepath.Join(scope.root, filepath.FromSlash(base))); err != nil || !info.IsDir() || scope.dropsPath(base) {
			return nil, 0, nil
		}
		walk = scope.below(base)
	}
	full := func(rel string) string { return path.Join(base, rel) }
	rels, err := searchFiles(ctx, walk,
		func(rel string) bool { return compiled.Reaches(full(rel)) },
		func(rel string) bool { return compiled.MatchVisible(full(rel)) })
	if err != nil {
		return nil, 0, err
	}
	found := statRecent(walk.root, rels)
	files := make([]string, 0, min(len(found), globMatchLimit))
	for _, file := range found[:min(len(found), globMatchLimit)] {
		files = append(files, full(file.rel))
	}
	return files, len(found), nil
}
