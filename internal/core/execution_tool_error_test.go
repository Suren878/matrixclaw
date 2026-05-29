package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	. "github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestRunContinuesAfterToolResultError(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	app.WithRunStarter(syncRunStarter{app: app})

	session := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	runtime := &toolErrorRecoveryRuntime{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewCoreCodingRegistry())

	if _, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: session.ID,
		Text:      "trigger bad grep",
	}); err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}

	run, err := sqliteStore.GetRun(ctx, "run_test_1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != RunStatusCompleted {
		t.Fatalf("run status = %q, error %q, want completed after model sees tool error", run.Status, run.Error)
	}
	messages, err := sqliteStore.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if !messagesContain(messages, "handled grep error") {
		t.Fatalf("messages missing recovery response: %#v", messages)
	}
	if runtime.calls != 2 {
		t.Fatalf("runtime calls = %d, want model called again after tool error", runtime.calls)
	}
}

type toolErrorRecoveryRuntime struct {
	calls int
}

func (r *toolErrorRecoveryRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	r.calls++
	if requestHasToolResultContent(request, "grep: error parsing regexp") {
		return providers.Response{Text: "handled grep error"}, nil
	}
	return providers.Response{ToolCalls: []providers.ToolCall{{
		ID:        "tool_bad_grep",
		Name:      "grep",
		Arguments: json.RawMessage(`{"pattern":"Update(msg","path":".","include":"*.go"}`),
	}}}, nil
}

func requestHasToolResultContent(request providers.Request, text string) bool {
	for _, message := range request.Messages {
		if message.Role != string(MessageRoleTool) {
			continue
		}
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}
