package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type progressCountingStore struct {
	Store
	run             Run
	message         transcript.Message
	progressSaves   int
	progressUpdates int
}

func (s *progressCountingStore) SaveMessageProgress(_ context.Context, message transcript.Message) error {
	s.progressSaves++
	s.message = message
	return nil
}

func (s *progressCountingStore) UpdateMessageProgress(_ context.Context, message transcript.Message) error {
	s.progressUpdates++
	s.message = message
	return nil
}

func (s *progressCountingStore) GetRun(context.Context, string) (Run, error) {
	return s.run, nil
}

func (s *progressCountingStore) GetSubagentTaskByChildRun(context.Context, string) (SubagentTask, error) {
	return SubagentTask{}, ErrNotFound
}

type streamingBurstRuntime struct {
	deltas int
}

func (r streamingBurstRuntime) Generate(ctx context.Context, _ providers.Request) (providers.Response, error) {
	for i := 0; i < r.deltas; i++ {
		if err := providers.StreamText(ctx, "x"); err != nil {
			return providers.Response{}, err
		}
	}
	return providers.Response{Text: strings.Repeat("x", r.deltas)}, nil
}

func TestGenerateAssistantTurnBatchesNativeStreamingPersistence(t *testing.T) {
	now := time.Now().UTC()
	store := &progressCountingStore{run: Run{ID: "run-1", Status: RunStatusRunning}}
	app := New(store).WithClock(func() time.Time { return now })
	const deltaCount = 4096
	assistant, saved, _, err := app.generateAssistantTurn(context.Background(), turnExecution{
		RunID:     "run-1",
		SessionID: "session-1",
		Runtime:   streamingBurstRuntime{deltas: deltaCount},
	}, providers.Request{})
	if err != nil {
		t.Fatalf("generate assistant turn: %v", err)
	}
	if !saved {
		t.Fatal("assistant was not persisted")
	}
	if len(assistant.Content) != deltaCount {
		t.Fatalf("assistant content bytes = %d, want %d", len(assistant.Content), deltaCount)
	}
	if store.progressSaves != 1 || store.progressUpdates != 1 {
		t.Fatalf("progress writes = %d saves + %d updates, want 1 + 1 for burst", store.progressSaves, store.progressUpdates)
	}
	if len(store.message.Parts) != 1 || store.message.Parts[0].Text == nil || len(store.message.Parts[0].Text.Text) != deltaCount {
		t.Fatalf("persisted message parts do not contain the complete streamed text")
	}
}
