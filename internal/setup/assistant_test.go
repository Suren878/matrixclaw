package setup

import (
	"strings"
	"testing"
)

func TestInitializeAssistantSystemPromptAddsCommandContext(t *testing.T) {
	prompt := InitializeAssistantSystemPrompt("Base prompt.")

	for _, want := range []string{
		"Project context:",
		"commands=",
		"/sessions",
		"/provider",
		"/remind",
		"/tasks",
		"/server",
		"/restart",
		"/plan",
		"/usage",
		"/search",
		"automation=enabled",
		"automation_tools=create_reminder,create_scheduled_ai_task",
		"plan_tools=plan_get,plan_set_goal,plan_add_item,plan_update_item,plan_clear",
		"session_runtime=daemon_owned_shared_between_terminal_telegram_future_clients",
		"storage=/modules storage imports_reads_deletes_promotes_temporary_files",
		"/provider -> select or edit provider model/settings",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("InitializeAssistantSystemPrompt() missing %q in:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "/provider key <provider> <key>") {
		t.Fatalf("InitializeAssistantSystemPrompt() exposed legacy provider key shortcut:\n%s", prompt)
	}
}

func TestInitializeAssistantSystemPromptRefreshesExistingProjectContext(t *testing.T) {
	prompt := InitializeAssistantSystemPrompt("Base prompt.\n\nProject context:\n- old=true")

	if strings.Contains(prompt, "old=true") {
		t.Fatalf("InitializeAssistantSystemPrompt() kept stale context:\n%s", prompt)
	}
	if count := strings.Count(prompt, "Project context:"); count != 1 {
		t.Fatalf("Project context count = %d, want 1 in:\n%s", count, prompt)
	}
	if !strings.Contains(prompt, "/sessions") {
		t.Fatalf("InitializeAssistantSystemPrompt() missing refreshed command context:\n%s", prompt)
	}
}

func TestNormalizeAssistantConfigKeepsCustomPrompt(t *testing.T) {
	cfg := normalizeConfig(Config{
		Assistant: AssistantConfig{
			SystemPrompt: "Custom daemon explanation.",
		},
	})

	if cfg.Assistant.SystemPrompt != "Custom daemon explanation." {
		t.Fatalf("SystemPrompt = %q, want custom prompt preserved", cfg.Assistant.SystemPrompt)
	}
}
