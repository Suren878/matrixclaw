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
	"github.com/Suren878/matrixclaw/internal/transcript"
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
			return tools.Result{Content: "the other child never ran alongside", Status: tools.ResultStatusError}, nil
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
		task, err := taskOfCall(db, "session_readonly", run.ID, id)
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
	if err != nil || !first.Task.Background || !strings.HasPrefix(first.Task.ID, "task_") {
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

func TestBackgroundChildrenStartedTogetherKeepTheLimit(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithBackgroundAgents(2).WithRunStarter(&recordingRunStarter{})
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if results = toolResults(request); len(results) > 0 {
			return providers.Response{Text: "Started."}, nil
		}
		var spawn []providers.ToolCall
		for _, id := range []string{"call-1", "call-2", "call-3", "call-4", "call-5"} {
			spawn = append(spawn, providers.ToolCall{ID: id, Name: "agent", Arguments: json.RawMessage(`{"description":"Scan","prompt":"scan the tree","background":true}`)})
		}
		return providers.Response{ToolCalls: spawn}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "limit_together", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	tasks, err := db.ListTasks(context.Background(), core.TaskFilter{SessionID: session.ID, Kind: core.TaskKindSubagent})
	if err != nil || len(tasks) != 2 {
		t.Fatalf("started %d children, want 2 (%v)", len(tasks), err)
	}
	refused := 0
	for _, result := range results {
		if strings.Contains(result, "at most 2 background subagents run at once") {
			refused++
		}
	}
	if refused != 3 {
		t.Fatalf("results = %q", results)
	}
}

func TestCancelingARunStopsTheCommandsItStarted(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	now := runRecoveryTestTime()
	for _, id := range []string{"run_one", "run_two"} {
		user := transcript.Message{ID: "msg_" + id, SessionID: session.ID, RunID: id, Role: transcript.MessageRoleUser, CreatedAt: now, UpdatedAt: now}
		if err := db.AcceptMessage(context.Background(), user, core.Run{ID: id, SessionID: session.ID, UserMessageID: user.ID, Status: core.RunStatusRunning, StartedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	start := func(runID string) string {
		started, err := app.RunCommand(context.Background(), tools.Call{SessionID: session.ID, RunID: runID}, tools.Command{Command: "sleep 30", Background: true})
		if err != nil {
			t.Fatal(err)
		}
		return started.TaskID
	}
	mine, other := start("run_one"), start("run_two")
	t.Cleanup(func() { _, _ = app.CancelTask(context.Background(), other) })

	if _, err := app.CancelRun(context.Background(), "run_one"); err != nil {
		t.Fatal(err)
	}

	stopped := waitTaskStatus(t, db, mine, core.TaskStatusCanceled)
	if stopped.Error != "its run was canceled" {
		t.Fatalf("stopped = %+v", stopped)
	}
	waitProcessGone(t, stopped.PID)
	if running, err := db.GetTask(context.Background(), other); err != nil || running.Status != core.TaskStatusRunning {
		t.Fatalf("another run's task = %+v, %v", running, err)
	}
}

func TestAFinishedChildsBackgroundCommandsStop(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionFiles(t.TempDir())
	registry := tools.NewRegistry(core.AgentToolExecutors(app)...)
	if err := registry.Register(tools.NewShellExecutors(app)...); err != nil {
		t.Fatal(err)
	}
	app.WithTools(registry)
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		answered := len(toolResults(request)) > 0
		switch {
		case childPrompt(request) != "" && !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-serve", Name: "bash", Arguments: json.RawMessage(`{"command":"sleep 30","run_in_background":true}`)}}}, nil
		case childPrompt(request) != "":
			return providers.Response{Text: "started the server"}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child", Name: "agent", Arguments: json.RawMessage(`{"description":"Serve","prompt":"start the server"}`)}}}, nil
		default:
			return providers.Response{Text: "Done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "child_bash", core.RunStatusAccepted, false)
	sessionIn(t, db, session, t.TempDir(), core.PermissionModeFullAuto)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	child, err := taskOfCall(db, session.ID, run.ID, "call-child")
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := db.ListTasks(context.Background(), core.TaskFilter{SessionID: child.ChildSessionID, Kind: core.TaskKindShell})
	if err != nil || len(tasks) != 1 || tasks[0].Status != core.TaskStatusCanceled || tasks[0].Error != "its subagent finished" {
		t.Fatalf("child tasks = %+v, %v", tasks, err)
	}
	waitProcessGone(t, tasks[0].PID)
}

func TestParentPromptExplainsTheAgentTool(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithBackgroundAgents(2)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	var system string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		system = request.SystemPrompt
		return providers.Response{Text: "Hi."}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "guidance", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"The agent tool runs a child agent", "readonly:true", "isolation worktree", "At most 2 background children", "Runtime IDs available for the agent tool: matrixclaw."} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt lacks %q:\n%s", want, system)
		}
	}
}

func TestReadonlyChildsApprovalIsRefusedWithoutAskingTheParent(t *testing.T) {
	t.Parallel()
	for _, background := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocking", true: "background"}[background], func(t *testing.T) {
			t.Parallel()
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			starter := &executingRunStarter{app: app}
			app.WithRunStarter(starter)
			app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), askingReadTool("ask_read"))...))
			var mu sync.Mutex
			var childSaw string
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
				results := toolResults(request)
				switch {
				case childPrompt(request) != "" && len(results) == 0:
					return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-read", Name: "ask_read", Arguments: json.RawMessage(`{}`)}}}, nil
				case childPrompt(request) != "":
					mu.Lock()
					childSaw = results[0]
					mu.Unlock()
					return providers.Response{Text: "read nothing"}, nil
				case len(results) == 0:
					args, _ := json.Marshal(map[string]any{"description": "Look", "prompt": "look around", "readonly": true, "background": background})
					return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-look", Name: "agent", Arguments: args}}}, nil
				default:
					return providers.Response{Text: "Done."}, nil
				}
			})})
			session, run := saveCrashRecoveryRun(t, db, "readonly_ask", core.RunStatusAccepted, false)

			if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
				t.Fatal(err)
			}
			task, err := taskOfCall(db, session.ID, run.ID, "call-look")
			if err != nil {
				t.Fatal(err)
			}
			waitRunStatus(t, db, task.ChildRunID, core.RunStatusCompleted)
			starter.wait(t)

			mu.Lock()
			defer mu.Unlock()
			if childSaw != "child-read=User denied: read-only subagent cannot run ask_read" {
				t.Fatalf("the child read %q", childSaw)
			}
			approvals, err := db.ListApprovals(context.Background(), session.ID, "")
			if err != nil || len(approvals) != 1 || approvals[0].State != core.ApprovalStateRejected || approvals[0].DecidedAt == nil || approvals[0].TaskID != task.ID {
				t.Fatalf("parent approvals = %+v, %v; want the child's, refused", approvals, err)
			}
			if deliveries, err := db.ListClientDeliveries(context.Background(), core.ClientDeliveryFilter{Type: core.ClientDeliveryTypeApproval}); err != nil || len(deliveries) != 0 {
				t.Fatalf("approval deliveries = %+v, %v", deliveries, err)
			}
		})
	}
}

func TestAChildsLabelIsOneLine(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	session, run := saveCrashRecoveryRun(t, db, "labels", core.RunStatusRunning, false)
	for _, tc := range []struct{ description, prompt, want string }{
		{"  Scan\n\tthe   tree ", "scan it", "Scan the tree"},
		{"", "\n  Count the files.\nThen report the biggest one.", "Count the files."},
	} {
		result, err := app.RunAgent(context.Background(), core.AgentInput{ParentSessionID: session.ID, ParentRunID: run.ID, ParentToolCallID: "call-" + tc.want, Description: tc.description, Prompt: tc.prompt, Background: true})
		if err != nil || result.Task.Description != tc.want {
			t.Fatalf("label = %q, %v; want %q", result.Task.Description, err, tc.want)
		}
	}
}
