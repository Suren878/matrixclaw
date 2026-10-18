package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/repomap"
)

const (
	repoMapToolName  = "repo_map"
	repoMapMinTokens = 1024
	repoMapMaxTokens = 4096
	// repoMapFileLimit bounds the files one map reads: the most recently
	// modified ones.
	repoMapFileLimit = 5000
)

type RepoMapParams struct {
	Path         string   `json:"path,omitempty"`
	FocusFiles   []string `json:"focus_files,omitempty"`
	FocusSymbols []string `json:"focus_symbols,omitempty"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
}

type RepoMapResponseMetadata struct {
	FilesystemPathMetadata
	MappedFiles int `json:"mapped_files"`
	ShownFiles  int `json:"shown_files"`
	Tokens      int `json:"tokens"`
}

// repoMapExecutor keeps one Mapper, so repeated maps of a tree reuse its tags.
type repoMapExecutor struct {
	mapper repomap.Mapper
}

func NewRepoMapExecutor() Executor { return &repoMapExecutor{} }

func (e *repoMapExecutor) Spec() Spec {
	return coreDefinitionSpec(repoMapToolName)
}

func (e *repoMapExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params RepoMapParams
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &params); err != nil {
			return Result{}, InvalidArgs(repoMapToolName, err)
		}
	}
	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	if info, err := os.Stat(policy.Path); err != nil || !info.IsDir() {
		return Result{Content: "path is not a directory: " + policy.Path, Metadata: filesystemPathMetadata(policy), Status: ResultStatusError}, nil
	}
	scope := newSearchScope(policy, call)
	visible := func(rel string) bool { return !hiddenElement(rel) }
	rels, err := searchFiles(ctx, scope, visible, func(rel string) bool { return visible(rel) && repomap.Supported(rel) })
	if err != nil {
		return Result{}, fmt.Errorf("repo_map: %w", err)
	}
	found := statRecent(policy.Path, rels)
	files := make([]repomap.File, 0, min(len(found), repoMapFileLimit))
	for _, file := range found[:min(len(found), repoMapFileLimit)] {
		files = append(files, repomap.File{Path: file.rel, Size: file.info.Size(), ModTime: file.info.ModTime()})
	}
	var focus []string
	for _, name := range params.FocusFiles {
		if rel, err := filepath.Rel(policy.Path, resolvedPath(call.WorkingDir, name)); err == nil && filepath.IsLocal(rel) {
			focus = append(focus, filepath.ToSlash(rel))
		}
	}
	tokens := 0
	if params.MaxTokens > 0 {
		tokens = min(max(params.MaxTokens, repoMapMinTokens), repoMapMaxTokens)
	}
	drawn, err := e.mapper.Map(ctx, repomap.Request{
		Root:         policy.Path,
		Files:        files,
		FocusFiles:   focus,
		FocusSymbols: params.FocusSymbols,
		Tokens:       tokens,
	})
	if err != nil {
		return Result{}, fmt.Errorf("repo_map: %w", err)
	}

	var content strings.Builder
	if drawn.Text == "" {
		fmt.Fprintf(&content, "No definitions found in %s (%d source files).", filepath.ToSlash(policy.Path), len(files))
	} else {
		fmt.Fprintf(&content, "Map of %s: definitions from %d of %d source files, most important first; each line is `line│definition`.\n\n%s",
			filepath.ToSlash(policy.Path), drawn.Shown, drawn.Mapped, drawn.Text)
	}
	if len(found) > len(files) {
		fmt.Fprintf(&content, "\n(Only the %d most recently modified of %d source files were read. Map a subdirectory to see the rest.)", len(files), len(found))
	}
	return Result{
		Content: content.String() + scope.leftNote(),
		Metadata: RepoMapResponseMetadata{
			FilesystemPathMetadata: filesystemPathMetadata(policy),
			MappedFiles:            drawn.Mapped,
			ShownFiles:             drawn.Shown,
			Tokens:                 drawn.Tokens,
		},
	}, nil
}
