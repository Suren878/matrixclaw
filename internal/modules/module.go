// Package modules defines the daemon's modules and the set that owns them.
package modules

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// Module is a daemon feature configured from setup.json.
type Module interface {
	ID() string
	// Apply makes the module follow cfg. It is called at start and after
	// every setup change, one call at a time, and is cheap when the module's
	// part of cfg did not change.
	Apply(ctx context.Context, cfg setup.Config) error
	// Tools are the tools the module offers now; none while it is off.
	Tools() []tools.Executor
	// Context is a system prompt paragraph about the module, "" for none.
	Context() string
	// Status describes the module without network calls beyond cached ones.
	Status(ctx context.Context) Status
	Close() error
}

// Status is a module's state in a shape any client can render.
type Status struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Enabled bool     `json:"enabled"`
	Ready   bool     `json:"ready"`
	State   string   `json:"state"`
	Detail  string   `json:"detail,omitempty"`
	Facts   []Fact   `json:"facts,omitempty"`
	Tools   []string `json:"tools,omitempty"`
}

// Fact is one labelled value of a module's status.
type Fact struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// StatusResponse is the body of GET /v1/modules.
type StatusResponse struct {
	Modules []Status `json:"modules"`
}

// Set owns the daemon's modules and serves their tools, with the base tools,
// as the core's tool executor.
type Set struct {
	base     []tools.Executor
	modules  []Module
	registry atomic.Pointer[tools.Registry]
	applyMu  sync.Mutex
}

func NewSet(base []tools.Executor, modules ...Module) (*Set, error) {
	s := &Set{base: base, modules: modules}
	registry := tools.NewRegistry(base...)
	if err := registry.Err(); err != nil {
		return nil, err
	}
	s.registry.Store(registry)
	return s, nil
}

// Apply applies cfg to every module and then offers the tools they offer
// now. A module that fails keeps its previous state; the others still apply.
func (s *Set) Apply(ctx context.Context, cfg setup.Config) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	var errs []error
	for _, module := range s.modules {
		if err := module.Apply(ctx, cfg); err != nil {
			errs = append(errs, err)
		}
	}
	registry := tools.NewRegistry(s.base...)
	for _, module := range s.modules {
		if err := registry.Register(module.Tools()...); err != nil {
			errs = append(errs, err)
		}
	}
	s.registry.Store(registry)
	return errors.Join(errs...)
}

// Context is the modules' system prompt paragraphs.
func (s *Set) Context() []string {
	out := []string{}
	for _, module := range s.modules {
		if context := module.Context(); context != "" {
			out = append(out, context)
		}
	}
	return out
}

func (s *Set) Statuses(ctx context.Context) []Status {
	out := make([]Status, 0, len(s.modules))
	for _, module := range s.modules {
		status := module.Status(ctx)
		for _, executor := range module.Tools() {
			status.Tools = append(status.Tools, executor.Spec().ID)
		}
		out = append(out, status)
	}
	return out
}

func (s *Set) Close() error {
	var errs []error
	for _, module := range s.modules {
		if err := module.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Set) List() []tools.Spec { return s.registry.Load().List() }

func (s *Set) Spec(toolID string) (tools.Spec, bool) { return s.registry.Load().Spec(toolID) }

func (s *Set) Execute(ctx context.Context, toolID string, call tools.Call) (tools.Result, error) {
	return s.registry.Load().Execute(ctx, toolID, call)
}

func (s *Set) Subject(toolID string, call tools.Call) permission.Subject {
	return s.registry.Load().Subject(toolID, call)
}

func (s *Set) ConcurrencyKey(toolID string, call tools.Call) string {
	return s.registry.Load().ConcurrencyKey(toolID, call)
}

// Static is a module with fixed tools and no settings.
func Static(id string, title string, context string, executors ...tools.Executor) Module {
	return &static{id: id, title: title, context: context, tools: executors}
}

type static struct {
	id, title, context string
	tools              []tools.Executor
}

func (m *static) ID() string                                { return m.id }
func (m *static) Apply(context.Context, setup.Config) error { return nil }
func (m *static) Tools() []tools.Executor                   { return m.tools }
func (m *static) Context() string                           { return m.context }
func (m *static) Close() error                              { return nil }

func (m *static) Status(context.Context) Status {
	return Status{ID: m.id, Title: m.title, Enabled: true, Ready: true, State: "Ready"}
}
