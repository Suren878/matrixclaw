package automation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestScheduledAITaskToolValidatesBeforeApproval(t *testing.T) {
	service := NewService(newFakeStore(), &fakeRunner{}, "UTC").WithClock(func() time.Time {
		return time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	})
	args, _ := json.Marshal(map[string]any{
		"schedule_mode": "once",
		"run_at":        "not-a-time",
		"prompt":        "check status",
	})

	result, err := NewScheduledAITaskTool(service).Execute(context.Background(), tools.Call{
		SessionID: "session_1",
		Args:      args,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("Execute() result = %#v, want validation error", result)
	}
	if result.Approval != nil {
		t.Fatalf("Execute() approval = %#v, want nil for invalid input", result.Approval)
	}
	if !strings.Contains(result.Content, "run_at") {
		t.Fatalf("Execute() content = %q, want run_at validation message", result.Content)
	}
}

func TestScheduledAITaskToolRequestsApprovalForValidInput(t *testing.T) {
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	service := NewService(newFakeStore(), &fakeRunner{}, "UTC").WithClock(func() time.Time {
		return now
	})
	args, _ := json.Marshal(map[string]any{
		"schedule_mode": "once",
		"run_at":        now.Add(time.Hour).Format(time.RFC3339),
		"prompt":        "check status",
	})

	result, err := NewScheduledAITaskTool(service).Execute(context.Background(), tools.Call{
		SessionID: "session_1",
		Args:      args,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError || result.Approval == nil {
		t.Fatalf("Execute() result = %#v, want approval", result)
	}
}
