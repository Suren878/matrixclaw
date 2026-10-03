package controlplane

import (
	"slices"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestNonOwnersSeeNoSettingsOrDaemonControls(t *testing.T) {
	daemon := newProviderDaemon(t)
	daemon.providers = []setup.ProviderSetupItem{
		{ID: "openai", Name: "OpenAI", Configured: true},
		{ID: "gemini", Name: "Gemini"},
	}

	if ids := pickerItemIDs(daemon.runAs(core.RoleMember, "/server")); !slices.Equal(ids, []string{"status"}) {
		t.Fatalf("member server menu = %v", ids)
	}
	if result := daemon.runAs(core.RoleMember, "/restart"); result.Confirm != nil {
		t.Fatalf("member restart = %+v", result)
	}
	if result := daemon.runAs(core.RoleMember, "/provider custom"); result.Text != ownerOnlySettings {
		t.Fatalf("member custom provider = %+v", result)
	}
	if ids := pickerItemIDs(daemon.runAs(core.RoleMember, "/provider")); !slices.Equal(ids, []string{"openai"}) {
		t.Fatalf("member provider picker = %v", ids)
	}
	if ids := pickerItemIDs(daemon.runAs(core.RoleOwner, "/provider")); !slices.Equal(ids, []string{"openai", "gemini", "custom"}) {
		t.Fatalf("owner provider picker = %v", ids)
	}
}
