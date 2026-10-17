package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FilesystemPathPolicy is a tool's path argument resolved against the call's
// working directory: Path is absolute, RealPath also has the symlinks of its
// existing part resolved.
type FilesystemPathPolicy struct {
	Input    string
	Path     string
	RealPath string
}

// FilesystemPathMetadata is the path part of a file tool's result metadata.
type FilesystemPathMetadata struct {
	RequestedPath string `json:"requested_path,omitempty"`
	ResolvedPath  string `json:"resolved_path,omitempty"`
}

// ResolveFilesystemPath is the one resolver of tool paths: value relative to
// workingDir (the daemon's directory when empty), or workingDir itself when
// value is empty.
func ResolveFilesystemPath(workingDir string, value string) (FilesystemPathPolicy, error) {
	wd := strings.TrimSpace(workingDir)
	if wd == "" {
		wd = "."
	}
	path := wd
	if input := strings.TrimSpace(value); input != "" {
		path = input
		if !filepath.IsAbs(input) {
			path = filepath.Join(wd, input)
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return FilesystemPathPolicy{}, err
	}
	realPath, _ := evalExistingPathPrefix(path)
	return FilesystemPathPolicy{Input: value, Path: path, RealPath: realPath}, nil
}

func filesystemPathMetadata(policy FilesystemPathPolicy) FilesystemPathMetadata {
	return FilesystemPathMetadata{RequestedPath: policy.Input, ResolvedPath: policy.Path}
}

func resolveReadablePath(workingDir string, value string) (FilesystemPathPolicy, *Result) {
	policy, err := ResolveFilesystemPath(workingDir, value)
	if err != nil {
		return FilesystemPathPolicy{}, &Result{Content: fmt.Sprintf("Invalid path: %v", err), Status: ResultStatusError}
	}
	return policy, nil
}

// comparableFilesystemPath is path made absolute with symlinks resolved, or ""
// for an empty path.
func comparableFilesystemPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	realPath, _ := evalExistingPathPrefix(absPath)
	return realPath
}

// evalExistingPathPrefix resolves the symlinks of the longest existing prefix
// of path and appends the rest; known is false when that failed.
func evalExistingPathPrefix(path string) (string, bool) {
	path = filepath.Clean(path)
	rest := ""
	for current := path; ; {
		real, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Join(real, rest), true
		}
		parent := filepath.Dir(current)
		if !os.IsNotExist(err) || parent == current {
			return path, false
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}
