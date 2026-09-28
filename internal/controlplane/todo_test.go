package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
)

type todoRuntime struct {
	tokenReportRuntime
	list    todo.List
	cleared int
}

func (r *todoRuntime) SessionTodo(context.Context, string) (todo.List, error) {
	return r.list, nil
}

func (r *todoRuntime) ClearSessionTodo(_ context.Context, sessionID string) (todo.List, error) {
	r.cleared++
	r.list = todo.List{SessionID: sessionID}
	return r.list, nil
}

type externalTodoRuntime struct {
	todoRuntime
}

func (r *externalTodoRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return []core.Session{{ID: "s1", Title: "s1", Kind: core.SessionKindExternalAgent}}, nil
}

func TestTodoCommandShowsTheList(t *testing.T) {
	runtime := &todoRuntime{list: todo.List{SessionID: "s1", Items: []todo.Item{
		{Content: "Fix the bug", Status: todo.Completed},
		{Content: "Run the tests", Status: todo.Pending},
	}}}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/todo")

	want := "1 of 2 done\n\n1. [completed] Fix the bug\n2. [pending] Run the tests\n\n" + todoUsage
	if err != nil || result.Info == nil || result.Info.Text != want || len(result.Info.Rows) != 2 {
		t.Fatalf("info = %+v err = %v", result.Info, err)
	}
}

func TestTodoCommandClearsAfterConfirmation(t *testing.T) {
	runtime := &todoRuntime{list: todo.List{SessionID: "s1", Items: []todo.Item{{Content: "Run the tests", Status: todo.Pending}}}}
	dispatcher := New(runtime, "")

	asked, err := dispatcher.Handle(context.Background(), "key", "/todo clear")
	if err != nil || asked.Confirm == nil || asked.Confirm.ConfirmCommand != "/todo clear confirm" || runtime.cleared != 0 {
		t.Fatalf("clear = %+v err = %v", asked, err)
	}
	cleared, err := dispatcher.Handle(context.Background(), "key", asked.Confirm.ConfirmCommand)

	if err != nil || cleared.Text != "Todo list cleared." || !cleared.ReloadSnapshot || runtime.cleared != 1 {
		t.Fatalf("confirm = %+v err = %v", cleared, err)
	}
}

func TestTodoCommandIsForMatrixclawSessionsOnly(t *testing.T) {
	runtime := &externalTodoRuntime{}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/todo")

	if err != nil || result.Text != "Todo lists are kept by Matrixclaw sessions only." {
		t.Fatalf("result = %+v err = %v", result, err)
	}
}
