package localruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
)

const managedProcessStopTimeout = 2 * time.Second

type managedProcess struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
}

type localProcessManager[T any] struct {
	mu        sync.Mutex
	processes map[string]T
}

type managedProcessOptions struct {
	cmd      *exec.Cmd
	logName  string
	waitName string
	stdin    bool
}

func (r *Runtime) startManagedProcess(options managedProcessOptions) (*managedProcess, error) {
	if options.cmd == nil {
		return nil, errors.New("managed process command is nil")
	}
	cmd := options.cmd
	var stdin io.WriteCloser
	if options.stdin {
		var err error
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
	}
	cmd.Stdout = io.Discard
	logFile := r.openManagedProcessLog(options.logName)
	if logFile != nil {
		cmd.Stderr = logFile
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		if logFile != nil {
			_ = logFile.Close()
		}
		return nil, err
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	process := &managedProcess{
		cmd:   cmd,
		stdin: stdin,
		done:  make(chan struct{}),
	}
	waitName := strings.TrimSpace(options.waitName)
	if waitName == "" {
		waitName = "localruntime.processWait"
	}
	safego.Go(waitName, func() {
		defer close(process.done)
		_ = cmd.Wait()
	})
	return process, nil
}

func (r *Runtime) openManagedProcessLog(name string) *os.File {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	dir := r.runtimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return file
}

func (p *managedProcess) running() bool {
	if p == nil || p.done == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *managedProcess) stop(timeout time.Duration) error {
	if p == nil {
		return nil
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	if p.done == nil {
		return nil
	}
	if timeout <= 0 {
		select {
		case <-p.done:
		default:
		}
		return nil
	}
	select {
	case <-p.done:
	case <-time.After(timeout):
	}
	return nil
}

type httpReadyOptions struct {
	url            string
	timeout        time.Duration
	process        *managedProcess
	ready          func(*http.Response) bool
	exitedMessage  string
	timeoutMessage string
}

func (r *Runtime) waitHTTPReady(ctx context.Context, options httpReadyOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if options.process != nil && !options.process.running() {
			return errors.New(options.exitedMessage)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, options.url, nil)
		if err != nil {
			return err
		}
		response, err := r.httpClient().Do(request)
		if err == nil {
			_ = response.Body.Close()
			if options.ready == nil || options.ready(response) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", options.timeoutMessage, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *Runtime) localVoiceProcessKey(provider setup.VoiceProviderOption) string {
	return filepath.Join(r.rootDir(), strings.TrimSpace(provider.ID))
}
