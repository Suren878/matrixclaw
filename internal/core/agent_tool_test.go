package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// childPrompt is the delegated prompt of a child's request.
func childPrompt(request providers.Request) string {
	for _, message := range request.Messages {
		if message.Role == "user" && strings.HasPrefix(message.Content, "Delegated task:") {
			return message.Content
		}
	}
	return ""
}

func TestReadonlyChildrenRunTogetherWithReadOnlyTools(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var started sync.WaitGroup
	started.Add(2)
	both := make(chan struct{})
	go func() { started.Wait(); close(both) }()
	probe := funcTool{spec: recoveryToolSpec("probe", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		started.Done()
		select {
		case <-both:
			return tools.Result{Content: "probed"}, nil
		case <-time.After(5 * time.Second):
			return tools.Result{Content: "the other child never ran alongside", IsError: true}, nil
		}
	}}
	edit := funcTool{spec: recoveryToolSpec("edit_file", tools.EffectMutation), fn: func(context.Context, tools.Call) (tools.Result, error) {
		return tools.Result{Content: "edited"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), probe, edit)...))
	var mu sync.Mutex
	var childTools [][]string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		prompt := childPrompt(request)
		answered := len(toolResults(request)) > 0
		switch {
		case prompt != "" && !answered:
			mu.Lock()
			var names []string
			for _, tool := range request.Tools {
				names = append(names, tool.Name)
			}
			childTools = append(childTools, names)
			mu.Unlock()
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "probe-" + strings.Fields(prompt)[3], Name: "probe", Arguments: json.RawMessage(`{}`)}}}, nil
		case prompt != "" && strings.HasSuffix(toolResults(request)[0], "=probed"):
			return providers.Response{Text: "looked at " + strings.Fields(prompt)[3]}, nil
		case prompt != "":
			return providers.Response{Text: "ran alone"}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"Review A","prompt":"review alpha","readonly":true}`)},
				{ID: "call-b", Name: "agent", Arguments: json.RawMessage(`{"description":"Review B","prompt":"review beta","readonly":true}`)},
			}}, nil
		default:
			return providers.Response{Text: "Both reviewed."}, nil
		}
	})})
	_, run := saveCrashRecoveryRun(t, db, "readonly", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if len(childTools) != 2 {
		t.Fatalf("child tool lists = %v", childTools)
	}
	for _, names := range childTools {
		if !slices.Contains(names, "probe") || slices.Contains(names, "edit_file") || slices.Contains(names, "agent") {
			t.Fatalf("a read-only child got %v", names)
		}
	}
	names := map[string]bool{}
	for _, id := range []string{"call-a", "call-b"} {
		task, err := db.GetSubagentTaskByParentToolCall(context.Background(), "session_readonly", run.ID, id)
		if err != nil || !task.Readonly || task.Status != core.TaskStatusCompleted || !strings.HasPrefix(task.Summary, "looked at") {
			t.Fatalf("task %s = %+v, %v", id, task, err)
		}
		names[task.AgentName] = true
	}
	if len(names) != 2 {
		t.Fatalf("children share a name: %v", names)
	}
}

func TestReadonlyChildCannotUseAMutatingTool(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	edit := funcTool{spec: recoveryToolSpec("edit_file", tools.EffectMutation), fn: func(context.Context, tools.Call) (tools.Result, error) {
		return tools.Result{Content: "edited"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), edit)...))
	var rejected string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results := toolResults(request)
		switch {
		case childPrompt(request) != "" && len(results) == 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-edit", Name: "edit_file", Arguments: json.RawMessage(`{}`)}}}, nil
		case childPrompt(request) != "":
			rejected = results[0]
			return providers.Response{Text: "could not edit"}, nil
		case len(results) == 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"Look","prompt":"look around","readonly":true}`)}}}, nil
		default:
			return providers.Response{Text: "Done."}, nil
		}
	})})
	_, run := saveCrashRecoveryRun(t, db, "readonly_edit", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(rejected, "this subagent is read-only") {
		t.Fatalf("the child's edit result = %q", rejected)
	}
}

func TestBackgroundChildrenAreLimitedPerSession(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithBackgroundAgents(1).WithRunStarter(&recordingRunStarter{})
	session, run := saveCrashRecoveryRun(t, db, "limit", core.RunStatusRunning, false)
	start := func(callID string) (core.AgentResult, error) {
		return app.RunAgent(context.Background(), core.AgentInput{ParentSessionID: session.ID, ParentRunID: run.ID, ParentToolCallID: callID, Description: "Scan", Prompt: "scan the tree", Background: true})
	}

	first, err := start("call-1")
	if err != nil || first.Task.Mode != core.SubagentTaskModeAsync || !strings.HasPrefix(first.Task.ID, "task_") {
		t.Fatalf("first = %+v, %v", first, err)
	}
	again, err := start("call-1")
	if err != nil || !again.Replayed || again.Task.ID != first.Task.ID {
		t.Fatalf("repeated call = %+v, %v", again, err)
	}
	if _, err := start("call-2"); !errors.Is(err, core.ErrInvalidInput) || !strings.Contains(err.Error(), "at most 1 background subagents") {
		t.Fatalf("second = %v", err)
	}
}
