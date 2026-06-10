package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type testSkillsContext struct {
	seenSessionID string
}

func (p *testSkillsContext) SkillsPromptContext(_ context.Context, req SkillsPromptContextRequest) string {
	p.seenSessionID = req.SessionID
	return "Skills context:\n- deploy-helper"
}

func TestProviderSystemPromptIncludesSkillsContext(t *testing.T) {
	provider := &testSkillsContext{}
	core := New(nil).WithSkillsContext(provider)
	turn := turnExecution{SessionID: "s1", WorkingDir: "/tmp/project"}

	prompt := core.providerSystemPrompt(context.Background(), turn, AssistantProfile{SystemPrompt: "Base prompt"}, "", nil)
	if provider.seenSessionID != "s1" {
		t.Fatalf("SessionID seen by skills context = %q", provider.seenSessionID)
	}
	if !strings.Contains(prompt, "Skills context:\n- deploy-helper") {
		t.Fatalf("prompt missing skills context:\n%s", prompt)
	}
}

type testRuntimeStatusContext struct {
	seenSessionID string
	seenToolIDs   []string
}

func (p *testRuntimeStatusContext) RuntimeStatusPromptContext(_ context.Context, req RuntimeStatusContextRequest) string {
	p.seenSessionID = req.SessionID
	p.seenToolIDs = append([]string(nil), req.ToolIDs...)
	return "Current runtime status (fresh for this request):\n- browser: disabled; browser_tools=unavailable"
}

func TestProviderSystemPromptIncludesRuntimeStatusContext(t *testing.T) {
	provider := &testRuntimeStatusContext{}
	core := New(nil).
		WithTools(tools.NewRegistry(promptTool{id: "web_search"}, promptTool{id: "mcp_browser_navigate"})).
		WithRuntimeStatusContext(provider)
	turn := turnExecution{SessionID: "s1", WorkingDir: "/tmp/project"}

	prompt := core.providerSystemPrompt(context.Background(), turn, AssistantProfile{SystemPrompt: "Base prompt"}, "", nil)
	if provider.seenSessionID != "s1" {
		t.Fatalf("SessionID seen by runtime status context = %q", provider.seenSessionID)
	}
	if !stringSliceContains(provider.seenToolIDs, "web_search") || !stringSliceContains(provider.seenToolIDs, "mcp_browser_navigate") {
		t.Fatalf("ToolIDs seen by runtime status context = %#v, want current provider tool IDs", provider.seenToolIDs)
	}
	if !strings.Contains(prompt, "Current runtime status (fresh for this request):\n- browser: disabled; browser_tools=unavailable") {
		t.Fatalf("prompt missing runtime status context:\n%s", prompt)
	}
}

func TestRuntimeStatusContextOmitsToolIDsWhenRuntimeDisablesTools(t *testing.T) {
	provider := &testRuntimeStatusContext{}
	core := New(nil).
		WithTools(tools.NewRegistry(promptTool{id: "web_search"})).
		WithRuntimeStatusContext(provider)
	turn := turnExecution{
		SessionID:  "s1",
		WorkingDir: "/tmp/project",
		Runtime:    disabledToolsRuntime{},
	}

	_ = core.providerSystemPrompt(context.Background(), turn, AssistantProfile{SystemPrompt: "Base prompt"}, "", nil)
	if len(provider.seenToolIDs) != 0 {
		t.Fatalf("ToolIDs seen by runtime status context = %#v, want none when current runtime disables tools", provider.seenToolIDs)
	}
}

func TestProviderSystemPromptIncludesToolUseDiscipline(t *testing.T) {
	core := New(nil).WithTools(tools.NewRegistry(promptTool{id: "web_search"}))
	turn := turnExecution{SessionID: "s1", WorkingDir: "/tmp/project"}

	prompt := core.providerSystemPrompt(context.Background(), turn, AssistantProfile{SystemPrompt: "Base prompt"}, "", nil)
	for _, want := range []string{
		"Tool use discipline:",
		"Use the fewest tool calls",
		"existing tool results already contain the requested answer",
		"stop tool use and reply",
		"Do not run extra searches, browser snapshots, or verification calls",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}

	disabledPrompt := core.providerSystemPrompt(context.Background(), turnExecution{SessionID: "s1", Runtime: disabledToolsRuntime{}}, AssistantProfile{SystemPrompt: "Base prompt"}, "", nil)
	if strings.Contains(disabledPrompt, "Tool use discipline:") {
		t.Fatalf("prompt includes tool discipline when tool use is disabled:\n%s", disabledPrompt)
	}
}

func TestProviderSystemPromptGuidesSpecificSiteLookupsToBrowser(t *testing.T) {
	core := New(nil).WithTools(tools.NewRegistry(
		promptTool{id: "web_research"},
		promptTool{id: "web_search"},
		promptTool{id: "mcp_browser_browser_navigate"},
	))
	turn := turnExecution{SessionID: "s1", WorkingDir: "/tmp/project"}

	prompt := core.providerSystemPrompt(context.Background(), turn, AssistantProfile{SystemPrompt: "Base prompt"}, "", nil)
	for _, want := range []string{
		"specific website/domain/page",
		"prefer the site's own navigation/search in the browser",
		"Use web_search only as a fallback",
		"keep it to one focused query",
		"Do not combine web_research, web_search, and browser tools for a simple single-page lookup",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

type promptTool struct {
	id string
}

func (t promptTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              t.id,
		Name:            t.id,
		Description:     "Prompt test tool.",
		Namespace:       "test",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Category:        tools.CategoryWeb,
		OutputKind:      tools.OutputText,
		Profiles:        []tools.Profile{tools.ProfileWeb},
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func (t promptTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{}, nil
}

type disabledToolsRuntime struct{}

func (disabledToolsRuntime) Generate(context.Context, providers.Request) (providers.Response, error) {
	return providers.Response{}, nil
}

func (disabledToolsRuntime) RuntimeProfile() providers.RuntimeProfile {
	return providers.RuntimeProfile{ToolUseMode: providers.ToolUseDisabled}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
