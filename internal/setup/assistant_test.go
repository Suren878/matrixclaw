package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadAssistant(t *testing.T, prompt string) AssistantConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup.json")
	quoted, _ := json.Marshal(prompt)
	data := []byte(`{"version":3,"assistant":{"system_prompt":` + string(quoted) + `}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(path)
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg, err = store.Load(); err != nil {
		t.Fatal(err)
	}
	return cfg.Assistant
}

func TestSavedBuiltInPromptIsNotAnOverride(t *testing.T) {
	for _, stored := range []string{
		"",
		savedDefaultSystemPrompt,
		savedDefaultSystemPrompt + "\n\nProject context:\n- project_root=/home/u/matrixclaw\n- approvals=write_shell_skill_manage_and_risky_tools_need_permission",
	} {
		assistant := loadAssistant(t, stored)
		if assistant.SystemPrompt != "" {
			t.Fatalf("stored %q kept as override %q", stored, assistant.SystemPrompt)
		}
		if got := assistant.SystemPromptOrDefault(); got != DefaultAssistantSystemPrompt {
			t.Fatalf("effective prompt = %q", got)
		}
	}
}

func TestUserPromptIsKeptWithoutProjectContext(t *testing.T) {
	assistant := loadAssistant(t, "Answer like a pirate.\n\nProject context:\n- project_root=/tmp")
	if assistant.SystemPrompt != "Answer like a pirate." || assistant.SystemPromptOrDefault() != "Answer like a pirate." {
		t.Fatalf("system prompt = %q", assistant.SystemPrompt)
	}
}
