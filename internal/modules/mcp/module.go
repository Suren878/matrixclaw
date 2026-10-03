// Package mcp is the module serving the tools of configured MCP servers,
// the local browser's included.
package mcp

import (
	"context"
	"fmt"
	"log"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	mcpbridge "github.com/Suren878/matrixclaw/internal/mcp"
	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// ExtraServer is a server another module runs through MCP, like the browser.
type ExtraServer func(setup.Config) (setup.MCPServerConfig, bool)

type Module struct {
	extra    ExtraServer
	connect  func(context.Context, mcpbridge.ServerConfig) (session, error)
	mu       sync.RWMutex
	enabled  bool
	sessions map[string]connected
	failed   map[string]string
}

type session interface {
	Name() string
	Tools() []tools.Executor
	Close() error
}

type connected struct {
	config  mcpbridge.ServerConfig
	session session
	tools   []tools.Executor
}

func New(extra ExtraServer) *Module {
	return &Module{
		extra: extra,
		connect: func(ctx context.Context, cfg mcpbridge.ServerConfig) (session, error) {
			return mcpbridge.Connect(ctx, cfg)
		},
		sessions: map[string]connected{},
		failed:   map[string]string{},
	}
}

func (m *Module) ID() string { return "mcp" }

// Apply connects the servers that are new or changed, in parallel, and
// closes the ones removed or changed; unchanged servers keep their session.
// A server that fails to connect is reported in Status.
func (m *Module) Apply(ctx context.Context, cfg setup.Config) error {
	wanted := map[string]mcpbridge.ServerConfig{}
	for _, server := range wantedServers(cfg, m.extra) {
		wanted[server.ID] = server
	}
	m.mu.RLock()
	current := maps.Clone(m.sessions)
	m.mu.RUnlock()

	var stale []connected
	keep := map[string]connected{}
	for id, conn := range current {
		if server, ok := wanted[id]; ok && reflect.DeepEqual(server, conn.config) {
			keep[id] = conn
		} else {
			stale = append(stale, conn)
		}
	}
	for _, conn := range stale {
		if err := conn.session.Close(); err != nil {
			log.Printf("mcp: close %s: %v", conn.session.Name(), err)
		}
	}

	type result struct {
		conn connected
		err  error
	}
	results := make(map[string]*result, len(wanted))
	var wg sync.WaitGroup
	for id, server := range wanted {
		if _, ok := keep[id]; ok {
			continue
		}
		r := &result{}
		results[id] = r
		wg.Go(func() {
			s, err := m.connect(ctx, server)
			if err != nil {
				r.err = err
				return
			}
			r.conn = connected{config: server, session: s, tools: s.Tools()}
		})
	}
	wg.Wait()

	failed := map[string]string{}
	for id, r := range results {
		if r.err != nil {
			failed[id] = r.err.Error()
			continue
		}
		keep[id] = r.conn
	}
	m.mu.Lock()
	m.enabled = cfg.Modules.MCP.Enabled || len(wanted) > 0
	m.sessions = keep
	m.failed = failed
	m.mu.Unlock()
	return nil
}

// wantedServers are the enabled MCP servers and the extra one, normalized.
func wantedServers(cfg setup.Config, extra ExtraServer) []mcpbridge.ServerConfig {
	servers := []setup.MCPServerConfig{}
	if cfg.Modules.MCP.Enabled {
		servers = append(servers, cfg.Modules.MCP.Servers...)
	}
	if extra != nil {
		if server, ok := extra(cfg); ok {
			servers = append(servers, server)
		}
	}
	out := []mcpbridge.ServerConfig{}
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		out = append(out, mcpbridge.ServerConfig{
			ID:         server.ID,
			Name:       server.Name,
			Enabled:    server.Enabled,
			Transport:  server.Transport,
			Command:    server.Command,
			Args:       slices.Clone(server.Args),
			Env:        maps.Clone(server.Env),
			Endpoint:   server.Endpoint,
			ToolPrefix: server.ToolPrefix,
			ReadOnly:   server.ReadOnly,
			Timeout:    time.Duration(server.TimeoutSeconds) * time.Second,
		})
	}
	return mcpbridge.NormalizeConfig(mcpbridge.Config{Enabled: true, Servers: out}).Servers
}

// connectedServers are the sessions sorted by server id.
func (m *Module) connectedServers() []connected {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := slices.Collect(maps.Values(m.sessions))
	slices.SortFunc(out, func(a, b connected) int { return strings.Compare(a.config.ID, b.config.ID) })
	return out
}

func (m *Module) Tools() []tools.Executor {
	out := []tools.Executor{}
	for _, conn := range m.connectedServers() {
		out = append(out, conn.tools...)
	}
	return out
}

func (m *Module) Context() string {
	servers := m.connectedServers()
	if len(servers) == 0 {
		return ""
	}
	names := make([]string, 0, len(servers))
	count := 0
	for _, conn := range servers {
		names = append(names, conn.session.Name())
		count += len(conn.tools)
	}
	return fmt.Sprintf("MCP module:\n- Connected MCP servers: %s.\n- Remote MCP tools are available as matrixclaw tools prefixed with mcp_<server>_. Use them when they directly match the task.\n- Remote MCP tool count: %d.", strings.Join(names, ", "), count)
}

func (m *Module) Status(context.Context) modules.Status {
	connectedCount := len(m.connectedServers())
	m.mu.RLock()
	enabled := m.enabled
	failures := make([]string, 0, len(m.failed))
	for id, err := range m.failed {
		failures = append(failures, id+": "+err)
	}
	m.mu.RUnlock()
	slices.Sort(failures)
	state := "Disabled"
	if enabled {
		state = fmt.Sprintf("%d connected", connectedCount)
		if len(failures) > 0 {
			state += fmt.Sprintf(", %d failed", len(failures))
		}
	}
	return modules.Status{
		ID:      m.ID(),
		Title:   "MCP",
		Enabled: enabled,
		Ready:   enabled && len(failures) == 0,
		State:   state,
		Detail:  strings.Join(failures, "; "),
		Facts: []modules.Fact{
			{Key: "connected_servers", Label: "Connected servers", Value: fmt.Sprint(connectedCount)},
			{Key: "failed_servers", Label: "Failed servers", Value: fmt.Sprint(len(failures))},
		},
	}
}

func (m *Module) Close() error {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[string]connected{}
	m.mu.Unlock()
	for _, conn := range sessions {
		_ = conn.session.Close()
	}
	return nil
}
