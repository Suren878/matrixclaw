package claudecode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func LookupPath(path string) (string, error) {
	if path == "" {
		path = "claude"
	}
	if filepath.IsAbs(path) || strings.Contains(path, string(os.PathSeparator)) {
		return exec.LookPath(path)
	}
	if resolved, err := exec.LookPath(path); err == nil {
		return resolved, nil
	}
	for _, candidate := range claudeBinaryCandidates(path) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func Version(ctx context.Context, path string) string {
	resolved, err := LookupPath(path)
	if err != nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, resolved, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func claudeBinaryCandidates(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "claude"
	}
	home, _ := os.UserHomeDir()
	home = strings.TrimSpace(home)
	candidates := []string{
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
		filepath.Join("/bin", name),
		filepath.Join("/snap/bin", name),
		filepath.Join("/opt/homebrew/bin", name),
	}
	if home != "" {
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
	seen := map[string]struct{}{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}
