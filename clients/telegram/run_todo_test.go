package telegram

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func todoCall(runID string, id string, items string) transcript.Message {
	return transcript.Message{ID: "m_" + id, RunID: runID, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: todo.ToolName, Input: `{"items":` + items + `}`, Finished: true},
	}}}
}

func todoResult(runID string, id string, failed bool) transcript.Message {
	status := "success"
	if failed {
		status = "error"
	}
	return transcript.Message{ID: "r_" + id, RunID: runID, Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: id, Name: todo.ToolName, Status: status},
	}}}
}

func TestRunStatusShowsTheListTheRunSavedLast(t *testing.T) {
	run := core.Run{ID: "run-1", Status: core.RunStatusRunning}
	messages := []transcript.Message{
		todoCall("run-1", "c1", `[{"content":"Fix the bug","active_form":"Fixing the bug","status":"in_progress"},{"content":"Run the tests","status":"pending"}]`),
		todoResult("run-1", "c1", false),
	}
	if got := renderRunStatusText(run, core.RunProgress{}, messages, nil); got != "⏳ Working\nThinking...\n\nTodo 0/2\n▶️ Fixing the bug\n⬜ Run the tests" {
		t.Fatalf("status = %q", got)
	}

	messages = append(messages,
		todoCall("run-1", "c2", `[{"content":"Fix the bug","status":"completed"},{"content":"Run the tests","status":"in_progress"}]`),
		todoResult("run-1", "c2", false),
		todoCall("run-1", "c3", `[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]`),
		todoResult("run-1", "c3", true),
		todoCall("run-2", "c4", `[]`),
		todoResult("run-2", "c4", false),
	)
	if got := renderRunStatusText(run, core.RunProgress{}, messages, nil); got != "⏳ Working\nThinking...\n\nTodo 1/2\n✅ Fix the bug\n▶️ Run the tests" {
		t.Fatalf("status = %q", got)
	}
}
