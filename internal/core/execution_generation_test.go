package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type generationRuntimeFunc func(context.Context, providers.Request) (providers.Response, error)

func (f generationRuntimeFunc) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	return f(ctx, request)
}

func TestToolTurnPersistsFinalCommentaryAndUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-streaming", true: "streaming"}[stream], func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
			app.WithTools(tools.NewRegistry(tool))
			calls := 0
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
				calls++
				if calls == 1 {
					if stream {
						if err := providers.StreamText(ctx, "Inspecting"); err != nil {
							return providers.Response{}, err
						}
					}
					return providers.Response{Text: "Inspecting the actual state.", Model: "test-model", Provider: "recovery-test", ToolCalls: []providers.ToolCall{{ID: "call-inspect", Name: "inspect_state", Arguments: []byte(`{}`)}}, Usage: providers.Usage{InputTokens: 10, OutputTokens: 2}}, nil
				}
				var commentary, result bool
				for _, message := range request.Messages {
					commentary = commentary || message.Content == "Inspecting the actual state."
					result = result || (message.ToolCallID == "call-inspect" && message.Content == "recovered tool result")
				}
				if !commentary || !result {
					t.Fatalf("next request lost commentary or result: %#v", request.Messages)
				}
				return providers.Response{Text: "Verified.", Model: "test-model", Provider: "recovery-test", Usage: providers.Usage{InputTokens: 20, OutputTokens: 3}}, nil
			})})
			session, run := saveCrashRecoveryRun(t, db, "tool-commentary", core.RunStatusAccepted, false)
			if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
				t.Fatal(err)
			}
			assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
			if calls != 2 || tool.callCount() != 1 {
				t.Fatalf("model calls=%d tool calls=%d", calls, tool.callCount())
			}
			messages, err := db.ListMessages(context.Background(), session.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, message := range messages {
				if message.Content == "Inspecting the actual state." {
					count++
				}
				if message.Content == "Inspecting" {
					t.Fatal("unfinished preview retained")
				}
			}
			if count != 1 {
				t.Fatalf("commentary messages=%d, want one", count)
			}
			usage, err := app.Usage(context.Background(), core.UsageFilter{RunID: run.ID})
			if err != nil {
				t.Fatal(err)
			}
			if len(usage.Records) != 1 || usage.Summary.InputTokens != 30 || usage.Summary.OutputTokens != 5 || usage.Summary.TotalTokens != 35 {
				t.Fatalf("usage did not include tool turn: %#v", usage)
			}
		})
	}
}

func TestEmptyModelReplyRetriesWithoutReexecutingTool(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		switch calls {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "only-once", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		case 2:
			return providers.Response{}, providers.ErrEmptyResponse
		default:
			return providers.Response{Text: "Recovered."}, nil
		}
	})})
	_, run := saveCrashRecoveryRun(t, db, "empty-retry", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if calls != 3 || tool.callCount() != 1 {
		t.Fatalf("model calls=%d tool calls=%d", calls, tool.callCount())
	}
}

func TestRepeatedToolCallIDExecutesOnlyOnce(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls <= 2 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "repeated", Name: "inspect_state", Arguments: []byte(`{}`)}, {ID: "repeated", Name: "inspect_state", Arguments: []byte(`{ }`)}}}, nil
		}
		return providers.Response{Text: "Done"}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "repeated", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || tool.callCount() != 1 {
		t.Fatalf("calls=%d tool executions=%d", calls, tool.callCount())
	}
	assertToolResultCount(t, db, run.SessionID, "repeated", 1)
}

func TestUnknownToolIsReturnedToModelForCorrection(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "bad-call", Name: "misspelled_tool", Arguments: []byte(`{}`)}}}, nil
		}
		if calls == 2 {
			found := false
			for _, message := range request.Messages {
				if message.ToolCallID == "bad-call" && strings.Contains(message.Content, "unknown tool") {
					found = true
				}
			}
			if !found {
				t.Fatal("model did not receive paired tool error")
			}
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "corrected-call", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Corrected and verified."}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "corrected", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if tool.callCount() != 1 {
		t.Fatalf("tool executions=%d", tool.callCount())
	}
}

type failingApprovedTool struct{ recoveryTool }

func (t *failingApprovedTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	_, _ = t.recoveryTool.Execute(ctx, call)
	return tools.Result{}, errors.New("target changed; inspect before retrying")
}

func TestApprovedToolFailureIsReturnedToModelWithoutReplay(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &failingApprovedTool{recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}}
	app.WithTools(tools.NewRegistry(tool))
	app.WithRunStarter(&recordingRunStarter{})
	modelCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		modelCalls++
		for _, message := range request.Messages {
			if message.Role == "tool" && message.ToolCallID == "approved-call" && strings.Contains(message.Content, "target changed") {
				return providers.Response{Text: "The approved operation failed because the target changed."}, nil
			}
		}
		t.Fatal("model did not receive the approved tool's error result")
		return providers.Response{}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "approved-failure", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, db, run, "approved-call", "mutate_state")
	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatal(err)
	}
	approvals, err := db.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 {
		t.Fatalf("pending approvals=%d", len(approvals))
	}
	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if tool.callCount() != 1 || modelCalls != 1 {
		t.Fatalf("tool calls=%d model calls=%d", tool.callCount(), modelCalls)
	}
	assertToolResultCount(t, db, run.SessionID, "approved-call", 1)
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
}

func TestModelFailuresAreBoundedAndDoNotReplayPartialOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		partial   string
		err       error
		wantCalls int
	}{
		{"empty", "", providers.ErrEmptyResponse, 3},
		{"permanent", "", errors.New("invalid API key"), 1},
		{"partial", "Unfinished answer", providers.ErrIncompleteResponse, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			calls := 0
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
				calls++
				if tc.partial != "" {
					if err := providers.StreamText(ctx, tc.partial); err != nil {
						return providers.Response{}, err
					}
				}
				return providers.Response{}, tc.err
			})})
			session, run := saveCrashRecoveryRun(t, db, tc.name, core.RunStatusAccepted, false)
			if err := app.ExecuteRun(context.Background(), run.ID); !errors.Is(err, tc.err) {
				t.Fatalf("error=%v, want %v", err, tc.err)
			}
			assertRecoveryRunStatus(t, db, run.ID, core.RunStatusFailed)
			if calls != tc.wantCalls {
				t.Fatalf("attempts=%d, want %d", calls, tc.wantCalls)
			}
			messages, err := db.ListMessages(context.Background(), session.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			assistantCount := 0
			for _, message := range messages {
				if message.Role == core.MessageRoleAssistant {
					assistantCount++
					if tc.partial != "" && !strings.Contains(message.Content, tc.partial) {
						t.Fatal("partial output was lost")
					}
				}
			}
			if assistantCount != 1 {
				t.Fatalf("assistant messages=%d, want one failure", assistantCount)
			}
		})
	}
}
