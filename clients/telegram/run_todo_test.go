package telegram

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func todoCall(runID string, id string, items string) transcript.Message {
	return transcript.Message{ID: "m_" + id, RunID: runID, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: todo.ToolName, Input: `{"items":` + items + `}`, Finished: true},
	}}}
}

func todoResult(runID string, id string, failed bool) transcript.Message {
	return transcript.Message{ID: "r_" + id, RunID: runID, Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: id, Name: todo.ToolName, IsError: failed},
	}}}
}

func TestRunTodoIsOneSilentMessageEditedAsTheListChanges(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := &Worker{api: api}
	target := chatTarget{chatID: 7, externalKey: "7"}
	state := newRunDeliveryState()
	messages := []transcript.Message{
		todoCall("run-1", "c1", `[{"content":"Fix the bug","active_form":"Fixing the bug","status":"in_progress"},{"content":"Run the tests","status":"pending"}]`),
		todoResult("run-1", "c1", false),
	}
	render := func() {
		t.Helper()
		if err := worker.renderTodoUpdates(context.Background(), target, messages, "run-1", state); err != nil {
			t.Fatal(err)
		}
	}

	render()
	render()
	if api.sendCount() != 1 || api.messages[0].Text != "Todo 0/2\n▶️ Fixing the bug\n⬜ Run the tests" || !api.messages[0].DisableNotification {
		t.Fatalf("sent = %+v", api.messages)
	}
	messages = append(messages,
		todoCall("run-1", "c2", `[{"content":"Fix the bug","status":"completed"},{"content":"Run the tests","status":"in_progress"}]`),
		todoResult("run-1", "c2", false),
		todoCall("run-1", "c3", `[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]`),
		todoResult("run-1", "c3", true),
		todoCall("run-2", "c4", `[]`),
		todoResult("run-2", "c4", false),
	)
	render()

	if api.sendCount() != 1 || api.editCount() != 1 || api.messages[0].Text != "Todo 1/2\n✅ Fix the bug\n▶️ Run the tests" {
		t.Fatalf("sent = %+v edits = %d", api.messages, api.editCount())
	}
}

func TestTodoWriteHasNoToolStatusMessage(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := &Worker{api: api}
	messages := []transcript.Message{todoCall("run-1", "c1", `[]`), todoResult("run-1", "c1", false)}
	state := newRunDeliveryState()

	if err := worker.renderToolCallUpdates(context.Background(), chatTarget{chatID: 7, externalKey: "7"}, messages, "run-1", state); err != nil {
		t.Fatal(err)
	}
	if err := worker.renderToolResultUpdates(context.Background(), chatTarget{chatID: 7, externalKey: "7"}, messages, "run-1", state); err != nil {
		t.Fatal(err)
	}

	if api.sendCount() != 0 {
		t.Fatalf("sent = %+v", api.messages)
	}
}
