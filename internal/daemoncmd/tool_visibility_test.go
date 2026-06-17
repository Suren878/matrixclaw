package daemoncmd

import (
	"context"
	"path/filepath"
	"testing"

	telephonymodule "github.com/Suren878/matrixclaw/internal/modules/telephony"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestSetupAwareToolExecutorFiltersTelephonyWhenUnavailable(t *testing.T) {
	service := testSetupService(t, setup.TelephonyConfig{Enabled: false})
	executor := newSetupAwareToolExecutor(testToolRegistry(t), service)

	if _, ok := executor.Spec(telephonymodule.CallToolID); ok {
		t.Fatalf("telephony_call should be hidden when telephony is disabled")
	}
	if _, ok := executor.Spec(telephonymodule.EndCallToolID); !ok {
		t.Fatalf("telephony_end_call should remain visible for active telephony sessions")
	}
	if _, ok := executor.Spec("web_search"); !ok {
		t.Fatalf("non-telephony tool should remain visible")
	}
}

func TestSetupAwareToolExecutorShowsTelephonyWhenConfigured(t *testing.T) {
	service := testSetupService(t, setup.TelephonyConfig{
		Enabled:    true,
		GatewayURL: "http://127.0.0.1:8090",
	})
	executor := newSetupAwareToolExecutor(testToolRegistry(t), service)

	if _, ok := executor.Spec(telephonymodule.CallToolID); !ok {
		t.Fatalf("telephony_call should be visible when telephony is configured")
	}
}

func testSetupService(t *testing.T, telephony setup.TelephonyConfig) *setup.Service {
	t.Helper()
	store := setup.NewFileStore(filepath.Join(t.TempDir(), "setup.json"))
	if err := store.Save(setup.Config{Modules: setup.ModulesConfig{Telephony: telephony}}); err != nil {
		t.Fatalf("save setup: %v", err)
	}
	return setup.NewService(store)
}

func testToolRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	registry := tools.NewRegistry(
		testTool{id: telephonymodule.CallToolID},
		testTool{id: telephonymodule.EndCallToolID},
		testTool{id: "web_search"},
	)
	if err := registry.Err(); err != nil {
		t.Fatalf("tool registry: %v", err)
	}
	return registry
}

type testTool struct {
	id string
}

func (t testTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              t.id,
		Name:            t.id,
		Description:     t.id + " test tool",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "test",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileAutomation},
		OutputKind:      tools.OutputText,
		InputJSONSchema: []byte(`{"type":"object","additionalProperties":false}`),
	}
}

func (t testTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Content: t.id, Status: tools.ResultStatusSuccess}, nil
}
