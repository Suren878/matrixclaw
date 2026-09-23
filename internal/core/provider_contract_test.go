package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestTruncatedOrFilteredReplyFailsTheTurnWithoutRetry(t *testing.T) {
	for _, reason := range []providers.StopReason{providers.StopMaxTokens, providers.StopContentFilter} {
		t.Run(string(reason), func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			calls := 0
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
				calls++
				if err := providers.StreamText(ctx, "Cut off"); err != nil {
					return providers.Response{}, err
				}
				return providers.Response{Text: "Cut off", Provider: "recovery-test", StopReason: reason}, nil
			})})
			session, run := saveCrashRecoveryRun(t, db, "stop-"+string(reason), core.RunStatusAccepted, false)
			err := app.ExecuteRun(context.Background(), run.ID)
			if want := "generation stopped before completion (" + string(reason) + ")"; err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error=%v, want %q", err, want)
			}
			assertRecoveryRunStatus(t, db, run.ID, core.RunStatusFailed)
			if calls != 1 {
				t.Fatalf("model calls=%d, want 1 (not retryable)", calls)
			}
			messages, err := db.ListMessages(context.Background(), session.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			kept := false
			for _, message := range messages {
				kept = kept || (message.Role == transcript.MessageRoleAssistant && strings.Contains(message.Content, "Cut off"))
			}
			if !kept {
				t.Fatal("partial reply was lost")
			}
		})
	}
}

func TestProviderRequestCarriesTheSessionCacheKey(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var cacheKey string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		cacheKey = request.CacheKey
		return providers.Response{Text: "Done", StopReason: providers.StopEndTurn}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cache-key", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if cacheKey != session.ID {
		t.Fatalf("CacheKey=%q, want session id %q", cacheKey, session.ID)
	}
}

func TestRunStepRecordsTheProviderStopReason(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "Cut off", Provider: "recovery-test", StopReason: providers.StopMaxTokens}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "run-step-stop", core.RunStatusAccepted, false)
	_ = app.ExecuteRun(context.Background(), run.ID)
	steps, err := db.ListRunSteps(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].StopReason != "max_tokens" {
		t.Fatalf("run steps=%+v, want one step with stop_reason max_tokens", steps)
	}
}

func TestToolStepReasoningIsSentBackWithItsCalls(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{
				Text:      "Inspecting both.",
				Reasoning: []providers.ReasoningBlock{{RedactedData: "enc-1"}},
				ToolCalls: []providers.ToolCall{
					{ID: "call-a", Name: "inspect_state", Arguments: []byte(`{}`)},
					{ID: "call-b", Name: "inspect_state", Arguments: []byte(`{}`)},
				},
				StopReason: providers.StopToolUse,
			}, nil
		}
		for i, message := range request.Messages {
			if len(message.ToolCalls) == 0 {
				continue
			}
			if message.Content != "Inspecting both." || len(message.ToolCalls) != 2 || len(message.Reasoning) != 1 || message.Reasoning[0].RedactedData != "enc-1" || message.ReasoningContent != nil {
				t.Fatalf("tool step=%+v", message)
			}
			if i+2 >= len(request.Messages) || request.Messages[i+1].ToolCallID != "call-a" || request.Messages[i+2].ToolCallID != "call-b" {
				t.Fatalf("results must follow the step in call order: %+v", request.Messages)
			}
			return providers.Response{Text: "Both inspected.", StopReason: providers.StopEndTurn}, nil
		}
		t.Fatalf("no tool step in %+v", request.Messages)
		return providers.Response{}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "reasoning-replay", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if calls != 2 || tool.callCount() != 2 {
		t.Fatalf("model calls=%d tool calls=%d", calls, tool.callCount())
	}
}
