package controlplane

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
)

const todoUsage = "Usage: /todo, /todo clear"

var (
	todoRoot         = verb(CommandTodo, "", 0)
	todoClear        = verb(CommandTodo, "clear", 0)
	todoClearConfirm = verb(CommandTodo, "clear confirm", 0)
)

func (d *Dispatcher) handleTodo(ctx context.Context, r Route) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return textResult("Select or create a session first."), nil
	}
	if !core.CapabilitiesForSession(*session).NativeTools {
		return textResult("Todo lists are kept by Matrixclaw sessions only."), nil
	}
	switch {
	case r.Value != "":
		return textResult(todoUsage), nil
	case r.Is(todoRoot):
		list, err := d.daemon.SessionTodo(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Screen: todoInfoData(list)}, nil
	case r.Is(todoClear):
		return Result{Screen: ConfirmData{
			Message:        "Clear the todo list?",
			ConfirmLabel:   "Clear",
			CancelLabel:    "Close",
			ConfirmCommand: todoClearConfirm.command(),
			CancelCommand:  todoRoot.command(),
		}}, nil
	default:
		if _, err := d.daemon.ClearSessionTodo(ctx, session.ID); err != nil {
			return Result{}, err
		}
		return Result{Screen: TextData{Text: "Todo list cleared."}, ReloadSnapshot: true}, nil
	}
}

func todoInfoData(list todo.List) InfoData {
	if len(list.Items) == 0 {
		return InfoData{Title: "Todo", Text: "The todo list is empty."}
	}
	rows := make([]InfoRow, 0, len(list.Items))
	for i, item := range list.Items {
		rows = append(rows, InfoRow{Label: fmt.Sprintf("%d. %s", i+1, item.Status), Value: item.Content})
	}
	text := fmt.Sprintf("%d of %d done\n\n%s\n\n%s", len(list.Items)-len(todo.Open(list.Items)), len(list.Items), todo.Text(list.Items), todoUsage)
	return InfoData{Title: "Todo", Text: text, Rows: rows}
}
