package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/orchestration"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// changingTool reports new output on every call, so the loop guard never fires.
func changingTool(id string) funcTool {
	var mu sync.Mutex
	calls := 0
	return funcTool{spec: recoveryToolSpec(id, tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return tools.Result{Content: fmt.Sprintf("state %d", calls)}, nil
	}}
}

func TestNativeRunEndsWithAFinalTurnAtTheDefaultStepBudget(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(changingTool("inspect_state")))
	var choices []providers.ToolChoice
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		choices = append(choices, request.ToolChoice)
		if request.ToolChoice == providers.ToolChoiceNone {
			return providers.Response{Text: "Inspected 32 times; /continue finishes the job."}, nil
		}
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("step-%d", len(choices)), Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "step-budget", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != core.RunStatusCompleted || stored.StopReason != agent.StopBudgetExhausted {
		t.Fatalf("run = %s (%s) stop reason %q", stored.Status, stored.Error, stored.StopReason)
	}
	if len(choices) != 33 || choices[31] != providers.ToolChoiceAuto || choices[32] != providers.ToolChoiceNone {
		t.Fatalf("model calls = %d, want 32 with tools and a final one without", len(choices))
	}
	notes := 0
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Origin == transcript.OriginEngine && message.Role == transcript.MessageRoleSystem {
			notes++
		}
	}
	if notes != 2 {
		t.Fatalf("engine notes = %d, want the wrap-up and the final-turn note", notes)
	}
}

func TestBudgetCountersSurviveAnApprovalPark(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunBudgets(core.RunBudgets{User: agent.Budget{Steps: 2}})
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, changingTool("inspect_state")))
	app.WithRunStarter(&recordingRunStarter{})
	var requests []providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		switch len(requests) {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		case 2:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-inspect", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		default:
			return providers.Response{Text: "Mutated and inspected; /continue verifies."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "budget-park", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	checkpoint, err := db.GetRunCheckpoint(context.Background(), run.ID)
	if err != nil || !strings.Contains(string(checkpoint.EngineState), `"steps":1`) {
		t.Fatalf("parked checkpoint = %+v err = %v, want the step counter", checkpoint, err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("pending approvals = %+v err = %v", approvals, err)
	}
	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != core.RunStatusCompleted || stored.StopReason != agent.StopBudgetExhausted || mutations != 1 {
		t.Fatalf("run = %s stop reason %q mutations %d", stored.Status, stored.StopReason, mutations)
	}
	if len(requests) != 3 || requests[1].ToolChoice != providers.ToolChoiceAuto || requests[2].ToolChoice != providers.ToolChoiceNone {
		t.Fatalf("requests = %d, want the resumed run to spend its one remaining step before the final turn", len(requests))
	}
}

func TestRecoveredRunKeepsItsBudgetCounters(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunBudgets(core.RunBudgets{User: agent.Budget{Steps: 2}})
	runtime := &recoveryRuntime{text: "Stopped after the restart."}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	app.WithRunStarter(orchestration.NewStub(app))
	_, run := saveCrashRecoveryRun(t, db, "budget-recovery", core.RunStatusRunning, false)
	if err := db.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{
		RunID: run.ID, Phase: core.RunCheckpointPhaseModel, EngineState: json.RawMessage(`{"steps":2}`), UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	waitForRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)

	if runtime.requestCount() != 1 || runtime.lastRequest().ToolChoice != providers.ToolChoiceNone {
		t.Fatalf("requests = %d, last tool choice %q, want only the final turn", runtime.requestCount(), runtime.lastRequest().ToolChoice)
	}
	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil || stored.StopReason != agent.StopBudgetExhausted {
		t.Fatalf("run = %+v err = %v", stored, err)
	}
}

func TestRunBudgetFollowsTheTrigger(t *testing.T) {
	budgets := core.RunBudgets{User: agent.Budget{Steps: 3}, Subagent: agent.Budget{Steps: 2}, Automation: agent.Budget{Steps: 1}}
	for _, tc := range []struct {
		name string
		want int
	}{
		{"user", 3},
		{"subagent", 2},
		{"automation", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			app.WithRunBudgets(budgets)
			app.WithRunStarter(&recordingRunStarter{})
			app.WithTools(tools.NewRegistry(changingTool("inspect_state")))
			withTools := 0
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
				if request.ToolChoice == providers.ToolChoiceNone {
					return providers.Response{Text: "Out of budget."}, nil
				}
				withTools++
				return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("call-%d", withTools), Name: "inspect_state", Arguments: []byte(fmt.Sprintf(`{"n":%d}`, withTools))}}}, nil
			})})
			status := core.RunStatusAccepted
			if tc.name == "automation" {
				status = core.RunStatusCompleted
			}
			session, run := saveCrashRecoveryRun(t, db, "trigger-"+tc.name, status, tc.name == "subagent")
			runID := run.ID
			if tc.name == "automation" {
				result, err := app.AcceptTriggeredRun(context.Background(), core.HandleTriggeredRunInput{TriggerID: "job-budget", SessionID: session.ID, Text: "scheduled check"})
				if err != nil {
					t.Fatal(err)
				}
				runID = result.Run.ID
			}

			if err := app.ExecuteRun(context.Background(), runID); err != nil {
				t.Fatal(err)
			}

			assertRecoveryRunStatus(t, db, runID, core.RunStatusCompleted)
			if withTools != tc.want {
				t.Fatalf("model calls with tools = %d, want %d", withTools, tc.want)
			}
		})
	}
}
