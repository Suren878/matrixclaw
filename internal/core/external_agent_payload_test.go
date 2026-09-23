package core

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestBlockingSubagentWorkJobCarriesHeartbeat(t *testing.T) {
	now := time.Now().UTC()
	job := subagentWorkJob(SubagentTask{
		ID:        "subagent-1",
		Mode:      SubagentTaskModeBlocking,
		Status:    SubagentTaskStatusRunning,
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now,
	})
	if job.HeartbeatAt == nil || !job.HeartbeatAt.Equal(now) {
		t.Fatalf("heartbeat = %v, want %v", job.HeartbeatAt, now)
	}
	if job.StartedAt == nil || job.Attempts != 1 {
		t.Fatalf("started_at = %v attempts = %d, want active job metadata", job.StartedAt, job.Attempts)
	}
}

func TestRunTimingUsesStreamingMessageUpdateAsLastEvent(t *testing.T) {
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

func TestExternalToolOutputIsBounded(t *testing.T) {
	assistant := &transcript.Message{}
	delta := strings.Repeat("д", externalToolOutputPerItemLimit)
	for i := 0; i < 8; i++ {
		itemID := string(rune('a' + i))
		for part := 0; part < 4; part++ {
			applyExternalToolOutputDelta(assistant, externalagents.Event{ItemID: itemID, Text: delta})
		}
	}

	total := 0
	results := 0
	for _, part := range assistant.Parts {
		if part.ToolResult == nil {
			continue
		}
		results++
		content := part.ToolResult.Content
		total += len(content)
		if len(content) > externalToolOutputPerItemLimit {
			t.Fatalf("tool result %q bytes = %d, want at most %d", part.ToolResult.ToolCallID, len(content), externalToolOutputPerItemLimit)
		}
		if !utf8.ValidString(content) {
			t.Fatalf("tool result %q is not valid UTF-8", part.ToolResult.ToolCallID)
		}
	}
	if results != 8 {
		t.Fatalf("tool result count = %d, want 8", results)
	}
	maxWithMarkers := externalToolOutputTotalLimit + results*len(externalTruncationMarker)
	if total > maxWithMarkers {
		t.Fatalf("total retained tool output = %d, want at most %d", total, maxWithMarkers)
	}
}

func TestClipExternalPayloadKeepsHeadTailAndValidUTF8(t *testing.T) {
	value := "HEAD-" + strings.Repeat("я", 100) + "-TAIL"
	clipped := clipExternalPayload(value, 120)
	if len(clipped) > 120 {
		t.Fatalf("clipped bytes = %d, want at most 120", len(clipped))
	}
	if !utf8.ValidString(clipped) {
		t.Fatalf("clipped payload is not valid UTF-8: %q", clipped)
	}
	if !strings.HasPrefix(clipped, "HEAD-") || !strings.HasSuffix(clipped, "-TAIL") {
		t.Fatalf("clipped payload = %q, want preserved head and tail", clipped)
	}
	if !strings.Contains(clipped, "truncated") {
		t.Fatalf("clipped payload = %q, want truncation marker", clipped)
	}
}
