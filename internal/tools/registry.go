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

type Policy struct {
	Profiles   []Profile
	Categories []Category
	IncludeIDs []string
	ExcludeIDs []string
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
	return r.ListFor(Policy{})
}

func (r *Registry) ListFor(policy Policy) []Spec {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]Spec, 0, len(r.executors))
	for _, id := range r.order {
		registered := r.executors[id]
		if !policy.Allows(registered.spec) {
			continue
		}
		specs = append(specs, cloneSpec(registered.spec))
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

func (r *Registry) View(policy Policy) *PolicyRegistry {
	return &PolicyRegistry{
		registry: r,
		policy:   normalizePolicy(policy),
	}
}

type PolicyRegistry struct {
	registry *Registry
	policy   Policy
}

func (r *PolicyRegistry) List() []Spec {
	if r == nil || r.registry == nil {
		return nil
	}
	return r.registry.ListFor(r.policy)
}

func (r *PolicyRegistry) Execute(ctx context.Context, toolID string, call Call) (Result, error) {
	if r == nil || r.registry == nil {
		return Result{}, fmt.Errorf("tool registry is not configured")
	}
	spec, ok := r.registry.Spec(toolID)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", strings.TrimSpace(toolID))
	}
	if !r.policy.Allows(spec) {
		return Result{}, fmt.Errorf("tool %q is not enabled by the active tool policy", strings.TrimSpace(toolID))
	}
	return r.registry.Execute(ctx, toolID, call)
}

func (r *PolicyRegistry) Spec(toolID string) (Spec, bool) {
	if r == nil || r.registry == nil {
		return Spec{}, false
	}
	spec, ok := r.registry.Spec(toolID)
	if !ok || !r.policy.Allows(spec) {
		return Spec{}, false
	}
	return spec, true
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

func (p Policy) Allows(spec Spec) bool {
	p = normalizePolicy(p)
	id := normalizeToolID(spec.ID)
	if slices.Contains(p.ExcludeIDs, id) {
		return false
	}
	if len(p.IncludeIDs) > 0 && slices.Contains(p.IncludeIDs, id) {
		return true
	}
	if len(p.Profiles) == 0 && len(p.Categories) == 0 && len(p.IncludeIDs) == 0 {
		return true
	}
	for _, profile := range spec.Profiles {
		if slices.Contains(p.Profiles, normalizeProfile(profile)) {
			return true
		}
	}
	if slices.Contains(p.Categories, normalizeCategory(spec.Category)) {
		return true
	}
	return false
}

func normalizeToolID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeSpec(spec Spec) (Spec, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" {
		return Spec{}, InvalidToolSpecError{Reason: "id is required"}
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: name is required", spec.ID)}
	}
	spec.Description = strings.TrimSpace(spec.Description)
	if spec.Description == "" {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: description is required", spec.ID)}
	}
	spec.Namespace = strings.ToLower(strings.TrimSpace(spec.Namespace))
	if spec.Namespace == "" {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: namespace is required", spec.ID)}
	}
	spec.Risk = normalizeRiskLevel(spec.Risk)
	if !knownRiskLevel(spec.Risk) {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown risk %q", spec.ID, spec.Risk)}
	}
	spec.Effect = normalizeEffect(spec.Effect)
	if spec.Effect == "" {
		spec.Effect = EffectReadOnly
	}
	if !knownEffect(spec.Effect) {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown effect %q", spec.ID, spec.Effect)}
	}
	spec.ApprovalMode = normalizeApprovalMode(spec.ApprovalMode)
	if spec.ApprovalMode == "" {
		if spec.Risk == RiskApproval {
			spec.ApprovalMode = ApprovalOnRequest
		} else {
			spec.ApprovalMode = ApprovalNever
		}
	}
	if !knownApprovalMode(spec.ApprovalMode) {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown approval mode %q", spec.ID, spec.ApprovalMode)}
	}
	spec.PermissionParams = strings.TrimSpace(spec.PermissionParams)
	spec.Category = normalizeCategory(spec.Category)
	if !knownCategory(spec.Category) {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown category %q", spec.ID, spec.Category)}
	}
	spec.OutputKind = normalizeOutputKind(spec.OutputKind)
	if !knownOutputKind(spec.OutputKind) {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown output kind %q", spec.ID, spec.OutputKind)}
	}
	spec.Profiles = normalizeProfiles(spec.Profiles)
	if len(spec.Profiles) == 0 {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: at least one profile is required", spec.ID)}
	}
	for _, profile := range spec.Profiles {
		if !knownProfile(profile) {
			return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: unknown profile %q", spec.ID, profile)}
		}
	}
	if len(spec.InputJSONSchema) == 0 {
		return Spec{}, InvalidToolSpecError{Reason: fmt.Sprintf("%s: input schema is required", spec.ID)}
	}
	return spec, nil
}

