package modules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type testTool struct{ id string }

func (t testTool) Spec() tools.Spec {
	return tools.Spec{ID: t.id, Description: t.id, Namespace: "test", Category: tools.CategoryAutomation, Effect: tools.EffectReadOnly, InputJSONSchema: json.RawMessage(`{"type":"object"}`)}
}

func (t testTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Content: t.id}, nil
}

// switchable offers its tool while telephony is enabled in the applied setup.
type switchable struct{ on bool }

func (m *switchable) ID() string { return "switchable" }
func (m *switchable) Apply(_ context.Context, cfg setup.Config) error {
	m.on = cfg.Modules.Telephony.Enabled
	return nil
}
func (m *switchable) Tools() []tools.Executor {
	if !m.on {
		return nil
	}
	return []tools.Executor{testTool{id: "module_tool"}}
}
func (m *switchable) Context() string { return "switchable context" }
func (m *switchable) Status(context.Context) Status {
	return Status{ID: m.ID(), Enabled: m.on}
}
func (m *switchable) Close() error { return nil }

func TestSetOffersModuleToolsAsModulesApply(t *testing.T) {
	set, err := NewSet([]tools.Executor{testTool{id: "base_tool"}}, &switchable{}, Static("static", "Static", "", testTool{id: "static_tool"}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, ok := set.Spec("static_tool"); ok {
		t.Fatal("module tools offered before the first Apply")
	}
	if err := set.Apply(ctx, setup.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := set.Spec("module_tool"); ok {
		t.Fatal("tool of a module that is off is offered")
	}
	if len(set.List()) != 2 {
		t.Fatalf("tools = %v", set.List())
	}
	on := setup.Config{Modules: setup.ModulesConfig{Telephony: setup.TelephonyConfig{Enabled: true}}}
	if err := set.Apply(ctx, on); err != nil {
		t.Fatal(err)
	}
	result, err := set.Execute(ctx, "module_tool", tools.Call{})
	if err != nil || result.Content != "module_tool" {
		t.Fatalf("execute = %+v, %v", result, err)
	}
	statuses := set.Statuses(ctx)
	if len(statuses) != 2 || len(statuses[0].Tools) != 1 || statuses[0].Tools[0] != "module_tool" || !statuses[1].Ready {
		t.Fatalf("statuses = %+v", statuses)
	}
	if got := set.Context(); len(got) != 1 || got[0] != "switchable context" {
		t.Fatalf("context = %v", got)
	}
	if err := set.Apply(ctx, setup.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Execute(ctx, "module_tool", tools.Call{}); err == nil {
		t.Fatal("tool of a module turned off still runs")
	}
}

func TestSetKeepsTheFirstToolAndReportsADuplicateInStatus(t *testing.T) {
	set, err := NewSet([]tools.Executor{testTool{id: "same"}}, Static("dup", "Dup", "", testTool{id: "same"}, testTool{id: "own"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Apply(context.Background(), setup.Config{}); err != nil {
		t.Fatalf("apply failed on a duplicate tool: %v", err)
	}
	if _, ok := set.Spec("same"); !ok {
		t.Fatal("base tool lost after a duplicate")
	}
	if _, ok := set.Spec("own"); !ok {
		t.Fatal("the module's other tool is not offered")
	}
	status := set.Statuses(context.Background())[0]
	if !strings.Contains(status.Detail, `duplicate tool id "same"`) {
		t.Fatalf("status detail = %q", status.Detail)
	}
}
