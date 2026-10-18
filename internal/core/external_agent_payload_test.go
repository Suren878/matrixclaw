package core

import (
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestRunTimingUsesStreamingMessageUpdateAsLastEvent(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().UTC().Add(-time.Minute)
	updatedAt := startedAt.Add(45 * time.Second)
	timing := deriveRunTiming(Run{
		ID:        "run-1",
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}, nil, []transcript.Message{{
		RunID:     "run-1",
		CreatedAt: startedAt.Add(time.Second),
		UpdatedAt: updatedAt,
	}}, time.Now().UTC())
	if !timing.LastEventAt.Equal(updatedAt) {
		t.Fatalf("last event = %v, want message update %v", timing.LastEventAt, updatedAt)
	}
}
