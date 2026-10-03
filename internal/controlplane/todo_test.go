package controlplane

import (
	"net/http"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
)

func todoDaemon(t *testing.T, list todo.List) (*fakeDaemon, *int) {
	cleared := 0
	daemon := newFakeDaemon(t).
		on("GET /v1/sessions/{id}/todo", func(*http.Request) any { return core.SessionTodoResponse{Todo: list} }).
		on("DELETE /v1/sessions/{id}/todo", func(r *http.Request) any {
			cleared++
			list = todo.List{SessionID: r.PathValue("id")}
			return core.SessionTodoResponse{Todo: list}
		})
	return daemon, &cleared
}

func TestTodoCommandShowsTheList(t *testing.T) {
	daemon, _ := todoDaemon(t, todo.List{SessionID: "s1", Items: []todo.Item{
		{Content: "Fix the bug", Status: todo.Completed},
		{Content: "Run the tests", Status: todo.Pending},
	}})

	result := daemon.run("/todo")

	want := "1 of 2 done\n\n1. [completed] Fix the bug\n2. [pending] Run the tests\n\n" + todoUsage
	if result.Info == nil || result.Info.Text != want || len(result.Info.Rows) != 2 {
		t.Fatalf("info = %+v", result.Info)
	}
}

func TestTodoCommandClearsAfterConfirmation(t *testing.T) {
	daemon, cleared := todoDaemon(t, todo.List{SessionID: "s1", Items: []todo.Item{{Content: "Run the tests", Status: todo.Pending}}})

	asked := daemon.run("/todo clear")
	if asked.Confirm == nil || asked.Confirm.ConfirmCommand != "/todo clear confirm" || *cleared != 0 {
		t.Fatalf("clear = %+v", asked)
	}
	if confirmed := daemon.run(asked.Confirm.ConfirmCommand); confirmed.Text != "Todo list cleared." || !confirmed.ReloadSnapshot || *cleared != 1 {
		t.Fatalf("confirm = %+v", confirmed)
	}
}

func TestTodoCommandIsForMatrixclawSessionsOnly(t *testing.T) {
	daemon, _ := todoDaemon(t, todo.List{})
	daemon.sessions = []core.Session{{ID: "s1", Title: "s1", Kind: core.SessionKindExternalAgent}}

	if result := daemon.run("/todo"); result.Text != "Todo lists are kept by Matrixclaw sessions only." {
		t.Fatalf("result = %+v", result)
	}
}
