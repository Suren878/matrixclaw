package externalagents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LookupBinary resolves an agent CLI from a configured path or name, then
// from the usual global npm, nvm, asdf and Homebrew locations.
func LookupBinary(name string, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = name
	}
	bundleErr := fmt.Errorf("%s CLI binary required; macOS .app bundle paths are not supported", name)
	if isMacOSAppBundlePath(path) {
		return "", bundleErr
	}
	resolved, err := exec.LookPath(path)
	if err == nil {
		if isMacOSAppBundlePath(resolved) {
			return "", bundleErr
		}
		return resolved, nil
	}
	if filepath.IsAbs(path) || strings.Contains(path, string(os.PathSeparator)) {
		return "", err
	}
	for _, candidate := range binaryCandidates(path) {
		if isMacOSAppBundlePath(candidate) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

// BinaryProbe remembers where an agent CLI was found and its version, so
// listing agents spawns `--version` once rather than on every prompt build.
type BinaryProbe struct {
	name string
	path string

	mu      sync.Mutex
	found   string
	version string
}

func NewBinaryProbe(name string, path string) *BinaryProbe {
	return &BinaryProbe{name: name, path: path}
}

// Probe returns the resolved binary and its version; a binary that is not
// found yet is looked up again on the next call.
func (p *BinaryProbe) Probe(ctx context.Context) (string, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.found != "" {
		return p.found, p.version, nil
	}
	resolved, err := LookupBinary(p.name, p.path)
	if err != nil {
		return "", "", err
	}
	p.found, p.version = resolved, binaryVersion(ctx, resolved)
	return p.found, p.version, nil
}

func binaryVersion(ctx context.Context, resolved string) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, resolved, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func isMacOSAppBundlePath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	cleaned := filepath.Clean(path)
	for {
		if strings.HasSuffix(strings.ToLower(filepath.Base(cleaned)), ".app") {
			return true
		}
		parent := filepath.Dir(cleaned)
		if parent == cleaned || parent == "." || parent == string(os.PathSeparator) {
			return false
		}
		cleaned = parent
	}
}

func binaryCandidates(name string) []string {
	candidates := []string{
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
		filepath.Join("/bin", name),
		filepath.Join("/snap/bin", name),
		filepath.Join("/opt/homebrew/bin", name),
	}
	if home, _ := os.UserHomeDir(); strings.TrimSpace(home) != "" {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", name),
			filepath.Join(home, ".npm-global", "bin", name),
			filepath.Join(home, ".npm", "bin", name),
			filepath.Join(home, ".volta", "bin", name),
			filepath.Join(home, ".bun", "bin", name),
		)
		for _, pattern := range []string{
			filepath.Join(home, ".nvm", "versions", "node", "*", "bin", name),
			filepath.Join(home, ".asdf", "installs", "nodejs", "*", "bin", name),
			filepath.Join(home, ".local", "share", "pnpm", name),
		} {
			matches, _ := filepath.Glob(pattern)
			sort.Strings(matches)
			candidates = append(candidates, matches...)
		}
	}
	return candidates
}
