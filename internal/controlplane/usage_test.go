package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
)

type tokenReportRuntime struct {
	usage   core.UsageReport
	context core.ContextReport
}

func (r tokenReportRuntime) CurrentBinding(context.Context, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: "s1"}, nil
}

func (r tokenReportRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return []core.Session{{ID: "s1", Title: "s1"}}, nil
}

func (r tokenReportRuntime) GetSession(ctx context.Context, id string) (core.Session, error) {
	sessions, _ := r.ListSessions(ctx)
	return sessionWithID(sessions, id)
}

func (r tokenReportRuntime) CreateSession(context.Context, string, string, string) (core.Session, error) {
	return core.Session{}, core.ErrInvalidInput
}

func (r tokenReportRuntime) UseSession(context.Context, string, string) (core.ClientBinding, error) {
	return core.ClientBinding{}, core.ErrInvalidInput
}

func (r tokenReportRuntime) RenameSession(context.Context, string, string) (core.Session, error) {
	return core.Session{}, core.ErrInvalidInput
}

func (r tokenReportRuntime) DeleteSession(context.Context, string) error {
	return core.ErrInvalidInput
}

func (r tokenReportRuntime) SessionUsage(context.Context, string) (core.UsageReport, error) {
	return r.usage, nil
}

func (r tokenReportRuntime) SessionContext(context.Context, string) (core.ContextReport, error) {
	return r.context, nil
}

func (r tokenReportRuntime) CompactSession(context.Context, string) (core.CompactSessionResult, error) {
	return core.CompactSessionResult{}, core.ErrInvalidInput
}

func TestUsageCommandShowsStepsAndCacheTokens(t *testing.T) {
	runtime := tokenReportRuntime{usage: core.UsageReport{Summary: core.UsageSummary{
		Runs: 2, Steps: 5, PromptTokens: 12_000, CacheReadTokens: 9_000, CacheWriteTokens: 1_500, OutputTokens: 800, ReasoningTokens: 200,
	}}}
	result, err := New(runtime, "").Handle(context.Background(), "key", "/usage")
	if err != nil {
		t.Fatal(err)
	}
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
		runtime := tokenReportRuntime{context: core.ContextReport{SessionID: "s1", LastProviderUsage: &usage}}
		result, err := New(runtime, "").Handle(context.Background(), "key", "/context info")
		if err != nil {
			t.Fatal(err)
		}
		if result.Info == nil || !strings.Contains(result.Info.Text, tc.want) {
			t.Errorf("context info=%+v, want line %q", result.Info, tc.want)
		}
	}
}

func sessionWithID(sessions []core.Session, id string) (core.Session, error) {
	for _, session := range sessions {
		if session.ID == id {
			return session, nil
		}
	}
	return core.Session{}, core.ErrNotFound
}
