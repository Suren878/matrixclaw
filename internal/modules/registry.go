package modules

import "github.com/Suren878/matrixclaw/internal/tools"

type Module interface {
	ID() string
	Name() string
	RegisterTools(*tools.Registry) error
	Context() string
}

type Registry struct {
	modules []Module
}

func NewRegistry(modules ...Module) *Registry {
	registry := &Registry{}
	registry.Register(modules...)
	return registry
}

func (r *Registry) Register(modules ...Module) {
	if r == nil {
		return
	}
	for _, module := range modules {
		if module == nil {
			continue
		}
		r.modules = append(r.modules, module)
	}
}

func (r *Registry) RegisterTools(registry *tools.Registry) error {
	if r == nil || registry == nil {
		return nil
	}
	for _, module := range r.modules {
		if err := module.RegisterTools(registry); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) Context() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.modules))
	for _, module := range r.modules {
		if context := module.Context(); context != "" {
			out = append(out, context)
		}
	}
	return out
}
