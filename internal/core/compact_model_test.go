package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
)

// twoModelLLMs resolves the provider "cheap" to its own runtime.
type twoModelLLMs struct {
	recoveryLLMs
	cheap providers.Runtime
}

func (l twoModelLLMs) Resolve(ctx context.Context, providerID string, modelID string) (providers.Runtime, core.SessionProviderOption, string, error) {
	if providerID == "cheap" {
		return l.cheap, core.SessionProviderOption{ID: "cheap", Configured: true, DefaultModel: modelID}, modelID, nil
	}
	return l.recoveryLLMs.Resolve(ctx, providerID, modelID)
}

func TestManualCompactUsesTheCompactModel(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var mainCalls, cheapCalls int
	app.WithSessionLLMs(twoModelLLMs{
		recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
			mainCalls++
			return providers.Response{Text: "main summary"}, nil
		})},
		cheap: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
			cheapCalls++
			return providers.Response{Text: "CHEAP SUMMARY"}, nil
		}),
	})
	app.WithCompactModel("cheap", "small")
	session, _ := saveCrashRecoveryRun(t, db, "cheap", core.RunStatusCompleted, false)

	result, err := app.CompactSession(context.Background(), session.ID)

	if err != nil || mainCalls != 0 || cheapCalls != 1 || result.Message.Compaction == nil || result.Message.Compaction.Summary != "CHEAP SUMMARY" {
		t.Fatalf("result = %+v err = %v main = %d cheap = %d", result.Message, err, mainCalls, cheapCalls)
	}
}
