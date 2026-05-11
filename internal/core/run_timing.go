package core

import (
	"strings"
	"time"
)

func deriveRunTiming(run Run, approvals []Approval, messages []Message, now time.Time) RunTiming {
	runID := strings.TrimSpace(run.ID)
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
		if strings.TrimSpace(message.RunID) != runID {
			continue
		}
		if message.CreatedAt.After(timing.LastEventAt) {
			timing.LastEventAt = message.CreatedAt
		}
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				timing.ModelMillis += durationMillis(waitStart, message.CreatedAt)
				toolStarted[strings.TrimSpace(part.ToolCall.ID)] = message.CreatedAt
			}
			if part.ToolResult != nil {
				toolCallID := strings.TrimSpace(part.ToolResult.ToolCallID)
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
		if strings.TrimSpace(approval.RunID) != runID {
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
