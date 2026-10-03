package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestRunStepsAnnounceContextUsageOnlyWhenItChanges(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(&recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		usage := providers.Usage{PromptTokens: 700, OutputTokens: 20}
		if calls < 3 {
			return providers.Response{Text: "Checking.", Usage: usage, ToolCalls: []providers.ToolCall{{ID: "call-" + string(rune('0'+calls)), Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		usage.PromptTokens = 900
		return providers.Response{Text: "Done.", Usage: usage}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "context-usage", core.RunStatusAccepted, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := app.SubscribeEvents(ctx, session.ID)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	var usages []core.ContextUsage
	for _, event := range drainEvents(events) {
		if event.Type == core.EventContextUpdated {
			usages = append(usages, event.Payload.(core.ContextUsage))
		}
	}
	if len(usages) != 2 || usages[0].TokenEstimate != 720 || usages[1].TokenEstimate != 920 || usages[0].SessionID != session.ID {
		t.Fatalf("context usages = %+v", usages)
	}
}

func TestClientSnapshotNamesTheNewestEventItCovers(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "snapshot-event-id", core.RunStatusAccepted, false)
	ctx := context.Background()
	if _, err := app.UseBinding(ctx, core.UseBindingInput{Client: "terminal:test", ExternalKey: "local", SessionID: session.ID}); err != nil {
		t.Fatal(err)
	}
	events := app.SubscribeEvents(ctx, session.ID)
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	published := drainEvents(events)

	snapshot, err := app.ClientSnapshot(ctx, "terminal:test", "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(published) == 0 || snapshot.EventID != published[len(published)-1].ID {
		t.Fatalf("snapshot event id = %d, last published = %+v", snapshot.EventID, published)
	}
}
