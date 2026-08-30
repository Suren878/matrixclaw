package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type FilesystemPathPolicy struct {
	Input              string
	WorkingDir         string
	Path               string
	RealPath           string
	RealWorkingDir     string
	BoundaryKnown      bool
	WithinWorkingDir   bool
	EscapesWorkingDir  bool
	SymlinkEvalApplied bool
}

type FilesystemPathMetadata struct {
	RequestedPath      string `json:"requested_path,omitempty"`
	ResolvedPath       string `json:"resolved_path,omitempty"`
	WorkingDir         string `json:"working_dir,omitempty"`
	RealPath           string `json:"real_path,omitempty"`
	RealWorkingDir     string `json:"real_working_dir,omitempty"`
	BoundaryKnown      bool   `json:"boundary_known,omitempty"`
	WithinWorkingDir   bool   `json:"within_working_dir,omitempty"`
	SymlinkEvalApplied bool   `json:"symlink_eval_applied,omitempty"`
}

func ResolveFilesystemPath(workingDir string, value string) (FilesystemPathPolicy, error) {
	wd := strings.TrimSpace(workingDir)
	if wd == "" {
		wd = "."
	}
	absWD, err := filepath.Abs(filepath.Clean(wd))
	if err != nil {
		return FilesystemPathPolicy{}, err
	}

	input := strings.TrimSpace(value)
	path := absWD
	if input != "" {
		if filepath.IsAbs(input) {
			path = filepath.Clean(input)
		} else {
			path = filepath.Join(absWD, filepath.Clean(input))
		}
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return FilesystemPathPolicy{}, err
	}

	realWD, wdKnown := evalExistingPathPrefix(absWD)
	realPath, pathKnown := evalExistingPathPrefix(path)
	boundaryKnown := wdKnown && pathKnown
	var within bool
	if boundaryKnown {
		within = pathWithin(realWD, realPath)
	} else {
		within = pathWithin(absWD, path)
	}

	return FilesystemPathPolicy{
		Input:              value,
		WorkingDir:         absWD,
		Path:               path,
		RealPath:           realPath,
		RealWorkingDir:     realWD,
		BoundaryKnown:      boundaryKnown,
		WithinWorkingDir:   within,
		EscapesWorkingDir:  !within,
		SymlinkEvalApplied: realWD != absWD || realPath != path,
	}, nil
}

func filesystemPathMetadata(policy FilesystemPathPolicy) FilesystemPathMetadata {
	return FilesystemPathMetadata{
		RequestedPath:      policy.Input,
		ResolvedPath:       policy.Path,
		WorkingDir:         policy.WorkingDir,
		RealPath:           policy.RealPath,
		RealWorkingDir:     policy.RealWorkingDir,
		BoundaryKnown:      policy.BoundaryKnown,
		WithinWorkingDir:   policy.WithinWorkingDir,
		SymlinkEvalApplied: policy.SymlinkEvalApplied,
	}
}

func resolvePath(workingDir string, value string) string {
	if strings.TrimSpace(value) == "" {
		if strings.TrimSpace(workingDir) == "" {
			return "."
		}
		return filepath.Clean(workingDir)
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	cleanValue := filepath.Clean(value)
	if strings.TrimSpace(workingDir) == "" {
		return cleanValue
	}
	return filepath.Clean(filepath.Join(workingDir, cleanValue))
}

func resolveReadablePath(workingDir string, value string) (FilesystemPathPolicy, *Result) {
	policy, err := ResolveFilesystemPath(workingDir, value)
	if err != nil {
		return FilesystemPathPolicy{}, &Result{Content: fmt.Sprintf("Invalid path: %v", err), IsError: true}
	}
	return policy, nil
}

func isProtectedCredentialPath(path string) bool {
	path = comparableFilesystemPath(path)
	if path == "" {
		return false
	}
	for _, credentialPath := range matrixclawCredentialPaths() {
		credentialPath = comparableFilesystemPath(credentialPath)
		if credentialPath == "" {
			continue
		}
		if path == credentialPath || credentialBackupPath(path, credentialPath) {
			return true
		}
	}
	return false
}

func matrixclawCredentialPaths() []string {
	paths := []string{}
	if configured := strings.TrimSpace(os.Getenv("MATRIXCLAW_SETUP_PATH")); configured != "" {
		paths = append(paths, configured, filepath.Join(filepath.Dir(configured), "daemon.env"))
	}
	if configDir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(configDir) != "" {
		root := filepath.Join(configDir, "matrixclaw")
		paths = append(paths, filepath.Join(root, "setup.json"), filepath.Join(root, "daemon.env"))
	}
	if dataDir := matrixclawUserDataDir(); dataDir != "" {
		root := filepath.Join(dataDir, "matrixclaw")
		paths = append(paths, filepath.Join(root, "matrixclaw.json"), filepath.Join(root, "providers.json"))
	}
	return paths
}

func matrixclawUserDataDir() string {
	if configured := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); configured != "" {
		return configured
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}

func credentialBackupPath(path string, credentialPath string) bool {
	return filepath.Dir(path) == filepath.Dir(credentialPath) &&
		strings.HasPrefix(filepath.Base(path), filepath.Base(credentialPath)+".")
}

func comparableFilesystemPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return ""
	}
	if realPath, known := evalExistingPathPrefix(absPath); known {
		return filepath.Clean(realPath)
	}
	return absPath
}

func resolveMutationPath(workingDir string, value string) (FilesystemPathPolicy, *Result) {
	policy, err := ResolveFilesystemPath(workingDir, value)
	if err != nil {
		return FilesystemPathPolicy{}, &Result{Content: fmt.Sprintf("Invalid path: %v", err), IsError: true}
	}
	return policy, nil
}

func ensureMutationWriteTarget(path string, allowMissing bool) error {
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect parent path: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("parent path is a symbolic link: %s", parent)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("parent path is not a directory: %s", parent)
	}

	info, err := os.Lstat(path)
	if err != nil {
		if allowMissing && os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is a symbolic link: %s", path)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", path)
	}
	return nil
}

func evalExistingPathPrefix(path string) (string, bool) {
	path = filepath.Clean(path)
	parts := []string{}
	current := path
	for {
		real, err := filepath.EvalSymlinks(current)
		if err == nil {
			if len(parts) == 0 {
				return filepath.Clean(real), true
			}
			all := append([]string{real}, parts...)
			return filepath.Clean(filepath.Join(all...)), true
		}
		if !os.IsNotExist(err) {
			return path, false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path, false
		}
		parts = append([]string{filepath.Base(current)}, parts...)
		current = parent
	}
}

func pathWithin(root string, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var out strings.Builder
	out.WriteString("^")
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				out.WriteString(".*")
				i++
			} else {
				out.WriteString(`[^/]*`)
			}
		case '?':
			out.WriteString(`[^/]`)
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			out.WriteByte('\\')
			out.WriteByte(pattern[i])
		default:
			out.WriteByte(pattern[i])
		}
	}
	out.WriteString("$")
	return regexp.Compile(out.String())
}

func shouldSkipHidden(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func matchesAnyIgnore(path string, root string, ignore []string) bool {
	if len(ignore) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, pattern := range ignore {
		regex, err := globToRegexp(pattern)
		if err != nil {
			continue
		}
		if regex.MatchString(rel) {
			return true
		}
	}
	return false
}

var errStopWalk = fmt.Errorf("stop walk")
