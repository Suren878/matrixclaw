package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestSkill(t *testing.T, root string) string {
	t.Helper()
	name := "deploy-helper"
	dir := filepath.Join(root, name)
	description := "Guides safe deployments"
	body := "Use rollout checks."
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\ntags: [deploy]\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestServiceInstallSearchAndTrustTransitions(t *testing.T) {
	tmp := t.TempDir()
	service, err := NewService(Config{DBPath: filepath.Join(tmp, "matrixclaw.db")})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer service.Close()

	source := writeTestSkill(t, tmp)
	installed, err := service.InstallLocal(source, InstallOptions{Provenance: "local-test"})
	if err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if installed.TrustState != TrustQuarantine {
		t.Fatalf("TrustState = %q, want quarantine", installed.TrustState)
	}

	results, err := service.Search("deploy", SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search() returned quarantined skill: %#v", results)
	}

	if err := service.Trust(installed.ID); err != nil {
		t.Fatalf("Trust() error = %v", err)
	}
	results, err = service.Search("deploy", SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != installed.ID {
		t.Fatalf("Search() = %#v", results)
	}
	results, err = service.Search("depl", SearchOptions{})
	if err != nil {
		t.Fatalf("prefix Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != installed.ID {
		t.Fatalf("prefix Search() = %#v", results)
	}

	if err := service.Archive(installed.ID); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	results, err = service.Search("deploy", SearchOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].State != StateArchived {
		t.Fatalf("archived Search() = %#v", results)
	}
}

func TestPromptContextLoadsExplicitTrustedSkillBody(t *testing.T) {
	tmp := t.TempDir()
	service, err := NewService(Config{DBPath: filepath.Join(tmp, "matrixclaw.db")})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer service.Close()

	source := writeTestSkill(t, tmp)
	installed, err := service.InstallLocal(source, InstallOptions{})
	if err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if err := service.Trust(installed.ID); err != nil {
		t.Fatalf("Trust() error = %v", err)
	}

	prompt := service.PromptContext(PromptRequest{
		SessionID: "s1",
		Messages:  []PromptMessage{{Role: "user", Content: "Use $deploy-helper for this release."}},
	})
	if !contains(prompt, "Available trusted skills:") {
		t.Fatalf("prompt missing available metadata:\n%s", prompt)
	}
	if !contains(prompt, "<skill id=\"deploy-helper\"") || !contains(prompt, "Use rollout checks.") {
		t.Fatalf("prompt missing explicit full skill body:\n%s", prompt)
	}
}

func TestDisabledSkillExcludedFromSearchPromptAndUse(t *testing.T) {
	tmp := t.TempDir()
	service, err := NewService(Config{DBPath: filepath.Join(tmp, "matrixclaw.db")})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer service.Close()

	source := writeTestSkill(t, tmp)
	installed, err := service.InstallLocal(source, InstallOptions{TrustState: TrustTrusted})
	if err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if !installed.Enabled {
		t.Fatal("new trusted skill is disabled")
	}
	if err := service.SetEnabled(installed.ID, false); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	results, err := service.Search("deploy", SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search() returned disabled skill: %#v", results)
	}
	if _, err := service.Use("s1", installed.ID); err == nil {
		t.Fatal("Use() disabled skill succeeded, want error")
	}
	prompt := service.PromptContext(PromptRequest{
		SessionID: "s1",
		Messages:  []PromptMessage{{Role: "user", Content: "Use $deploy-helper for this release."}},
	})
	if contains(prompt, "deploy-helper") {
		t.Fatalf("PromptContext() included disabled skill:\n%s", prompt)
	}
}

func TestSessionUseAndUnloadArePerSession(t *testing.T) {
	tmp := t.TempDir()
	service, err := NewService(Config{DBPath: filepath.Join(tmp, "matrixclaw.db")})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer service.Close()

	source := writeTestSkill(t, tmp)
	installed, err := service.InstallLocal(source, InstallOptions{TrustState: TrustTrusted})
	if err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if _, err := service.Use("s1", installed.ID); err != nil {
		t.Fatalf("Use(s1) error = %v", err)
	}
	s1, err := service.SessionSkills("s1")
	if err != nil {
		t.Fatalf("SessionSkills(s1) error = %v", err)
	}
	s2, err := service.SessionSkills("s2")
	if err != nil {
		t.Fatalf("SessionSkills(s2) error = %v", err)
	}
	if len(s1) != 1 || s1[0].ID != installed.ID {
		t.Fatalf("SessionSkills(s1) = %#v", s1)
	}
	if len(s2) != 0 {
		t.Fatalf("SessionSkills(s2) = %#v", s2)
	}
	if err := service.Unload("s1", installed.ID); err != nil {
		t.Fatalf("Unload(s1) error = %v", err)
	}
	s1, err = service.SessionSkills("s1")
	if err != nil {
		t.Fatalf("SessionSkills(s1) after unload error = %v", err)
	}
	if len(s1) != 0 {
		t.Fatalf("SessionSkills(s1) after unload = %#v", s1)
	}
}

func TestCreateDraftCreatesQuarantinedSkill(t *testing.T) {
	tmp := t.TempDir()
	service, err := NewService(Config{DBPath: filepath.Join(tmp, "matrixclaw.db")})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer service.Close()

	draft, err := service.CreateDraft("Matrix UI", "Guides Matrixclaw UI edits", []string{"ui"}, "Use existing pickers.")
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if draft.TrustState != TrustQuarantine || draft.State != StateActive || draft.Enabled {
		t.Fatalf("draft state = trust %q state %q enabled %v", draft.TrustState, draft.State, draft.Enabled)
	}
	detail, err := service.Get(draft.ID)
	if err != nil {
		t.Fatalf("Get(draft) error = %v", err)
	}
	if !contains(detail.Body, "Use existing pickers.") {
		t.Fatalf("draft body = %q", detail.Body)
	}
}

func TestUpdateBodyRewritesSkillAndSearchIndex(t *testing.T) {
	tmp := t.TempDir()
	service, err := NewService(Config{DBPath: filepath.Join(tmp, "matrixclaw.db")})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer service.Close()

	source := writeTestSkill(t, tmp)
	installed, err := service.InstallLocal(source, InstallOptions{TrustState: TrustTrusted})
	if err != nil {
		t.Fatalf("InstallLocal() error = %v", err)
	}
	if err := service.UpdateBody(installed.ID, "Use canary verification before rollout."); err != nil {
		t.Fatalf("UpdateBody() error = %v", err)
	}
	detail, err := service.Get(installed.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !contains(detail.Body, "canary verification") {
		t.Fatalf("updated body = %q", detail.Body)
	}
	results, err := service.Search("canary", SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != installed.ID {
		t.Fatalf("Search(canary) = %#v", results)
	}
}

func contains(value string, fragment string) bool {
	return len(fragment) == 0 || (len(value) >= len(fragment) && indexOf(value, fragment) >= 0)
}

func indexOf(value string, fragment string) int {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return i
		}
	}
	return -1
}
