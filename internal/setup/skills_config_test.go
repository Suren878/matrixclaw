package setup

import "testing"

func TestNormalizeModulesConfigDefaultsSkills(t *testing.T) {
	modules := normalizeModulesConfig(ModulesConfig{})
	if !modules.Skills.Enabled {
		t.Fatal("Skills.Enabled = false, want true")
	}
	if !modules.Skills.AutoInvoke {
		t.Fatal("Skills.AutoInvoke = false, want true")
	}
	if modules.Skills.TrustPolicy != "quarantine" {
		t.Fatalf("TrustPolicy = %q", modules.Skills.TrustPolicy)
	}
	if modules.Skills.SelfImprove != "drafts" {
		t.Fatalf("SelfImprove = %q", modules.Skills.SelfImprove)
	}
}
