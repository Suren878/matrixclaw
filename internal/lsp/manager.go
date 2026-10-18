// Package lsp runs language servers (gopls, typescript-language-server,
// pyright or pylsp, rust-analyzer) for the agent: the errors an edit leaves,
// in the edited files and in the files that use them, and the `lsp` tool's
// code navigation.
package lsp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/procsup"
	"github.com/Suren878/matrixclaw/internal/safego"
)

const (
	// idleTimeout stops a server nobody used for that long.
	idleTimeout = 10 * time.Minute
	// brokenRetry is how long a server that failed to start is not tried again.
	brokenRetry = 5 * time.Minute
)

// errNoServer says no language server serves a file.
var errNoServer = errors.New("no language server for this file")

// Manager starts one server per language and project root on first use and
// stops it when idle and on Close.
type Manager struct {
	procs  *procsup.Supervisor
	env    func() []string
	config func() Config
	logf   func(format string, args ...any)
	idle   time.Duration

	lifetime context.Context
	cancel   context.CancelFunc

	mu      sync.Mutex
	clients map[string]*client
	broken  map[string]failure
	started int
	closed  bool
}

// failure is a server that did not start, and with which command.
type failure struct {
	at   time.Time
	argv []string
}

// NewManager returns a manager whose servers log stderr into logDir and run
// with env (nil for the daemon's environment) and the settings of config.
func NewManager(logDir string, env func() []string, config func() Config) *Manager {
	lifetime, cancel := context.WithCancel(context.Background())
	return &Manager{procs: procsup.New(logDir), env: env, config: config, logf: log.Printf, idle: idleTimeout,
		lifetime: lifetime, cancel: cancel, clients: map[string]*client{}, broken: map[string]failure{}}
}

// target is the server a file belongs to: its language, root, command and key.
type target struct {
	lang *language
	root string
	argv []string
	key  string
}

// targetFor finds the server of a file or directory under cfg, with server
// programs looked up on path; ok is false when its language has no root, no
// installed server or servers are off.
func targetFor(lang *language, dir string, cfg Config, path string) (target, bool) {
	if cfg.Disabled {
		return target{}, false
	}
	root := lang.root(dir)
	if root == "" {
		return target{}, false
	}
	argv := lang.command(cfg, path)
	if argv == nil {
		return target{}, false
	}
	return target{lang: lang, root: root, argv: argv, key: lang.name + ":" + root}, true
}

// Available reports whether a server would serve a project in dir, so the
// `lsp` tool is offered there.
func (m *Manager) Available(dir string) bool {
	if m == nil || dir == "" {
		return false
	}
	cfg, path := m.config(), pathOf(m.environ())
	for i := range languages {
		if t, ok := targetFor(&languages[i], dir, cfg, path); ok && !m.isBroken(t) {
			return true
		}
	}
	return false
}

func (m *Manager) isBroken(t target) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	failed, ok := m.broken[t.key]
	return ok && slices.Equal(failed.argv, t.argv) && time.Since(failed.at) < brokenRetry
}

// clientFor is the server of file, started when it is not running; it may
// still be initializing. errNoServer when none serves the file.
func (m *Manager) clientFor(file string) (*client, error) {
	lang := languageFor(file)
	if lang == nil {
		return nil, errNoServer
	}
	env := m.environ()
	t, ok := targetFor(lang, filepath.Dir(file), m.config(), pathOf(env))
	if !ok || m.isBroken(t) {
		return nil, errNoServer
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errNoServer
	}
	if c := m.clients[t.key]; c != nil {
		if c.alive() && slices.Equal(c.argv, t.argv) {
			m.touch(t.key, c)
			return c, nil
		}
		delete(m.clients, t.key)
		safego.Go("lsp.stop", func() { m.stop(c) })
	}
	m.started++
	c := newClient(t.lang, t.root, t.argv)
	c.procKey = fmt.Sprintf("%s#%d", t.key, m.started)
	m.clients[t.key] = c
	m.touch(t.key, c)
	safego.Go("lsp.start", func() {
		err := c.start(m.lifetime, m.procs, c.procKey, env)
		if err != nil {
			m.logf("lsp: %s for %s did not start: %v", strings.Join(t.argv, " "), t.root, err)
			m.mu.Lock()
			m.broken[t.key] = failure{at: time.Now(), argv: t.argv}
			if m.clients[t.key] == c {
				delete(m.clients, t.key)
			}
			m.mu.Unlock()
		}
		c.err = err
		close(c.ready)
		if err != nil {
			m.stop(c)
		}
	})
	return c, nil
}

// touch marks c used now and arms its idle stop; m.mu is held.
func (m *Manager) touch(key string, c *client) {
	c.lastUsed = time.Now()
	if c.idle == nil {
		c.idle = time.AfterFunc(m.idle, func() { m.retire(key, c) })
		return
	}
	c.idle.Reset(m.idle)
}

// retire stops c when it was not used for the idle timeout.
func (m *Manager) retire(key string, c *client) {
	m.mu.Lock()
	if m.clients[key] != c {
		m.mu.Unlock()
		return
	}
	if left := m.idle - time.Since(c.lastUsed); left > 0 {
		c.idle.Reset(left)
		m.mu.Unlock()
		return
	}
	delete(m.clients, key)
	m.mu.Unlock()
	m.stop(c)
}

// stop asks an initialized server to exit, then kills what is left of it.
func (m *Manager) stop(c *client) {
	if c.idle != nil {
		c.idle.Stop()
	}
	select {
	case <-c.ready:
		if c.err == nil {
			c.shutdown()
		}
	default:
	}
	m.procs.Stop(c.procKey)
}

// Close stops every server; the daemon calls it on shutdown.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	clients := make([]*client, 0, len(m.clients))
	for key, c := range m.clients {
		clients = append(clients, c)
		delete(m.clients, key)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Go(func() { m.stop(c) })
	}
	wg.Wait()
	m.cancel()
	m.procs.StopAll()
}

// ready is the initialized server of file, with its open documents synced
// with the disk; it waits for initialize at most as long as ctx allows.
func (m *Manager) ready(ctx context.Context, file string) (*client, error) {
	c, err := m.clientFor(file)
	if err != nil {
		return nil, err
	}
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	return c, c.refresh()
}

func (m *Manager) environ() []string {
	if m.env == nil {
		return os.Environ()
	}
	return m.env()
}
