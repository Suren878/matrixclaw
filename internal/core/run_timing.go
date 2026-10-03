package core

import (
	"time"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func deriveRunTiming(run Run, approvals []Approval, messages []transcript.Message, now time.Time) RunTiming {
	runID := run.ID
	if runID == "" {
		return RunTiming{}
	}
	end := now
	if run.FinishedAt != nil && !run.FinishedAt.IsZero() {
		end = run.FinishedAt.UTC()
	}
	timing := RunTiming{
		TotalMillis: durationMillis(run.StartedAt, end),
		LastEventAt: run.UpdatedAt,
	}

	toolStarted := map[string]time.Time{}
	waitStart := run.StartedAt
	for _, message := range messages {
		if message.RunID != runID {
			continue
		}
		messageEventAt := message.UpdatedAt
		if messageEventAt.IsZero() {
			messageEventAt = message.CreatedAt
		}
		if messageEventAt.After(timing.LastEventAt) {
			timing.LastEventAt = messageEventAt
		}
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				timing.ModelMillis += durationMillis(waitStart, message.CreatedAt)
				toolStarted[part.ToolCall.ID] = message.CreatedAt
			}
			if part.ToolResult != nil {
				toolCallID := part.ToolResult.ToolCallID
				if startedAt, ok := toolStarted[toolCallID]; ok {
					timing.ToolMillis += durationMillis(startedAt, message.CreatedAt)
				}
				waitStart = message.CreatedAt
			}
		}
	}
	if end.After(waitStart) {
		timing.ModelMillis += durationMillis(waitStart, end)
	}

	for _, approval := range approvals {
		if approval.RunID != runID {
			continue
		}
		approvalEnd := now
		if approval.DecidedAt != nil && !approval.DecidedAt.IsZero() {
			approvalEnd = approval.DecidedAt.UTC()
		}
		timing.ApprovalMillis += durationMillis(approval.RequestedAt, approvalEnd)
	}
	return timing
}

func durationMillis(start time.Time, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
