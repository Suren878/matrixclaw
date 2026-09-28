package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestNextRunInASessionKeepsWhatTheLastRunElided(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("probe", tools.EffectReadOnly), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		return tools.Result{Content: call.ToolCallID + "\n" + strings.Repeat("y", 8_000)}, nil
	}}))
	// The provider counts ~26k tokens where the estimate sees far fewer, so the
	// first run elides old outputs; the second run's own estimate stays under
	// the elision threshold.
	var requests []providers.Request
	steps := 0
	app.WithSessionLLMs(windowLLMs{window: 60_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		usage := providers.Usage{PromptTokens: 26_000, OutputTokens: 10}
		last := request.Messages[len(request.Messages)-1]
		if steps >= 8 || strings.HasPrefix(last.Content, "next task") {
			return providers.Response{Text: "Done.", Usage: usage}, nil
		}
		steps++
		arguments := json.RawMessage(fmt.Sprintf(`{"n":%d}`, steps))
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("probe_%d", steps), Name: "probe", Arguments: arguments}}, Usage: usage}, nil
	})}})
	session, first := saveNativeRunWithHistory(t, db, "carry")
	if err := app.ExecuteRun(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	lastOfFirst := requests[len(requests)-1]

	second := core.Run{ID: "run_carry_next", SessionID: session.ID, UserMessageID: "msg_user_carry_next", Status: core.RunStatusAccepted, StartedAt: runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime()}
	user := transcript.Message{
		ID: second.UserMessageID, SessionID: session.ID, RunID: second.ID, Role: transcript.MessageRoleUser,
		Content: "next task", Parts: transcript.NormalizeMessageParts("next task", nil), CreatedAt: runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime(),
	}
	if err := db.AcceptMessage(context.Background(), user, second); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, second.ID, core.RunStatusCompleted)
	elided, carried := elidedCalls(lastOfFirst), elidedCalls(requests[len(requests)-1])
	if len(elided) == 0 || !slices.Equal(elided, carried) {
		t.Fatalf("first run ended eliding %v, the next run starts eliding %v", elided, carried)
	}
}

// elidedCalls lists the calls whose results a request hides.
func elidedCalls(request providers.Request) []string {
	var ids []string
	for _, message := range request.Messages {
		if message.ToolCallID != "" && strings.HasPrefix(message.Content, "[output of probe(") {
			ids = append(ids, message.ToolCallID)
		}
	}
	return ids
}
