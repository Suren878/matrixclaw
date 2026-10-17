package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/glob"
)

const (
	lsToolName       = "ls"
	defaultListDepth = 3
	maxListEntries   = 1000
)

type LSParams struct {
	Path   string   `json:"path,omitempty"`
	Ignore []string `json:"ignore,omitempty"`
	Depth  int      `json:"depth,omitempty"`
}

type LSResponseMetadata struct {
	FilesystemPathMetadata
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type lsExecutor struct{}

func NewLSExecutor() Executor { return &lsExecutor{} }

func (e *lsExecutor) Spec() Spec {
	return coreDefinitionSpec(lsToolName)
}

type lsNode struct {
	name     string
	dir      bool
	children []*lsNode
}

func (e *lsExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params LSParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(lsToolName, err)
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	info, err := os.Stat(policy.Path)
	if err != nil {
		return Result{Content: "path does not exist: " + policy.Path, Metadata: filesystemPathMetadata(policy), Status: ResultStatusError}, nil
	}
	metadata := LSResponseMetadata{FilesystemPathMetadata: filesystemPathMetadata(policy)}
	if !info.IsDir() {
		metadata.NumberOfFiles = 1
		return Result{Content: fmt.Sprintf("%s (file, %d bytes)", filepath.ToSlash(policy.Path), info.Size()), Metadata: metadata}, nil
	}

	scope := newSearchScope(policy, call)
	root, count, truncated, err := listTree(ctx, scope, params)
	if err != nil {
		return Result{Content: err.Error(), Metadata: metadata.FilesystemPathMetadata, Status: ResultStatusError}, nil
	}
	var out strings.Builder
	out.WriteString(strings.TrimSuffix(filepath.ToSlash(policy.Path), "/") + "/\n")
	for i, node := range root.children {
		renderTree(&out, node, "", i == len(root.children)-1)
	}
	if count == 0 {
		out.WriteString("(empty)\n")
	}
	if truncated {
		_, _ = fmt.Fprintf(&out, "\n(Showing the first %d entries, shallower levels first. List a subdirectory or pass a smaller depth.)", maxListEntries)
	}
	metadata.NumberOfFiles, metadata.Truncated = count, truncated
	return Result{Content: strings.TrimRight(out.String(), "\n") + scope.leftNote(), Metadata: metadata}, nil
}

// listTree reads the tree below the scope's root, breadth first so a cut
// keeps the shallow levels, without hidden entries or what ignore matches.
func listTree(ctx context.Context, scope *searchScope, params LSParams) (*lsNode, int, bool, error) {
	depth := params.Depth
	if depth <= 0 {
		depth = defaultListDepth
	}
	var ignores []*glob.Pattern
	for _, pattern := range params.Ignore {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			ignores = append(ignores, glob.CompileFilter(strings.TrimSuffix(filepath.ToSlash(pattern), "/")))
		}
	}
	root := &lsNode{dir: true}
	dirs := map[string]*lsNode{".": root}
	count, truncated := 0, false
	err := walkTree(ctx, scope, func(rel string, entry fs.DirEntry) (bool, bool) {
		if strings.HasPrefix(entry.Name(), ".") || slices.ContainsFunc(ignores, func(p *glob.Pattern) bool { return p.Match(rel) }) {
			return false, false
		}
		return true, entry.IsDir() && strings.Count(rel, "/")+1 < depth
	}, func(rel string, entry fs.DirEntry) error {
		if count == maxListEntries {
			truncated = true
			return errStopWalk
		}
		count++
		node := &lsNode{name: entry.Name(), dir: entry.IsDir()}
		parent := dirs[path.Dir(rel)]
		parent.children = append(parent.children, node)
		if node.dir {
			dirs[rel] = node
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, 0, false, err
	}
	return root, count, truncated, nil
}

func renderTree(out *strings.Builder, node *lsNode, prefix string, last bool) {
	connector := "├── "
	nextPrefix := prefix + "│   "
	if last {
		connector = "└── "
		nextPrefix = prefix + "    "
	}
	out.WriteString(prefix)
	out.WriteString(connector)
	out.WriteString(node.name)
	if node.dir {
		out.WriteByte('/')
	}
	out.WriteByte('\n')
	for i, child := range node.children {
		renderTree(out, child, nextPrefix, i == len(node.children)-1)
	}
}
