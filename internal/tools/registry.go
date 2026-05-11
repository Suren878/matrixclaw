package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Registry struct {
	executors map[string]Executor
}

func NewRegistry(executors ...Executor) *Registry {
	registry := &Registry{
		executors: map[string]Executor{},
	}
	registry.Register(executors...)
	return registry
}

func (r *Registry) Register(executors ...Executor) {
	if r == nil {
		return
	}
	if r.executors == nil {
		r.executors = map[string]Executor{}
	}
	for _, executor := range executors {
		if executor == nil {
			continue
		}
		spec := executor.Spec()
		r.executors[normalizeToolID(spec.ID)] = executor
	}
}

func (r *Registry) List() []Spec {
	if r == nil {
		return nil
	}
	specs := make([]Spec, 0, len(r.executors))
	for _, executor := range r.executors {
		specs = append(specs, executor.Spec())
	}
	return specs
}

func (r *Registry) Execute(ctx context.Context, toolID string, call Call) (Result, error) {
	if r == nil {
		return Result{}, fmt.Errorf("tool registry is not configured")
	}
	executor, ok := r.executors[normalizeToolID(toolID)]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", strings.TrimSpace(toolID))
	}
	result, err := executor.Execute(ctx, call)
	if err != nil && errors.Is(err, ErrInvalidArgs) {
		return invalidArgsResult(toolID, err), nil
	}
	return result, err
}

func normalizeToolID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
