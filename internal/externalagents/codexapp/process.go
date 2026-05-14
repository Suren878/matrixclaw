package codexapp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

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
	return exec.LookPath(path)
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
		reader: stdout,
		writer: stdin,
		cmd:    cmd,
	}
	return NewClient(conn), nil
}

type processConn struct {
	reader io.ReadCloser
	writer io.WriteCloser
	cmd    *exec.Cmd
}

func (c *processConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *processConn) Write(p []byte) (int, error) {
	return c.writer.Write(p)
}

func (c *processConn) Close() error {
	_ = c.writer.Close()
	_ = c.reader.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}
