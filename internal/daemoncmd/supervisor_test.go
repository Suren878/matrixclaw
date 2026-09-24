package daemoncmd

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type fakeProfileSink struct {
	profile core.AssistantProfile
}

func (f *fakeProfileSink) SetAssistantProfile(profile core.AssistantProfile) {
	f.profile = profile
}

func TestApplyAssistantProfileAppendsModuleContext(t *testing.T) {
	sink := &fakeProfileSink{}
	moduleContext := func() []string { return []string{"storage: enabled", "mcp: enabled"} }

	applyAssistantProfile(sink, core.AssistantProfile{SystemPrompt: "Be helpful."}, moduleContext)

	if !strings.Contains(sink.profile.SystemPrompt, "storage: enabled") {
		t.Fatalf("expected system prompt to contain module context, got %q", sink.profile.SystemPrompt)
	}
	if !strings.Contains(sink.profile.SystemPrompt, "mcp: enabled") {
		t.Fatalf("expected system prompt to contain module context, got %q", sink.profile.SystemPrompt)
	}
	if !strings.Contains(sink.profile.SystemPrompt, "Be helpful.") {
		t.Fatalf("expected system prompt to retain base prompt, got %q", sink.profile.SystemPrompt)
	}
}
