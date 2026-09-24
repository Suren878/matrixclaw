package core_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type funcTool struct {
	spec tools.Spec
	fn   func(context.Context, tools.Call) (tools.Result, error)
}

func (t funcTool) Spec() tools.Spec { return t.spec }
func (t funcTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	return t.fn(ctx, call)
}

type windowLLMs struct {
	recoveryLLMs
	window int
}

func (w windowLLMs) ContextWindowTokens(string, string) (int, bool) { return w.window, true }

func drainEvents(events <-chan core.Event) []core.Event {
	var out []core.Event
	for {
		select {
		case event := <-events:
			out = append(out, event)
		default:
			return out
		}
	}
}

func describeEvent(event core.Event) string {
	switch payload := event.Payload.(type) {
	case transcript.Message:
		kinds := make([]string, 0, len(payload.Parts))
		for _, part := range payload.Parts {
			kind := string(part.Kind)
			if part.ToolCall != nil && part.ToolCall.Finished {
				kind += "(done)"
			}
			kinds = append(kinds, kind)
		}
		return fmt.Sprintf("%s %s/%s", event.Type, payload.Role, strings.Join(kinds, ","))
	case core.ToolUpdate:
		return fmt.Sprintf("%s %s", event.Type, payload.State)
	case core.Run:
		return fmt.Sprintf("%s %s", event.Type, payload.Status)
	default:
		return string(event.Type)
	}
}

func sessionMessages(t *testing.T, db *store.SQLiteStore, sessionID string) []transcript.Message {
	t.Helper()
	messages, err := db.ListMessages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestNativeToolRoundPublishesEventsInOrder(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	now := runRecoveryTestTime()
	app.WithClock(func() time.Time { return now })
	app.WithTools(tools.NewRegistry(&recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}))
	usage := providers.Usage{PromptTokens: 3, OutputTokens: 1}
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			if err := providers.StreamText(ctx, "Checking"); err != nil {
				return providers.Response{}, err
			}
			return providers.Response{Text: "Checking.", Usage: usage, ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		if err := providers.StreamText(ctx, "Done"); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{Text: "Done.", Usage: usage}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "events", core.RunStatusAccepted, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := app.SubscribeEvents(ctx, session.ID)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	var trace []string
	for _, event := range drainEvents(events) {
		switch event.Type {
		case core.EventRunUpdated, core.EventMessageCreated, core.EventMessageUpdated, core.EventToolUpdated:
			trace = append(trace, describeEvent(event))
		}
	}
	want := []string{
		"run.updated running",
		"message.created assistant/text",
		"message.updated assistant/text,finish",
		"message.created assistant/tool_call",
		"tool.updated requested",
		"message.updated assistant/tool_call(done)",
		"message.created tool/tool_result",
		"tool.updated completed",
		"message.created assistant/text",
		"message.updated assistant/text,finish",
		"run.updated completed",
	}
	if got := strings.Join(trace, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("event trace:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

func TestNativeRunCheckpointsModelAndToolPhases(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var run core.Run
	var seen []string
	record := func(label string) {
		checkpoint, err := db.GetRunCheckpoint(context.Background(), run.ID)
		if err != nil {
			seen = append(seen, label+":none")
			return
		}
		seen = append(seen, fmt.Sprintf("%s:%s:%s:%s", label, checkpoint.Phase, checkpoint.ToolCallID, checkpoint.ToolName))
	}
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		record("tool")
		return tools.Result{Content: "inspected"}, nil
	}}))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		record(fmt.Sprintf("model%d", calls))
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	_, run = saveCrashRecoveryRun(t, db, "checkpoints", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	want := []string{"model1:model::", "tool:tool:call-1:inspect_state", "model2:model::"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("checkpoints = %v, want %v", seen, want)
	}
	waitForRecoveryCheckpointGone(t, db, run.ID)
}

func TestNativeRunFailsAfterThirtyTwoToolSteps(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("step-%d", calls), Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "step-cap", core.RunStatusAccepted, false)

	err := app.ExecuteRun(context.Background(), run.ID)

	if err == nil || err.Error() != "tool loop exceeded 32 steps" {
		t.Fatalf("ExecuteRun error = %v", err)
	}
	got, getErr := db.GetRun(context.Background(), run.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.Status != core.RunStatusFailed || got.Error != "tool loop exceeded 32 steps" {
		t.Fatalf("run = %s (%s)", got.Status, got.Error)
	}
	if calls != 32 || tool.callCount() != 32 {
		t.Fatalf("model calls=%d tool calls=%d, want 32/32", calls, tool.callCount())
	}
}
