package daemoncmd

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules"
	telephonymodule "github.com/Suren878/matrixclaw/internal/modules/telephony"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestStatusNoteFollowsAppliedModules(t *testing.T) {
	set, err := modules.NewSet(nil, telephonymodule.New(), modules.Static("storage", "Storage", ""))
	if err != nil {
		t.Fatal(err)
	}
	s := newSupervisor(context.Background(), nil, nil, set)
	cfg := setup.Config{Modules: setup.ModulesConfig{
		Telephony:      setup.TelephonyConfig{Enabled: true, GatewayURL: "http://gw"},
		ExternalAgents: map[string]setup.ExternalAgentConfig{"codex-app": {Enabled: true}},
	}}
	if err := s.applyBootstrap(bootstrapConfig{Setup: cfg}); err != nil {
		t.Fatal(err)
	}
	note := s.RuntimeStatusPromptContext(context.Background(), core.RuntimeStatusContextRequest{ToolIDs: []string{"telephony_call"}})
	for _, want := range []string{"telephony: enabled=true; ready=true; gateway_configured=yes; token_configured=no; tools=1", "storage: enabled=true; ready=true; tools=0", "external_agents: codex-app=enabled"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "Local ·") || strings.Contains(note, "Configured") {
		t.Fatalf("note carries changing state text:\n%s", note)
	}
}
