package tools

import (
	"context"
	"testing"
)

func TestConcurrencyKeyDefaults(t *testing.T) {
	call := Call{WorkingDir: "/work/repo/"}
	for _, tc := range []struct {
		name string
		spec Spec
		want string
	}{
		{"read-only file tool", coreDefinitionSpec(readToolName), ""},
		{"grep", coreDefinitionSpec(grepToolName), ""},
		{"edit", coreDefinitionSpec(editToolName), "dir:/work/repo"},
		{"bash", coreDefinitionSpec(bashToolName), "dir:/work/repo"},
		{"read-only MCP tool", Spec{Namespace: "mcp.browser", Effect: EffectReadOnly}, "mcp.browser"},
		{"mutating MCP tool", Spec{Namespace: "MCP.Browser", Effect: EffectMutation, Category: CategoryWeb}, "mcp.browser"},
		{"other mutating tool", Spec{Namespace: "module.storage", Effect: EffectMutation, Category: CategoryStorage}, "tool:module.storage"},
		{"other read-only tool", Spec{Namespace: "module.storage", Effect: EffectReadOnly, Category: CategoryStorage}, ""},
	} {
		if got := tc.spec.ConcurrencyKey(call); got != tc.want {
			t.Errorf("%s: key = %q, want %q", tc.name, got, tc.want)
		}
	}
}

type keyedExecutor struct{ spec Spec }

func (e keyedExecutor) Spec() Spec { return e.spec }
func (e keyedExecutor) Execute(context.Context, Call) (Result, error) {
	return Result{}, nil
}
func (e keyedExecutor) ConcurrencyKey(call Call) string { return "own:" + call.WorkingDir }

func TestRegistryConcurrencyKeyPrefersTheExecutorsOwnKey(t *testing.T) {
	own := keyedExecutor{spec: Spec{ID: "own", Name: "Own", Description: "own key", Risk: RiskSafe, Namespace: "test", Effect: EffectMutation, Category: CategoryAutomation, Profiles: []Profile{ProfileCoding}, OutputKind: OutputText, InputJSONSchema: []byte(`{}`)}}
	registry := NewRegistry(append(NewShellExecutors(nil), own)...)
	if err := registry.Err(); err != nil {
		t.Fatal(err)
	}
	call := Call{WorkingDir: "/work"}

	if got := registry.ConcurrencyKey("bash", call); got != "dir:/work" {
		t.Fatalf("bash key = %q", got)
	}
	if got := registry.ConcurrencyKey("own", call); got != "own:/work" {
		t.Fatalf("own key = %q", got)
	}
	if got := registry.ConcurrencyKey("missing", call); got != "" {
		t.Fatalf("unknown tool key = %q", got)
	}
}
