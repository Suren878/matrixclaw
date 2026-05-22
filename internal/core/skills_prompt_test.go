package core

import (
	"context"
	"strings"
	"testing"
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
