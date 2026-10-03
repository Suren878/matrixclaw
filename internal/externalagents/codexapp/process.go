package codexapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/safego"
)

type ProcessOptions struct {
	Path   string
	Args   []string
	Stderr io.Writer
}

func Start(ctx context.Context, opts ProcessOptions) (*Client, error) {
	path, err := externalagents.LookupBinary("codex", opts.Path)
	if err != nil {
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
	err, ready := c.waitErrorReady()
	if !ready {
		return nil
	}
	if c.isClosing() {
		return nil
	}
	return processExitError(err)
}

func (c *processConn) StdoutClosedError() error {
	if c.isClosing() {
		return nil
	}
	if c.waitDone != nil {
		timer := time.NewTimer(25 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-c.waitDone:
			if c.isClosing() {
				return nil
			}
			c.mu.Lock()
			err := c.waitErr
			c.mu.Unlock()
			return processExitError(err)
		case <-timer.C:
		}
	}
	if c.isClosing() {
		return nil
	}
	return fmt.Errorf("codex app-server stdout closed")
}

func processExitError(err error) error {
	if err == nil {
		return fmt.Errorf("codex app-server exited")
	}
	return fmt.Errorf("codex app-server exited: %w", err)
}

func (c *processConn) isClosing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closing
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

func (c *processConn) waitErrorReady() (error, bool) {
	if c.waitDone == nil {
		return nil, false
	}
	select {
	case <-c.waitDone:
	default:
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waitErr, true
}
