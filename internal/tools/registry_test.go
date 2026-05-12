package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type registryTestExecutor struct {
	spec Spec
}

func (e registryTestExecutor) Spec() Spec {
	return e.spec
}

func (e registryTestExecutor) Execute(context.Context, Call) (Result, error) {
	return Result{Content: e.spec.ID}, nil
}

func registryTestSpec(id string) Spec {
	return Spec{
		ID:              id,
		Name:            id,
		Description:     id + " test tool",
		Risk:            RiskSafe,
		Namespace:       "test.registry",
		Category:        CategoryFilesystem,
		Profiles:        []Profile{ProfileCoding},
		OutputKind:      OutputText,
		InputJSONSchema: rawSchema(`{"type":"object"}`),
	}
}

func TestRegistryListIsRegistrationOrdered(t *testing.T) {
	registry := NewRegistry(
		registryTestExecutor{spec: registryTestSpec("beta")},
		registryTestExecutor{spec: registryTestSpec("alpha")},
		registryTestExecutor{spec: registryTestSpec("gamma")},
	)
	if err := registry.Err(); err != nil {
		t.Fatalf("registry.Err() = %v", err)
	}

	got := registry.List()
	if len(got) != 3 {
		t.Fatalf("List() returned %d specs, want 3", len(got))
	}
	wantIDs := []string{"beta", "alpha", "gamma"}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("List()[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
}

func TestRegistryRejectsDuplicateToolID(t *testing.T) {
	registry := NewRegistry(registryTestExecutor{spec: registryTestSpec("Read")})
	duplicateSpec := registryTestSpec(" read ")
	err := registry.Register(registryTestExecutor{spec: duplicateSpec})
	if err == nil {
		t.Fatal("Register() error = nil, want duplicate error")
	}
	var duplicate DuplicateToolError
	if !errors.As(err, &duplicate) {
		t.Fatalf("Register() error = %T %v, want DuplicateToolError", err, err)
	}

	got := registry.List()
	if len(got) != 1 {
		t.Fatalf("List() returned %d specs, want original only", len(got))
	}
	if got[0].ID != "Read" {
		t.Fatalf("duplicate registration replaced original ID: %q", got[0].ID)
	}
	if _, err := registry.Execute(context.Background(), "read", Call{}); err != nil {
		t.Fatalf("Execute() after duplicate registration = %v", err)
	}
}

func TestRegistryListReturnsClonedSpecs(t *testing.T) {
	spec := registryTestSpec("read")
	spec.Profiles = []Profile{ProfileReadOnly}
	registry := NewRegistry(registryTestExecutor{spec: spec})

	first := registry.List()
	first[0].Profiles[0] = ProfileCoding
	first[0].InputJSONSchema[0] = '['

	second := registry.List()
	if second[0].Profiles[0] != ProfileReadOnly {
		t.Fatalf("Profiles mutated through List(): %#v", second[0].Profiles)
	}
	if strings.HasPrefix(string(second[0].InputJSONSchema), "[") {
		t.Fatalf("InputJSONSchema mutated through List(): %s", second[0].InputJSONSchema)
	}
}

func TestRegistryPolicyViewFiltersListAndExecute(t *testing.T) {
	readSpec := registryTestSpec("read")
	readSpec.Profiles = []Profile{ProfileReadOnly}
	bashSpec := registryTestSpec("bash")
	bashSpec.Category = CategoryShell
	writeSpec := registryTestSpec("write")
	registry := NewRegistry(
		registryTestExecutor{spec: readSpec},
		registryTestExecutor{spec: bashSpec},
		registryTestExecutor{spec: writeSpec},
	)

	view := registry.View(Policy{
		Profiles:   []Profile{ProfileReadOnly},
		IncludeIDs: []string{"write"},
		ExcludeIDs: []string{"bash"},
	})
	got := view.List()
	if len(got) != 2 || got[0].ID != "read" || got[1].ID != "write" {
		t.Fatalf("filtered List() = %#v, want read and write in registration order", got)
	}

	if _, err := view.Execute(context.Background(), "write", Call{}); err != nil {
		t.Fatalf("Execute(write) = %v", err)
	}
	if _, err := view.Execute(context.Background(), "bash", Call{}); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("Execute(bash) error = %v, want disabled tool error", err)
	}
}

func TestRegistryRejectsInvalidToolPolicyMetadata(t *testing.T) {
	spec := registryTestSpec("mystery")
	spec.Category = "unknown"
	registry := NewRegistry(registryTestExecutor{spec: spec})
	var invalid InvalidToolSpecError
	if !errors.As(registry.Err(), &invalid) {
		t.Fatalf("registry.Err() = %v, want InvalidToolSpecError", registry.Err())
	}
	if got := registry.List(); len(got) != 0 {
		t.Fatalf("List() = %#v, want invalid tool omitted", got)
	}
}
