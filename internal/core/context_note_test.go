package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type runtimeStatusFunc func() string

func (f runtimeStatusFunc) RuntimeStatusPromptContext(context.Context, core.RuntimeStatusContextRequest) string {
	return f()
}

// twoStepRun executes a run whose one tool call runs change and records the
// run's two requests.
func twoStepRun(t *testing.T, app *core.Core, db *store.SQLiteStore, suffix string, change func(ctx context.Context, sessionID string) error) []providers.Request {
	t.Helper()
	ctx := context.Background()
	session, run := saveCrashRecoveryRun(t, db, suffix, core.RunStatusAccepted, false)
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("change", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		if err := change(ctx, session.ID); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
		return tools.Result{Content: "changed"}, nil
	}}))
	var requests []providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		if len(requests) == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call_change", Name: "change", Arguments: json.RawMessage(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})

	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[0].SystemPrompt != requests[1].SystemPrompt {
		t.Fatalf("system prompt changed during the run:\n%s\n---\n%s", requests[0].SystemPrompt, requests[1].SystemPrompt)
	}
	return requests
}

func TestMemoryWrittenDuringARunReachesTheModelAsAContextNote(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	if _, err := app.CreateMemory(context.Background(), core.MemoryEntry{Scope: core.MemoryScopeGlobal, Content: "prefers tabs"}); err != nil {
		t.Fatal(err)
	}

	requests := twoStepRun(t, app, db, "memory", func(ctx context.Context, _ string) error {
		_, err := app.CreateMemory(ctx, core.MemoryEntry{Scope: core.MemoryScopeGlobal, Content: "uses Go 1.26"})
		return err
	})

	if !strings.Contains(requests[0].SystemPrompt, "prefers tabs") || strings.Contains(requests[1].SystemPrompt, "uses Go 1.26") {
		t.Fatalf("system prompt = %q", requests[1].SystemPrompt)
	}
	last := requests[1].Messages[len(requests[1].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Memory changed during this run") || !strings.Contains(last.Content, "uses Go 1.26") {
		t.Fatalf("second request ends with %+v", last)
	}
}

func TestTodoAndRuntimeStatusChangesLeaveTheSystemPromptIntact(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	status := "Current runtime status: browser=off"
	app.WithRuntimeStatusContext(runtimeStatusFunc(func() string { return status }))

	requests := twoStepRun(t, app, db, "todo", func(ctx context.Context, sessionID string) error {
		status = "Current runtime status: browser=on"
		return db.SaveSessionTodo(ctx, todo.List{SessionID: sessionID, Items: []todo.Item{{Content: "write the parser", Status: todo.InProgress}}})
	})

	for _, text := range []string{"browser=", "write the parser"} {
		if strings.Contains(requests[0].SystemPrompt, text) {
			t.Fatalf("system prompt carries %q: %q", text, requests[0].SystemPrompt)
		}
	}
	first := requests[0].Messages[len(requests[0].Messages)-1]
	last := requests[1].Messages[len(requests[1].Messages)-1]
	if !strings.Contains(first.Content, "browser=off") || !strings.Contains(last.Content, "browser=on") || !strings.Contains(last.Content, "write the parser") {
		t.Fatalf("context notes: first %q, last %q", first.Content, last.Content)
	}
}

func TestRuntimeStatusDescribesTheToolsTheRunWasGiven(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var seen [][]string
	app.WithRuntimeStatusContext(runtimeStatusRecorder(func(req core.RuntimeStatusContextRequest) { seen = append(seen, req.ToolIDs) }))

	twoStepRun(t, app, db, "frozen_tools", func(context.Context, string) error {
		app.WithTools(tools.NewRegistry(
			funcTool{spec: recoveryToolSpec("change", tools.EffectReadOnly)},
			funcTool{spec: recoveryToolSpec("added", tools.EffectReadOnly)},
		))
		return nil
	})

	if len(seen) < 2 {
		t.Fatalf("runtime status built %d times", len(seen))
	}
	for _, ids := range seen {
		if strings.Join(ids, ",") != "change" {
			t.Fatalf("runtime status tool lists = %v, want the run's own tools each time", seen)
		}
	}
}

type runtimeStatusRecorder func(core.RuntimeStatusContextRequest)

func (f runtimeStatusRecorder) RuntimeStatusPromptContext(_ context.Context, req core.RuntimeStatusContextRequest) string {
	f(req)
	return "status"
}

func TestSubagentContextNoteCarriesItsOwnTodo(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(core.TodoToolExecutors(app)...))
	var requests []providers.Request
	app.WithSessionLLMs(scriptedLLMs(&requests,
		todoWriteCall("call_child", `[{"content":"Scan the repo","status":"completed"}]`),
		providers.Response{Text: "Scanned."},
	))
	_, run := saveCrashRecoveryRun(t, db, "todo_child_note", core.RunStatusAccepted, true)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(requests[0].SystemPrompt, "todo_write") {
		t.Fatalf("subagent system prompt = %q", requests[0].SystemPrompt)
	}
	last := requests[1].Messages[len(requests[1].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Todo list") || !strings.Contains(last.Content, "1. [completed] Scan the repo") {
		t.Fatalf("second request ends with %+v", last)
	}
}
