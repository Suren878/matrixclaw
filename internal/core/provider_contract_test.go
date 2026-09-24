package core_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestReplyCutFourTimesInARowFailsTheRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		calls++
		text := fmt.Sprintf("Part %d", calls)
		if err := providers.StreamText(ctx, text); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{Text: text, Provider: "recovery-test", StopReason: providers.StopMaxTokens}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cut-four", core.RunStatusAccepted, false)

	err := app.ExecuteRun(context.Background(), run.ID)

	if err == nil || err.Error() != "reply cut by the output limit 4 times in a row" {
		t.Fatalf("error = %v", err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusFailed)
	if calls != 4 {
		t.Fatalf("model calls = %d, want 4", calls)
	}
	parts, notes := 0, 0
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleAssistant && strings.HasPrefix(message.Content, "Part ") {
			parts++
		}
		if message.Origin == transcript.OriginEngine {
			notes++
		}
	}
	if parts != 4 || notes != 3 {
		t.Fatalf("kept parts = %d continuation notes = %d, want 4 and 3", parts, notes)
	}
}

func TestFilteredReplyCompletesTheRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Provider: "recovery-test", StopReason: providers.StopContentFilter}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "filtered", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil || stored.Status != core.RunStatusCompleted || stored.StopReason != agent.StopDone {
		t.Fatalf("run = %+v err = %v", stored, err)
	}
	if !hasAssistantContent(sessionMessages(t, db, session.ID), run.ID, "The provider stopped this reply (content_filter).") {
		t.Fatal("filtered reply has no visible note")
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
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{Text: "Cut off", Provider: "recovery-test", StopReason: providers.StopMaxTokens}, nil
		}
		return providers.Response{Text: "and finished.", Provider: "recovery-test", StopReason: providers.StopEndTurn}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "run-step-stop", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	steps, err := db.ListRunSteps(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].StopReason != "max_tokens" || steps[1].StopReason != "end_turn" {
		t.Fatalf("run steps=%+v, want max_tokens then end_turn", steps)
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

type identifiedRuntime struct {
	generationRuntimeFunc
	provider, model string
}

func (r identifiedRuntime) Identity() (string, string) { return r.provider, r.model }

func TestSignedReasoningIsDroppedAfterAModelSwitch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		model      string
		wantSigned int
	}{
		{name: "same model", model: "claude-a", wantSigned: 1},
		{name: "switched model", model: "gemini-b", wantSigned: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			app.WithTools(tools.NewRegistry(&recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}))
			calls := 0
			runtime := identifiedRuntime{provider: providers.TypeAnthropic, model: tc.model}
			runtime.generationRuntimeFunc = func(_ context.Context, request providers.Request) (providers.Response, error) {
				calls++
				if calls == 1 {
					return providers.Response{
						Provider:   providers.TypeAnthropic,
						Model:      "claude-a",
						Reasoning:  []providers.ReasoningBlock{{Text: "thinking", Signature: "sig-a"}},
						ToolCalls:  []providers.ToolCall{{ID: "call-a", Name: "inspect_state", Arguments: []byte(`{}`)}},
						StopReason: providers.StopToolUse,
					}, nil
				}
				for _, message := range request.Messages {
					if len(message.ToolCalls) > 0 && len(message.Reasoning) != tc.wantSigned {
						t.Fatalf("tool step=%+v, want %d signed blocks", message, tc.wantSigned)
					}
				}
				return providers.Response{Text: "Done.", StopReason: providers.StopEndTurn}, nil
			}
			app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
			_, run := saveCrashRecoveryRun(t, db, "model-switch-"+tc.model, core.RunStatusAccepted, false)
			if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
				t.Fatal(err)
			}
			assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
			if calls != 2 {
				t.Fatalf("model calls=%d", calls)
			}
		})
	}
}
