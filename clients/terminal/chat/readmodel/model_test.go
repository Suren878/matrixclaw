package readmodel

import (
	"encoding/json"
	"testing"
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func apply(t *testing.T, m *Model, eventType core.EventType, sessionID string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(daemonclient.LiveEvent{Type: eventType, SessionID: sessionID, Payload: raw}); err != nil {
		t.Fatal(err)
	}
}

func text(id string, role transcript.MessageRole, content string) transcript.Message {
	return transcript.Message{ID: id, SessionID: "s1", Role: role, Content: content, Parts: transcript.NormalizeMessageParts(content, nil)}
}

func ids(messages []surfacemessage.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.ID)
	}
	return out
}

func TestMessagesUpdateInPlaceAndKeepTheirOrder(t *testing.T) {
	m := New(core.ClientSnapshot{SessionID: "s1", Session: &core.Session{ID: "s1", ProviderID: "openrouter", ModelID: "gpt-test"}, Messages: []transcript.Message{text("u1", transcript.MessageRoleUser, "hi")}})
	before := m.Messages()

	apply(t, m, core.EventMessageCreated, "s1", transcript.Message{ID: "a1", SessionID: "s1", Role: transcript.MessageRoleAssistant})
	if got := ids(m.Messages()); len(got) != 1 {
		t.Fatalf("an empty reply is shown: %v", got)
	}
	apply(t, m, core.EventMessageUpdated, "s1", text("a1", transcript.MessageRoleAssistant, "hello"))
	apply(t, m, core.EventMessageCreated, "s1", text("u2", transcript.MessageRoleUser, "again"))
	apply(t, m, core.EventMessageUpdated, "s1", text("a1", transcript.MessageRoleAssistant, "hello there"))
	apply(t, m, core.EventMessageCreated, "other", text("x1", transcript.MessageRoleUser, "not ours"))

	messages := m.Messages()
	if got := ids(messages); len(got) != 3 || got[0] != "u1" || got[1] != "a1" || got[2] != "u2" {
		t.Fatalf("order = %v", got)
	}
	if messages[1].Content().Text != "hello there" || messages[1].Model != "gpt-test" || messages[1].Provider != "openrouter" {
		t.Fatalf("reply = %+v", messages[1])
	}
	if len(before) != 1 || before[0].ID != "u1" {
		t.Fatalf("an earlier Messages slice changed: %v", ids(before))
	}
	if m.Revision("a1") <= m.Revision("u2") {
		t.Fatalf("revisions: a1 %d, u2 %d", m.Revision("a1"), m.Revision("u2"))
	}
}

func TestApprovalResultClearsTheRequestAndNotesTheDecision(t *testing.T) {
	m := New(core.ClientSnapshot{SessionID: "s1", Approvals: []core.Approval{
		{ID: "b", SessionID: "s1", ToolCallRef: "call_b", ToolName: "bash", Path: "/z", State: core.ApprovalStatePending},
		{ID: "a", SessionID: "s1", ToolCallRef: "call_a", ToolName: "bash", Path: "/a", State: core.ApprovalStatePending},
	}})
	if approvals := m.Approvals(); len(approvals) != 2 || approvals[0].ID != "a" {
		t.Fatalf("approvals = %+v", approvals)
	}

	apply(t, m, core.EventApprovalResult, "s1", core.PermissionNotification{ApprovalID: "a", ToolCallID: "call_a", Denied: true})

	if approvals := m.Approvals(); len(approvals) != 1 || approvals[0].ID != "b" {
		t.Fatalf("approvals = %+v", approvals)
	}
	if notes := m.ApprovalNotifications(); len(notes) != 1 || notes[0].ToolCallID != "call_a" || !notes[0].Denied {
		t.Fatalf("notifications = %+v", notes)
	}
}

func TestSubagentsAndInputsFollowTheirEvents(t *testing.T) {
	now := time.Now()
	m := New(core.ClientSnapshot{SessionID: "s1"})
	apply(t, m, core.EventTaskUpdated, "s1", core.Task{ID: "t2", Kind: core.TaskKindSubagent, Runtime: "codex", Status: core.TaskStatusRunning, ParentToolCallID: "call_2", StartedAt: now.Add(time.Second)})
	apply(t, m, core.EventTaskUpdated, "s1", core.Task{ID: "t1", Kind: core.TaskKindSubagent, AgentName: "scout", Status: core.TaskStatusLost, StartedAt: now})
	apply(t, m, core.EventTaskUpdated, "s1", core.Task{ID: "t3", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, StartedAt: now})
	apply(t, m, core.EventInputUpdated, "s1", core.SessionInput{ID: "i1", Status: core.SessionInputStatusPending, Mode: core.BusyInputModeQueue})
	apply(t, m, core.EventInputUpdated, "s1", core.SessionInput{ID: "i1", Status: core.SessionInputStatusConsumed})

	subagents := m.Subagents()
	if len(subagents) != 2 || subagents[0].Name != "scout" || subagents[0].State != surfacemessage.SubagentFailed {
		t.Fatalf("subagents = %+v", subagents)
	}
	if second := subagents[1]; second.Name != "Codex" || !second.Blocking || !second.State.Active() || second.ParentToolCallID != "call_2" {
		t.Fatalf("second subagent = %+v", second)
	}
	if inputs := m.PendingInputs(); len(inputs) != 0 {
		t.Fatalf("consumed input still pending: %+v", inputs)
	}
}

func TestContextUpdateKeepsTheRestOfTheReport(t *testing.T) {
	m := New(core.ClientSnapshot{SessionID: "s1", Context: &core.ContextReport{SessionID: "s1", TokenEstimate: 10, WindowTokens: 100, MessageCount: 4}})

	apply(t, m, core.EventContextUpdated, "s1", core.ContextUsage{SessionID: "s1", TokenEstimate: 70, WindowTokens: 128})

	if report := m.Context(); report.TokenEstimate != 70 || report.WindowTokens != 128 || report.MessageCount != 4 {
		t.Fatalf("context = %+v", report)
	}
}

func TestSnapshotApprovalKeepsTheSubagentName(t *testing.T) {
	m := New(core.ClientSnapshot{SessionID: "parent", Approvals: []core.Approval{{
		ID: "a1", SessionID: "child", TaskID: "task_1", AgentName: "researcher", ToolCallRef: "call_1", ToolName: "bash", State: core.ApprovalStatePending,
	}}})
	if got := m.Approvals()[0].AgentName; got != "researcher" {
		t.Fatalf("agent name = %q, want researcher", got)
	}
}
