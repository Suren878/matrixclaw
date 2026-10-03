package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	executors map[string]registeredTool
	order     []string
	err       error
}

type registeredTool struct {
	executor Executor
	spec     Spec
}

type DuplicateToolError struct {
	ID string
}

func (e DuplicateToolError) Error() string {
	return fmt.Sprintf("duplicate tool id %q", e.ID)
}

type InvalidToolSpecError struct {
	Reason string
}

func (e InvalidToolSpecError) Error() string {
	if strings.TrimSpace(e.Reason) == "" {
		return "invalid tool spec"
	}
	return "invalid tool spec: " + e.Reason
}

func NewRegistry(executors ...Executor) *Registry {
	registry := &Registry{
		executors: map[string]registeredTool{},
	}
	_ = registry.Register(executors...)
	return registry
}

func (r *Registry) Register(executors ...Executor) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.executors == nil {
		r.executors = map[string]registeredTool{}
	}
	var registerErr error
	for _, executor := range executors {
		if executor == nil {
			continue
		}
		spec, err := normalizeSpec(executor.Spec())
		if err != nil {
			registerErr = errors.Join(registerErr, err)
			continue
		}
		id := normalizeToolID(spec.ID)
		if _, exists := r.executors[id]; exists {
			registerErr = errors.Join(registerErr, DuplicateToolError{ID: spec.ID})
			continue
		}
		r.executors[id] = registeredTool{executor: executor, spec: spec}
		r.order = append(r.order, id)
	}
	r.err = errors.Join(r.err, registerErr)
	return registerErr
}

func (r *Registry) Err() error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.err
}

func (r *Registry) List() []Spec {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]Spec, 0, len(r.executors))
	for _, id := range r.order {
		specs = append(specs, cloneSpec(r.executors[id].spec))
	}
	return specs
}

func (r *Registry) Execute(ctx context.Context, toolID string, call Call) (Result, error) {
	if r == nil {
		return Result{}, fmt.Errorf("tool registry is not configured")
	}
	r.mu.RLock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	r.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", strings.TrimSpace(toolID))
	}
	result, err := registered.executor.Execute(ctx, call)
	if err != nil && errors.Is(err, ErrInvalidArgs) {
		return invalidArgsResult(toolID, err), nil
	}
	return result, err
}

func (r *Registry) Spec(toolID string) (Spec, bool) {
	if r == nil {
		return Spec{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	if !ok {
		return Spec{}, false
	}
	return cloneSpec(registered.spec), true
}

func normalizeToolID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeSpec(spec Spec) (Spec, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return Spec{}, InvalidToolSpecError{Reason: "id is required"}
	}
	spec.Description = strings.TrimSpace(spec.Description)
	if spec.Description == "" {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: description is required", spec.ID)}
	}
	spec.Namespace = strings.ToLower(strings.TrimSpace(spec.Namespace))
	if spec.Namespace == "" {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: namespace is required", spec.ID)}
	}
	spec.Effect = normalizeEffect(spec.Effect)
	if spec.Effect == "" {
		spec.Effect = EffectReadOnly
	}
	if spec.Effect != EffectReadOnly && spec.Effect != EffectMutation {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown effect %q", spec.ID, spec.Effect)}
	}
	spec.Category = normalizeCategory(spec.Category)
	if !knownCategory(spec.Category) {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown category %q", spec.ID, spec.Category)}
	}
	if len(spec.InputJSONSchema) == 0 {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: input schema is required", spec.ID)}
	}
	return spec, nil
}

func normalizeCategory(value Category) Category {
	return Category(strings.ToLower(strings.TrimSpace(string(value))))
}

func knownCategory(value Category) bool {
	switch value {
	case CategoryFilesystem, CategoryShell, CategoryAutomation, CategoryStorage, CategoryWeb, CategorySkills:
		return true
	default:
		return false
	}
}

func normalizeEffect(value Effect) Effect {
	return Effect(strings.ToLower(strings.TrimSpace(string(value))))
}

func cloneSpec(spec Spec) Spec {
	if spec.InputJSONSchema != nil {
		spec.InputJSONSchema = slices.Clone(spec.InputJSONSchema)
	}
	return spec
}
