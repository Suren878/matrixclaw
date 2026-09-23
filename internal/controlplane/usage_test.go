package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
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
	want := "Runs: 2\nSteps: 5\nPrompt: 12k tokens\nCache read: 9.0k tokens\nCache write: 1.5k tokens\nOutput: 800 tokens\nReasoning: 200 tokens"
	if result.Info == nil || result.Info.Text != want || len(result.Info.Rows) != 7 {
		t.Fatalf("usage result=%+v, want text:\n%s", result.Info, want)
	}
}
