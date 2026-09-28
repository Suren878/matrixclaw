package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
)

const todoUsage = "Usage: /todo, /todo clear"

func (d *Dispatcher) handleTodo(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.todo == nil {
		return unsupportedRuntime("todo"), nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if !core.CapabilitiesForSession(*session).NativeTools {
		return Result{Handled: true, Text: "Todo lists are kept by Matrixclaw sessions only."}, nil
	}
	switch strings.ToLower(strings.TrimSpace(args)) {
	case "":
		list, err := d.todo.SessionTodo(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		info := todoInfoData(list)
		return Result{Handled: true, Info: &info}, nil
	case "clear":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Clear the todo list?",
			ConfirmLabel:   "Clear",
			CancelLabel:    "Close",
			ConfirmCommand: "/todo clear confirm",
			CancelCommand:  "/todo",
		}}, nil
	case "clear confirm":
		if _, err := d.todo.ClearSessionTodo(ctx, session.ID); err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Todo list cleared.", ReloadSnapshot: true}, nil
	default:
		return Result{Handled: true, Text: todoUsage}, nil
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
