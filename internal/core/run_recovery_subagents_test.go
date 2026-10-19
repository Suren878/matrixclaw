package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate(t *testing.T) {
	t.Parallel()
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "recovered completion"}
	app.SetSessionLLMs(recoveryLLMs{runtime: runtime})
	app.SetTools(tools.NewRegistry(core.AgentToolExecutors(app)...))

	parentSession, parentRun := saveCrashRecoveryRun(t, sqliteStore, "parent", core.RunStatusRunning, false)
	childSession, childRun := saveCrashRecoveryRun(t, sqliteStore, "child", core.RunStatusRunning, true)
	parentRun.StartedAt = childRun.StartedAt.Add(-time.Minute)
	parentRun.UpdatedAt = parentRun.StartedAt
	if err := sqliteStore.SealRun(context.Background(), parentRun, nil, false); err != nil {
		t.Fatal(err)
	}
	delegateArgs := `{"description":"Finish","prompt":"finish child work","runtime":"matrixclaw"}`
	saveInterruptedToolCallWithInput(t, sqliteStore, parentRun, "tool_delegate", "agent", delegateArgs)
	now := runRecoveryTestTime().Add(3 * time.Second)
	if err := sqliteStore.CreateTask(context.Background(), core.Task{
		ID: "subagent_recovery", Kind: core.TaskKindSubagent, AgentName: "Neo", Description: "Recovery child",
		Isolation: core.SubagentIsolationShared,
		SessionID: parentSession.ID, RunID: parentRun.ID, ParentToolCallID: "tool_delegate",
		ChildSessionID: childSession.ID, ChildRunID: childRun.ID, Runtime: "matrixclaw",
		Command: "finish child work", Status: core.TaskStatusRunning,
		StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, childRun.ID, core.RunStatusCompleted)
	waitForRecoveryRunStatus(t, sqliteStore, parentRun.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, parentSession.ID, "tool_delegate", 1)
	task, err := sqliteStore.GetTask(context.Background(), "subagent_recovery")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.TaskStatusCompleted {
		t.Fatalf("subagent task status = %q, want completed", task.Status)
	}
}

func TestRestartedParentWaitsForAllItsBlockingChildren(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	app, starter := executingCore(core.Config{Store: sqliteStore})
	app.SetSessionLLMs(recoveryLLMs{runtime: &recoveryRuntime{text: "child done"}})
	app.SetTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	parentSession, parentRun := saveCrashRecoveryRun(t, sqliteStore, "parent", core.RunStatusRunning, false)
	var childRuns []core.Run
	for _, name := range []string{"a", "b"} {
		childSession, childRun := saveCrashRecoveryRun(t, sqliteStore, "child_"+name, core.RunStatusAccepted, true)
		saveInterruptedToolCallWithInput(t, sqliteStore, parentRun, "tool_"+name, "agent", `{"description":"Part `+name+`","prompt":"do `+name+`","runtime":"matrixclaw"}`)
		now := runRecoveryTestTime().Add(3 * time.Second)
		if err := sqliteStore.CreateTask(context.Background(), core.Task{
			ID: "subagent_" + name, Kind: core.TaskKindSubagent, AgentName: "Neo", Description: "Child " + name,
			Isolation: core.SubagentIsolationShared,
			SessionID: parentSession.ID, RunID: parentRun.ID, ParentToolCallID: "tool_" + name,
			ChildSessionID: childSession.ID, ChildRunID: childRun.ID, Runtime: "matrixclaw",
			Command: "do " + name, Status: core.TaskStatusRunning, StartedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		childRuns = append(childRuns, childRun)
	}

	if err := app.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, parentRun.ID, core.RunStatusCompleted)
	starter.wait(t)

	for i, name := range []string{"a", "b"} {
		assertToolResultCount(t, sqliteStore, parentSession.ID, "tool_"+name, 1)
		if got := storedToolResult(t, sqliteStore, parentSession.ID, "tool_"+name); got != "child done" {
			t.Fatalf("tool_%s result = %q", name, got)
		}
		assertRecoveryRunStatus(t, sqliteStore, childRuns[i].ID, core.RunStatusCompleted)
		if got := starter.count(childRuns[i].ID); got != 1 {
			t.Fatalf("child %s starts = %d, want 1", name, got)
		}
	}
}