func normalizePolicy(policy Policy) Policy {
	policy.Profiles = normalizeProfiles(policy.Profiles)
	policy.Categories = normalizeCategories(policy.Categories)
	policy.IncludeIDs = normalizeToolIDs(policy.IncludeIDs)
	policy.ExcludeIDs = normalizeToolIDs(policy.ExcludeIDs)
	return policy
}

func normalizeToolIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeToolID(value)
		if normalized == "" || slices.Contains(out, normalized) {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeProfiles(values []Profile) []Profile {
	out := make([]Profile, 0, len(values))
	for _, value := range values {
		normalized := normalizeProfile(value)
		if normalized == "" || slices.Contains(out, normalized) {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeProfile(value Profile) Profile {
	return Profile(strings.ToLower(strings.TrimSpace(string(value))))
}

func knownProfile(value Profile) bool {
	switch value {
	case ProfileReadOnly, ProfileCoding, ProfileAutomation, ProfileStorage:
		return true
	default:
		return false
	}
}

func normalizeCategories(values []Category) []Category {
	out := make([]Category, 0, len(values))
	for _, value := range values {
		normalized := normalizeCategory(value)
		if normalized == "" || slices.Contains(out, normalized) {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeCategory(value Category) Category {
	return Category(strings.ToLower(strings.TrimSpace(string(value))))
}

func knownCategory(value Category) bool {
	switch value {
	case CategoryFilesystem, CategoryShell, CategoryAutomation, CategoryStorage:
		return true
	default:
		return false
	}
}

func normalizeOutputKind(value OutputKind) OutputKind {
	return OutputKind(strings.ToLower(strings.TrimSpace(string(value))))
}

func knownOutputKind(value OutputKind) bool {
	switch value {
	case OutputText, OutputFileContent, OutputFileTree, OutputSearchResults, OutputDiff, OutputJob, OutputStorageEntry, OutputStorageList:
		return true
	default:
		return false
	}
}

func normalizeRiskLevel(value RiskLevel) RiskLevel {
	return RiskLevel(strings.ToLower(strings.TrimSpace(string(value))))
}

func knownRiskLevel(value RiskLevel) bool {
	switch value {
	case RiskSafe, RiskApproval:
		return true
	default:
		return false
	}
}

func normalizeEffect(value Effect) Effect {
	return Effect(strings.ToLower(strings.TrimSpace(string(value))))
}

func knownEffect(value Effect) bool {
	switch value {
	case EffectReadOnly, EffectMutation:
		return true
	default:
		return false
	}
}

func normalizeApprovalMode(value ApprovalMode) ApprovalMode {
	return ApprovalMode(strings.ToLower(strings.TrimSpace(string(value))))
}

func knownApprovalMode(value ApprovalMode) bool {
	switch value {
	case ApprovalNever, ApprovalOnRequest:
		return true
	default:
		return false
	}
}

func cloneSpec(spec Spec) Spec {
	spec.Profiles = slices.Clone(spec.Profiles)
	if spec.InputJSONSchema != nil {
		spec.InputJSONSchema = slices.Clone(spec.InputJSONSchema)
	}
	return spec
}
