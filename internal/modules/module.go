// Package modules defines the daemon's modules and the set that owns them.
package modules

import (
	"context"
	"errors"
	"log"
	"strings"
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
	// Settings tells that the module has a settings screen (Configurable).
	Settings bool `json:"settings,omitempty"`
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
	// refused are the module tools the registry turned down, by module id.
	refused atomic.Pointer[map[string]string]
	applyMu sync.Mutex
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
// A tool whose id is taken is left out, logged and shown in its module's Status.
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
	refused := map[string]string{}
	for _, module := range s.modules {
		if err := registry.Register(module.Tools()...); err != nil {
			log.Printf("modules: %s tools left out: %v", module.ID(), err)
			refused[module.ID()] = strings.ReplaceAll(err.Error(), "\n", "; ")
		}
	}
	s.registry.Store(registry)
	s.refused.Store(&refused)
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
		out = append(out, s.status(ctx, module))
	}
	return out
}

func (s *Set) status(ctx context.Context, module Module) Status {
	status := module.Status(ctx)
	for _, executor := range module.Tools() {
		status.Tools = append(status.Tools, executor.Spec().ID)
	}
	if refused := s.refused.Load(); refused != nil && (*refused)[module.ID()] != "" {
		status.Detail = strings.TrimPrefix(status.Detail+"; "+(*refused)[module.ID()], "; ")
	}
	_, status.Settings = module.(Configurable)
	return status
}

// Configurable is the module with id when it has a settings screen.
func (s *Set) Configurable(id string) (Configurable, bool) {
	for _, module := range s.modules {
		if module.ID() == id {
			configurable, ok := module.(Configurable)
			return configurable, ok
		}
	}
	return nil, false
}

// Settings is the settings screen of the module with id.
func (s *Set) Settings(ctx context.Context, id string) (Settings, bool) {
	for _, module := range s.modules {
		if configurable, ok := module.(Configurable); ok && module.ID() == id {
			return Settings{Status: s.status(ctx, module), Items: configurable.Settings(ctx)}, true
		}
	}
	return Settings{}, false
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

func (s *Set) Preview(ctx context.Context, toolID string, call tools.Call) (tools.ApprovalRequest, *tools.Result) {
	return s.registry.Load().Preview(ctx, toolID, call)
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
