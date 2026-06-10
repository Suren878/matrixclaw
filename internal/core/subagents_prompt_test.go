package core

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestProviderSystemPromptMentionsDelegateTaskWhenAvailable(t *testing.T) {
	app := New(nil).WithTools(tools.NewRegistry(DelegateTaskToolExecutor(New(nil))))
	prompt := app.providerSystemPrompt(context.Background(), turnExecution{SessionID: "s1"}, AssistantProfile{}, "", nil)

	if !strings.Contains(prompt, "delegate_task") {
		t.Fatalf("system prompt missing delegate_task guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "subagent") {
		t.Fatalf("system prompt missing subagent guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "matrixclaw") {
		t.Fatalf("system prompt missing matrixclaw runtime:\n%s", prompt)
	}
	if !strings.Contains(prompt, "do not ask which runtime") {
		t.Fatalf("system prompt missing single-runtime guidance:\n%s", prompt)
	}
}

func TestProviderSystemPromptListsExternalSubagentConfiguration(t *testing.T) {
	registry, err := externalagents.NewRegistry(
		promptExternalAgent{id: "codex-app", aliases: []string{"codex"}, display: "Codex", installed: true, enabled: true, models: []string{"gpt-5.4-mini"}},
		promptExternalAgent{id: "claude-code", aliases: []string{"claude"}, display: "Claude Code", installed: true, enabled: false, detail: "disabled in setup"},
	)
	if err != nil {
		t.Fatalf("external registry: %v", err)
	}
	app := New(nil).
		WithTools(tools.NewRegistry(DelegateTaskToolExecutor(New(nil)))).
		WithExternalAgents(registry, nil)
	prompt := app.providerSystemPrompt(context.Background(), turnExecution{SessionID: "s1"}, AssistantProfile{}, "", nil)

	for _, want := range []string{
		"matrixclaw: available",
		"codex: available",
		"Claude Code",
		"claude: unavailable",
		"Runtime IDs available for delegate_task: matrixclaw, codex",
		"answer exactly: matrixclaw, codex",
		"gpt-5.4-mini",
		"ask the user which runtime",
		"Do not select unavailable runtimes",
		"questions in any language about available or connected subagents",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestProviderDelegateTaskToolDescriptionIncludesRuntimeChoices(t *testing.T) {
	registry, err := externalagents.NewRegistry(
		promptExternalAgent{id: "codex-app", aliases: []string{"codex"}, display: "Codex", installed: true, enabled: true},
	)
	if err != nil {
		t.Fatalf("external registry: %v", err)
	}
	app := New(nil).
		WithTools(tools.NewRegistry(DelegateTaskToolExecutor(New(nil)))).
		WithExternalAgents(registry, nil)
	definitions := app.providerToolDefinitions(context.Background(), turnExecution{})

	var description string
	for _, definition := range definitions {
		if definition.Name == "delegate_task" {
			description = definition.Description
			break
		}
	}
	if !strings.Contains(description, "matrixclaw") || !strings.Contains(description, "codex") {
		t.Fatalf("delegate_task description missing runtime choices: %q", description)
	}
}

type promptExternalAgent struct {
	id        string
	aliases   []string
	display   string
	installed bool
	enabled   bool
	models    []string
	detail    string
}

func (a promptExternalAgent) ID() string { return a.id }

func (a promptExternalAgent) Aliases() []string { return a.aliases }

func (a promptExternalAgent) DisplayName() string { return a.display }

func (a promptExternalAgent) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{
		Installed: a.installed,
		Enabled:   a.enabled,
		Detail:    a.detail,
	}
}

func (a promptExternalAgent) Models(context.Context) []string { return a.models }
