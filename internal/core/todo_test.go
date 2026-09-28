package core_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// scriptedLLMs answers every request with the next response and records the requests.
func scriptedLLMs(requests *[]providers.Request, responses ...providers.Response) recoveryLLMs {
	return recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		*requests = append(*requests, request)
		if len(responses) == 0 {
			return providers.Response{Text: "Done."}, nil
		}
		next := responses[0]
		responses = responses[1:]
		return next, nil
	})}
}

func todoWriteCall(id string, items string) providers.Response {
	return providers.Response{ToolCalls: []providers.ToolCall{{ID: id, Name: todo.ToolName, Arguments: json.RawMessage(`{"items":` + items + `}`)}}}
}

func TestTodoWriteKeepsTheListOfTheRunChain(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx := context.Background()
	app.WithRunStarter(&recordingRunStarter{})
	app.WithTools(tools.NewRegistry(core.TodoToolExecutors(app)...))
	var requests []providers.Request
	app.WithSessionLLMs(scriptedLLMs(&requests,
		todoWriteCall("call_first", `[{"content":"Read the parser","status":"completed"}]`),
		providers.Response{Text: "Read it."},
		todoWriteCall("call_second", `[{"content":"Read the parser","status":"completed"},{"content":"Fix the bug","active_form":"Fixing the bug","status":"completed"}]`),
		providers.Response{Text: "Fixed it."},
	))
	session, first := saveCrashRecoveryRun(t, db, "todo_chain", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	list, err := app.SessionTodo(ctx, session.ID)
	if err != nil || list.ChainRunID != first.ID || list.UpdatedRunID != first.ID || len(list.Items) != 1 {
		t.Fatalf("todo after the first run = %+v err = %v", list, err)
	}
	continued, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: session.ID, Continue: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(ctx, continued.Run.ID); err != nil {
		t.Fatal(err)
	}

	list, err = app.SessionTodo(ctx, session.ID)
	want := []todo.Item{{Content: "Read the parser", Status: todo.Completed}, {Content: "Fix the bug", ActiveForm: "Fixing the bug", Status: todo.Completed}}
	if err != nil || list.ChainRunID != first.ID || list.UpdatedRunID != continued.Run.ID || !reflect.DeepEqual(list.Items, want) {
		t.Fatalf("todo after /continue = %+v err = %v", list, err)
	}
}

func TestTodoWriteReturnsWhatToCorrectToTheModel(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx := context.Background()
	app.WithTools(tools.NewRegistry(core.TodoToolExecutors(app)...))
	var requests []providers.Request
	app.WithSessionLLMs(scriptedLLMs(&requests,
		todoWriteCall("call_bad", `[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]`),
		providers.Response{Text: "Done."},
	))
	session, run := saveCrashRecoveryRun(t, db, "todo_invalid", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	var result providers.Message
	for _, message := range requests[1].Messages {
		if message.ToolCallID == "call_bad" {
			result = message
		}
	}
	if !result.IsError || !strings.Contains(result.Content, "2 items are in_progress") {
		t.Fatalf("tool result = %+v", result)
	}
	if list, err := app.SessionTodo(ctx, session.ID); err != nil || len(list.Items) != 0 || list.UpdatedRunID != "" {
		t.Fatalf("todo = %+v err = %v", list, err)
	}
}

func TestSubagentsGetTheirOwnTodoList(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx := context.Background()
	app.WithTools(tools.NewRegistry(append(core.TodoToolExecutors(app), core.MemoryToolExecutors(app)...)...))
	var requests []providers.Request
	app.WithSessionLLMs(scriptedLLMs(&requests,
		todoWriteCall("call_child", `[{"content":"Scan the repo","status":"completed"}]`),
		providers.Response{Text: "Scanned."},
	))
	child, run := saveCrashRecoveryRun(t, db, "todo_child", core.RunStatusAccepted, true)

	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tool := range requests[0].Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != todo.ToolName {
		t.Fatalf("subagent tools = %v, want only %s", names, todo.ToolName)
	}
	if list, err := app.SessionTodo(ctx, child.ID); err != nil || len(list.Items) != 1 || list.UpdatedRunID != run.ID {
		t.Fatalf("child todo = %+v err = %v", list, err)
	}
}

func TestClearingTheTodoListTellsClients(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, _ := saveCrashRecoveryRun(t, db, "todo_clear", core.RunStatusCompleted, false)
	if err := db.SaveSessionTodo(ctx, todo.List{SessionID: session.ID, Items: []todo.Item{{Content: "a", Status: todo.Pending}}}); err != nil {
		t.Fatal(err)
	}
	events := app.SubscribeEvents(ctx, session.ID)

	list, err := app.ClearSessionTodo(ctx, session.ID)

	if err != nil || len(list.Items) != 0 {
		t.Fatalf("cleared = %+v err = %v", list, err)
	}
	select {
	case event := <-events:
		payload, ok := event.Payload.(todo.List)
		if event.Type != core.EventTodoUpdated || !ok || len(payload.Items) != 0 {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("no todo.updated event")
	}
}
func TestTodoWritesOfOneSessionShareAConcurrencyKey(t *testing.T) {
	registry := tools.NewRegistry(core.TodoToolExecutors(core.New(nil))...)
	first := registry.ConcurrencyKey(todo.ToolName, tools.Call{SessionID: "s1"})
	if first == "" || first != registry.ConcurrencyKey(todo.ToolName, tools.Call{SessionID: "s1"}) || first == registry.ConcurrencyKey(todo.ToolName, tools.Call{SessionID: "s2"}) {
		t.Fatalf("todo_write keys = %q", first)
	}
}
