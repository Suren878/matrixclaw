package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func (e *lsExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params LSParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(lsToolName, err)
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	root := policy.Path
	output, metadata, err := listDirectoryTree(root, params)
	if err != nil {
		return Result{Content: err.Error(), Metadata: filesystemPathMetadata(policy), IsError: true}, nil
	}
	metadata.FilesystemPathMetadata = filesystemPathMetadata(policy)
	return Result{
		Content:  output,
		Metadata: metadata,
	}, nil
}

func listDirectoryTree(root string, params LSParams) (string, LSResponseMetadata, error) {
	if _, err := os.Stat(root); err != nil {
		return "", LSResponseMetadata{}, fmt.Errorf("path does not exist: %s", root)
	}
	depth := params.Depth
	if depth <= 0 {
		depth = defaultListDepth
	}
	nodes := []*TreeNode{}
	index := map[string]*TreeNode{}
	count := 0
	truncated := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if shouldSkipHidden(path, root) || matchesAnyIgnore(path, root, params.Ignore) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		level := strings.Count(filepath.ToSlash(rel), "/") + 1
		if level > depth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count > maxListEntries {
			truncated = true
			return errStopWalk
		}
		node := &TreeNode{
			Name: filepath.Base(path),
			Path: filepath.ToSlash(path),
			Type: NodeTypeFile,
		}
		if d.IsDir() {
			node.Type = NodeTypeDirectory
			node.Children = []*TreeNode{}
		}
		index[rel] = node
		parentRel := filepath.Dir(rel)
		if parentRel == "." {
			nodes = append(nodes, node)
			return nil
		}
		if parent, ok := index[parentRel]; ok {
			parent.Children = append(parent.Children, node)
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return "", LSResponseMetadata{}, err
	}

	var out strings.Builder
	for i, node := range nodes {
		renderTree(&out, node, "", i == len(nodes)-1)
	}
	output := out.String()
	if truncated {
		output += "\n\n(Results are truncated. Consider using a more specific path.)"
	}
	return output, LSResponseMetadata{
		NumberOfFiles: count,
		Truncated:     truncated,
	}, nil
}

func renderTree(out *strings.Builder, node *TreeNode, prefix string, last bool) {
	connector := "├── "
	nextPrefix := prefix + "│   "
	if last {
		connector = "└── "
		nextPrefix = prefix + "    "
	}
	out.WriteString(prefix)
	out.WriteString(connector)
	out.WriteString(node.Name)
	out.WriteByte('\n')
	for i, child := range node.Children {
		renderTree(out, child, nextPrefix, i == len(node.Children)-1)
	}
}
