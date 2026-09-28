package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// renderTodoUpdates keeps one silent message per run with the todo list the run
// saved last, edited whenever the list changes.
func (w *Worker) renderTodoUpdates(ctx context.Context, target chatTarget, messages []transcript.Message, runID string, state *runDeliveryState) error {
	items, ok := runTodo(messages, runID)
	if !ok {
		return nil
	}
	text := renderTelegramTodo(items)
	if text == state.todo.text {
		return nil
	}
	messageID, err := w.editOrSendMessage(silentTelegramDelivery(ctx), target, state.todo.messageID, text, nil)
	if err != nil {
		return err
	}
	state.todo = sentTodoStatus{messageID: messageID, text: text}
	return nil
}

// runTodo is the list the run's last successful todo_write call saved.
func runTodo(messages []transcript.Message, runID string) ([]todo.Item, bool) {
	runID = strings.TrimSpace(runID)
	saved := map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Parts {
			result := part.ToolResult
			if strings.TrimSpace(message.RunID) == runID && result != nil && result.Name == todo.ToolName && !result.IsError && !strings.EqualFold(result.Status, "error") {
				saved[result.ToolCallID] = true
			}
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		parts := messages[i].Parts
		for j := len(parts) - 1; j >= 0; j-- {
			call := parts[j].ToolCall
			if call == nil || !saved[call.ID] {
				continue
			}
			if items, err := todo.Parse(json.RawMessage(call.Input)); err == nil {
				return items, true
			}
		}
	}
	return nil, false
}

func renderTelegramTodo(items []todo.Item) string {
	if len(items) == 0 {
		return "Todo list cleared"
	}
	lines := []string{fmt.Sprintf("Todo %d/%d", len(items)-len(todo.Open(items)), len(items))}
	for _, item := range items {
		marker := "⬜"
		switch item.Status {
		case todo.InProgress:
			marker = "▶️"
		case todo.Completed:
			marker = "✅"
		}
		lines = append(lines, marker+" "+item.Label())
	}
	return clipTelegramText(strings.Join(lines, "\n"))
}
