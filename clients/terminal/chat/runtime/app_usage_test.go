package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestContextUsageTextUsesEffectiveContextAfterClear(t *testing.T) {
	m := newApp(context.Background(), nil)
	m.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session",
		Session: &core.Session{
			ID:         "session",
			ProviderID: "provider",
			ModelID:    "model",
		},
		Context: &core.ContextReport{
			TokenEstimate: 100,
			WindowTokens:  1000,
			Blocks: []core.ContextBlock{{
				Kind:          core.ContextBlockClearMarker,
				TokenEstimate: 6,
				Included:      true,
			}},
		},
		Messages: []core.Message{
			{ID: "old", Role: core.MessageRoleUser, Content: strings.Repeat("old context ", 5000)},
			{ID: "clear", Role: core.MessageRoleSystem, Content: core.ContextClearedMessageContent()},
		},
	})

	got := m.contextUsageText()
	if !strings.Contains(got, "Context: ~100 / 1.0k") {
		t.Fatalf("contextUsageText() = %q, want cleared context report estimate", got)
	}
	if strings.Contains(got, "15k") {
		t.Fatalf("contextUsageText() = %q, counted pre-clear visible history", got)
	}
}

func TestContextUsageTextIgnoresStaleReportAfterVisibleClearMarker(t *testing.T) {
	m := newApp(context.Background(), nil)
	m.read = viewmodel.NewReadModel(core.ClientSnapshot{
		SessionID: "session",
		Session:   &core.Session{ID: "session"},
		Context: &core.ContextReport{
			TokenEstimate: 50_000,
			WindowTokens:  100_000,
			Blocks: []core.ContextBlock{{
				Kind:          core.ContextBlockMessages,
				TokenEstimate: 50_000,
				Included:      true,
			}},
		},
		Messages: []core.Message{
			{ID: "old", Role: core.MessageRoleUser, Content: strings.Repeat("old context ", 5000)},
			{ID: "clear", Role: core.MessageRoleSystem, Content: core.ContextClearedMessageContent()},
		},
	})

	got := m.contextUsageText()
	want := fmt.Sprintf("Context: ~%s / 100k", formatTokenCount(estimateTokens("Context cleared by user.")))
	if !strings.Contains(got, want) {
		t.Fatalf("contextUsageText() = %q, want immediate visible clear estimate %q", got, want)
	}
	if strings.Contains(got, "50k") {
		t.Fatalf("contextUsageText() = %q, used stale pre-clear report", got)
	}
}
