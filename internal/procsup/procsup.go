// Package procsup runs long-lived local helper processes (voice servers), one
// per key, and stops them with the daemon.
package procsup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

const (
	stopTimeout         = 2 * time.Second
	defaultReadyTimeout = 60 * time.Second
	probeInterval       = 100 * time.Millisecond
)

// Spec describes a process; two specs with the same command, environment and
// stdin mode run the same process.
type Spec struct {
	Key   string
	Path  string
	Args  []string
	Env   []string // nil inherits the daemon's environment
	Stdin bool     // keep a stdin pipe
	Log   string   // stderr log file name in the supervisor's log dir; "" discards
	// Ready is one readiness probe; the supervisor repeats it until it passes,
	// the process exits or ReadyTimeout ends. Nil means started is ready.
	Ready        func(ctx context.Context) error
	ReadyTimeout time.Duration
}

func (s Spec) sameProcess(other Spec) bool {
	return s.Path == other.Path && slices.Equal(s.Args, other.Args) && slices.Equal(s.Env, other.Env) && s.Stdin == other.Stdin
}

type Supervisor struct {
	logDir string
	mu     sync.Mutex
	procs  map[string]*Process
}

func New(logDir string) *Supervisor {
	return &Supervisor{logDir: logDir, procs: map[string]*Process{}}
}

// Start returns the running process of spec.Key when it was started from the
// same command, else replaces it, and waits until it is ready. The probe runs
// outside the supervisor lock; concurrent Starts of one key share it.
func (s *Supervisor) Start(ctx context.Context, spec Spec) (*Process, error) {
	s.mu.Lock()
	old := s.procs[spec.Key]
	if old != nil && old.Running() && old.spec.sameProcess(spec) {
		s.mu.Unlock()
		return old, old.waitReady(ctx)
	}
	delete(s.procs, spec.Key)
	p, err := s.spawn(spec)
	if err == nil {
		s.procs[spec.Key] = p
	}
	s.mu.Unlock()
	if old != nil {
		old.stop()
	}
	if err != nil {
		return nil, err
	}
	p.readyErr = p.probe(ctx)
	close(p.ready)
	if p.readyErr != nil {
		s.forget(p)
		p.stop()
		return nil, p.readyErr
	}
	return p, nil
}

// Running is the live process of key.
func (s *Supervisor) Running(key string) (*Process, bool) {
	s.mu.Lock()
	p := s.procs[key]
	s.mu.Unlock()
	if p == nil || !p.Running() {
		return nil, false
	}
	return p, true
}

func (s *Supervisor) Stop(key string) {
	s.mu.Lock()
	p := s.procs[key]
	delete(s.procs, key)
	s.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

// StopAll stops every process; the daemon calls it on shutdown.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	procs := make([]*Process, 0, len(s.procs))
	for key, p := range s.procs {
		procs = append(procs, p)
		delete(s.procs, key)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Go(p.stop)
	}
	wg.Wait()
}

func (s *Supervisor) forget(p *Process) {
	s.mu.Lock()
	if s.procs[p.spec.Key] == p {
		delete(s.procs, p.spec.Key)
	}
	s.mu.Unlock()
}

func (s *Supervisor) spawn(spec Spec) (*Process, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Env = spec.Env
	Prepare(cmd)
	p := &Process{spec: spec, cmd: cmd, done: make(chan struct{}), ready: make(chan struct{})}
	if spec.Stdin {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		p.stdin = stdin
	}
	logFile := s.openLog(spec.Log)
	if logFile != nil {
		cmd.Stderr = logFile
		defer func() { _ = logFile.Close() }()
	}
	if err := cmd.Start(); err != nil {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		return nil, err
	}
	safego.Go("procsup.wait", func() {
		defer close(p.done)
		_ = cmd.Wait()
	})
	return p, nil
}

func (s *Supervisor) openLog(name string) *os.File {
	if name == "" || s.logDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.logDir, 0o755); err != nil {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(s.logDir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return file
}

// Process is a supervised child process.
type Process struct {
	spec     Spec
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	ready    chan struct{}
	readyErr error
	use      sync.Mutex
}

func (p *Process) Running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *Process) PID() int { return p.cmd.Process.Pid }

// RSS is the resident memory of the process in bytes; 0 when unknown.
func (p *Process) RSS() uint64 { return rssBytes(p.PID()) }

// Use runs fn with the process's stdin, one caller at a time.
func (p *Process) Use(fn func(stdin io.Writer) error) error {
	p.use.Lock()
	defer p.use.Unlock()
	if !p.Running() || p.stdin == nil {
		return errors.New("process is not running")
	}
	return fn(p.stdin)
}

func (p *Process) probe(ctx context.Context) error {
	if p.spec.Ready == nil {
		return nil
	}
	timeout := p.spec.ReadyTimeout
	if timeout <= 0 {
		timeout = defaultReadyTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for {
		if !p.Running() {
			return fmt.Errorf("%s exited before it was ready", filepath.Base(p.spec.Path))
		}
		if p.spec.Ready(ctx) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not start: %w", filepath.Base(p.spec.Path), ctx.Err())
		case <-p.done:
		case <-ticker.C:
		}
	}
}

func (p *Process) waitReady(ctx context.Context) error {
	select {
	case <-p.ready:
		return p.readyErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Process) stop() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(stopTimeout):
	}
}
