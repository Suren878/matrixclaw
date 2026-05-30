package codexapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

var errCodexAppBundlePath = errors.New("codex CLI binary required; macOS .app bundle paths are not supported")

type ProcessOptions struct {
	Path   string
	Args   []string
	Stderr io.Writer
}

func Available(path string) bool {
	_, err := LookupPath(path)
	return err == nil
}

func LookupPath(path string) (string, error) {
	if path == "" {
		path = "codex"
	}
	if isMacOSAppBundlePath(path) {
		return "", errCodexAppBundlePath
	}
	if filepath.IsAbs(path) || strings.Contains(path, string(os.PathSeparator)) {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", err
		}
		if isMacOSAppBundlePath(resolved) {
			return "", errCodexAppBundlePath
		}
		return resolved, nil
	}
	if resolved, err := exec.LookPath(path); err == nil {
		if isMacOSAppBundlePath(resolved) {
			return "", errCodexAppBundlePath
		}
		return resolved, nil
	}
	for _, candidate := range codexBinaryCandidates(path) {
		if isMacOSAppBundlePath(candidate) {
			continue
		}
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

func Start(ctx context.Context, opts ProcessOptions) (*Client, error) {
	path := opts.Path
	if path == "" {
		path = "codex"
	}
	resolved, err := LookupPath(path)
	if err == nil {
		path = resolved
	} else {
		return nil, fmt.Errorf("resolve codex binary: %w", err)
	}
	args := opts.Args
	if len(args) == 0 {
		args = []string{"app-server", "--listen", "stdio://"}
	}

	cmd := exec.CommandContext(ctx, path, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex app-server stdout: %w", err)
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	conn := &processConn{
		reader:   stdout,
		writer:   stdin,
		cmd:      cmd,
		waitDone: make(chan struct{}),
	}
	safego.Go("codexapp.processWait", conn.wait)
	return NewClient(conn), nil
}

func isMacOSAppBundlePath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	cleaned := filepath.Clean(path)
	for {
		base := filepath.Base(cleaned)
		if strings.HasSuffix(strings.ToLower(base), ".app") {
			return true
		}
		parent := filepath.Dir(cleaned)
		if parent == cleaned || parent == "." || parent == string(os.PathSeparator) {
			return false
		}
		cleaned = parent
	}
}

func codexBinaryCandidates(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "codex"
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

type processConn struct {
	reader   io.ReadCloser
	writer   io.WriteCloser
	cmd      *exec.Cmd
	waitDone chan struct{}

	closeOnce sync.Once
	mu        sync.Mutex
	waitErr   error
	closeErr  error
	closing   bool
}

func (c *processConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *processConn) Write(p []byte) (int, error) {
	return c.writer.Write(p)
}

func (c *processConn) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closing = true
		c.mu.Unlock()
		_ = c.writer.Close()
		_ = c.reader.Close()
		if c.cmd.Process != nil {
			if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				c.mu.Lock()
				c.closeErr = err
				c.mu.Unlock()
			}
		}
		waitErr := c.waitError()
		c.mu.Lock()
		if c.closeErr == nil {
			c.closeErr = waitErr
		}
		c.mu.Unlock()
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

func (c *processConn) ProcessError() error {
	err := c.waitError()
	if err == nil {
		return nil
	}
	c.mu.Lock()
	closing := c.closing
	c.mu.Unlock()
	if closing {
		return nil
	}
	return fmt.Errorf("codex app-server exited: %w", err)
}

func (c *processConn) wait() {
	err := c.cmd.Wait()
	c.mu.Lock()
	c.waitErr = err
	c.mu.Unlock()
	close(c.waitDone)
}

func (c *processConn) waitError() error {
	if c.waitDone == nil {
		return nil
	}
	<-c.waitDone
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waitErr
}
