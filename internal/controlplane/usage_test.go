package controlplane

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageCommandShowsStepsAndCacheTokens(t *testing.T) {
	daemon := newFakeDaemon(t).on("GET /v1/sessions/{id}/usage", func(*http.Request) any {
		return core.UsageResponse{Usage: core.UsageReport{Summary: core.UsageSummary{
			Runs: 2, Steps: 5, PromptTokens: 12_000, CacheReadTokens: 9_000, CacheWriteTokens: 1_500, OutputTokens: 800, ReasoningTokens: 200,
		}}}
	})

	result := daemon.run("/usage")

	want := "Runs: 2\nSteps: 5\nPrompt: 12k tokens\nCache read: 9.0k tokens\nCache write: 1.5k tokens\nCache hit: 75%\nOutput: 800 tokens\nReasoning: 200 tokens"
	if result.Info == nil || result.Info.Text != want || len(result.Info.Rows) != 8 {
		t.Fatalf("usage result=%+v, want text:\n%s", result.Info, want)
	}
}

func TestContextInfoShowsCachedPromptTokens(t *testing.T) {
	for _, tc := range []struct {
		usage providers.Usage
		want  string
	}{
		{providers.Usage{PromptTokens: 12_000, OutputTokens: 800}, "Last provider usage: 12k in / 800 out"},
		{providers.Usage{PromptTokens: 12_000, CacheReadTokens: 9_000, CacheWriteTokens: 1_500, OutputTokens: 800}, "Last provider usage: 12k in (9.0k cached, 1.5k written) / 800 out"},
		{providers.Usage{ProviderRaw: []byte(`{}`)}, "Last provider usage: reported"},
	} {
		usage := tc.usage
		daemon := newFakeDaemon(t).on("GET /v1/sessions/{id}/context", func(*http.Request) any {
			return core.SessionContextResponse{Context: core.ContextReport{SessionID: "s1", LastProviderUsage: &usage}}
		})

		if result := daemon.run("/context info"); result.Info == nil || !strings.Contains(result.Info.Text, tc.want) {
			t.Errorf("context info=%+v, want line %q", result.Info, tc.want)
		}
	}
}
