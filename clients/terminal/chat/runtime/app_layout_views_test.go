package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestWorkingStatusPhaseToolLabels(t *testing.T) {
	tests := []struct {
		name string
		tool string
		want string
	}{
		{name: "bash", tool: "bash", want: "Executing command"},
		{name: "read", tool: "read", want: "Reading file"},
		{name: "write", tool: "write", want: "Writing file"},
		{name: "edit", tool: "edit", want: "Editing file"},
		{name: "multiedit", tool: "multiedit", want: "Editing file"},
		{name: "grep", tool: "grep", want: "Searching files"},
		{name: "glob", tool: "glob", want: "Searching files"},
		{name: "ls", tool: "ls", want: "Listing files"},
		{name: "web_fetch", tool: "web_fetch", want: "Fetching web page"},
		{name: "web_search", tool: "web_search", want: "Searching web"},
		{name: "unknown", tool: "custom_tool", want: "Running custom_tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newWorkingStatusTestModel(core.ClientSnapshot{
				SessionID: "session",
				Run:       workingStatusTestRun(core.RunStatusRunning),
				ToolUpdates: []core.ToolUpdate{{
					ToolCallID: "tool-call",
					ToolName:   tt.tool,
					State:      core.ToolLifecycleRequested,
					RunID:      "run",
					SessionID:  "session",
				}},
			})

			if got := m.workingStatusPhase(); got != tt.want {
				t.Fatalf("workingStatusPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkingStatusPhaseCurrentDelegateToolUsesToolText(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusRunning),
		Messages: []core.Message{{
			ID:        "assistant",
			SessionID: "session",
			RunID:     "run",
			Role:      core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindToolCall,
				ToolCall: &core.ToolCallPart{
					ID:    "delegate-call",
					Name:  "delegate_task",
					Input: `{"runtime":"matrixclaw"}`,
				},
			}},
		}},
		ToolUpdates: []core.ToolUpdate{{
			ToolCallID: "delegate-call",
			ToolName:   "delegate_task",
			State:      core.ToolLifecycleRequested,
			RunID:      "run",
			SessionID:  "session",
		}},
	})

	if got := m.workingStatusPhase(); got != "Running delegate_task" {
		t.Fatalf("workingStatusPhase() = %q, want %q", got, "Running delegate_task")
	}
}

func TestWorkingStatusPhaseCurrentBlockingSubagentUsesTaskText(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusRunning),
		Messages: []core.Message{{
			ID:        "assistant",
			SessionID: "session",
			RunID:     "run",
			Role:      core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindToolCall,
				ToolCall: &core.ToolCallPart{
					ID:    "delegate-call",
					Name:  "delegate_task",
					Input: `{"runtime":"matrixclaw"}`,
				},
			}},
		}},
		ToolUpdates: []core.ToolUpdate{{
			ToolCallID: "delegate-call",
			ToolName:   "delegate_task",
			State:      core.ToolLifecycleRequested,
			RunID:      "run",
			SessionID:  "session",
		}},
		Subagents: []core.SubagentTask{{
			ID:               "subagent",
			AgentName:        "Neo",
			DisplayName:      "Repo scan",
			Mode:             core.SubagentTaskModeBlocking,
			ParentSessionID:  "session",
			ParentRunID:      "run",
			ParentToolCallID: "delegate-call",
			Status:           core.SubagentTaskStatusRunning,
		}},
	})

	if got := m.workingStatusPhase(); got != "Waiting for subagent: Neo" {
		t.Fatalf("workingStatusPhase() = %q, want %q", got, "Waiting for subagent: Neo")
	}
}

func TestWorkingStatusPhaseIgnoresStaleSubagentToolUpdate(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		updateRunID string
	}{
		{name: "delegate with old run id", tool: "delegate_task", updateRunID: "old-run"},
		{name: "delegate without run id", tool: "delegate_task"},
		{name: "spawn with old run id", tool: "spawn_subagent", updateRunID: "old-run"},
		{name: "spawn without run id", tool: "spawn_subagent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newWorkingStatusTestModel(core.ClientSnapshot{
				SessionID: "session",
				Run:       workingStatusTestRun(core.RunStatusRunning),
				Messages: []core.Message{
					{
						ID:        "old-tool-call",
						SessionID: "session",
						RunID:     "old-run",
						Role:      core.MessageRoleAssistant,
						Parts: []core.MessagePart{{
							Kind: core.MessagePartKindToolCall,
							ToolCall: &core.ToolCallPart{
								ID:   "old-subagent-call",
								Name: tt.tool,
							},
						}},
					},
					{
						ID:        "assistant-thinking",
						SessionID: "session",
						RunID:     "run",
						Role:      core.MessageRoleAssistant,
						Parts: []core.MessagePart{{
							Kind:      core.MessagePartKindReasoning,
							Reasoning: &core.ReasoningPart{Text: "thinking about the normal prompt"},
						}},
					},
				},
				ToolUpdates: []core.ToolUpdate{{
					ToolCallID: "old-subagent-call",
					ToolName:   tt.tool,
					State:      core.ToolLifecycleRequested,
					RunID:      tt.updateRunID,
					SessionID:  "session",
				}},
			})

			if got := m.workingStatusPhase(); got != "Thinking" {
				t.Fatalf("workingStatusPhase() = %q, want %q", got, "Thinking")
			}
		})
	}
}

func TestWorkingStatusViewBusyPendingRunDoesNotShowWaitingSubagents(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusCompleted),
		Subagents: []core.SubagentTask{{
			ID:          "subagent_1",
			DisplayName: "Repo scan",
			Status:      core.SubagentTaskStatusRunning,
		}},
	})
	m.width = 100
	m.busy = true

	got := m.workingStatusView()
	if !strings.Contains(got, "Waiting for model") {
		t.Fatalf("workingStatusView() = %q, want model launch status", got)
	}
	if strings.Contains(got, "Waiting for subagents") || strings.Contains(got, "Repo scan") {
		t.Fatalf("workingStatusView() = %q, must not show idle subagent status while launching model", got)
	}
}

func TestDelegateRuntimeLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty input", input: "", want: "MatrixClaw"},
		{name: "invalid json", input: "{", want: "MatrixClaw"},
		{name: "empty runtime", input: `{"runtime":""}`, want: "MatrixClaw"},
		{name: "auto", input: `{"runtime":"auto"}`, want: "MatrixClaw"},
		{name: "matrixclaw", input: `{"runtime":"matrixclaw"}`, want: "MatrixClaw"},
		{name: "codex", input: `{"runtime":"codex"}`, want: "Codex"},
		{name: "openai codex", input: `{"runtime":"openai-codex"}`, want: "Codex"},
		{name: "claude", input: `{"runtime":"claude"}`, want: "Claude Code"},
		{name: "claude code", input: `{"runtime":"claude-code"}`, want: "Claude Code"},
		{name: "fallback title cased", input: `{"runtime":"my_runtime"}`, want: "My Runtime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := delegateRuntimeLabel(tt.input); got != tt.want {
				t.Fatalf("delegateRuntimeLabel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWorkingStatusPhaseApprovalWins(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusWaitingApproval),
		Approvals: []core.Approval{{
			ID:          "approval",
			SessionID:   "session",
			RunID:       "run",
			ToolCallRef: "tool-call",
			ToolName:    "bash",
			State:       core.ApprovalStatePending,
		}},
		ToolUpdates: []core.ToolUpdate{{
			ToolCallID: "tool-call",
			ToolName:   "bash",
			State:      core.ToolLifecycleRequested,
		}},
	})

	if got := m.workingStatusPhase(); got != "Waiting for permission" {
		t.Fatalf("workingStatusPhase() = %q, want %q", got, "Waiting for permission")
	}
}

func TestWorkingStatusPhaseReasoningOnly(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusRunning),
		Messages: []core.Message{{
			ID:        "assistant",
			SessionID: "session",
			RunID:     "run",
			Role:      core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind:      core.MessagePartKindReasoning,
				Reasoning: &core.ReasoningPart{Text: "internal reasoning"},
			}},
		}},
	})

	if got := m.workingStatusPhase(); got != "Thinking" {
		t.Fatalf("workingStatusPhase() = %q, want %q", got, "Thinking")
	}
}

func TestWorkingStatusPhaseVisibleText(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusRunning),
		Messages: []core.Message{{
			ID:        "assistant",
			SessionID: "session",
			RunID:     "run",
			Role:      core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind: core.MessagePartKindText,
				Text: &core.TextPart{Text: "visible answer"},
			}},
		}},
	})

	if got := m.workingStatusPhase(); got != "Writing response" {
		t.Fatalf("workingStatusPhase() = %q, want %q", got, "Writing response")
	}
}

func TestWorkingStatusPhaseAcceptedRun(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusAccepted),
	})

	if got := m.workingStatusPhase(); got != "Waiting for model" {
		t.Fatalf("workingStatusPhase() = %q, want %q", got, "Waiting for model")
	}
}

func TestWorkingStatusViewShowsPendingQueuedInputWithoutOverridingModelPhase(t *testing.T) {
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusRunning),
		Messages: []core.Message{{
			ID:        "assistant",
			SessionID: "session",
			RunID:     "run",
			Role:      core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind:      core.MessagePartKindReasoning,
				Reasoning: &core.ReasoningPart{Text: "thinking"},
			}},
		}},
		PendingInputs: []core.SessionInput{{
			ID:        "input",
			SessionID: "session",
			Mode:      core.BusyInputModeQueue,
			Status:    core.SessionInputStatusPending,
			Text:      "next",
		}},
	})
	m.width = 100
	m.busy = true

	got := m.workingStatusView()
	if !strings.Contains(got, "Thinking") {
		t.Fatalf("workingStatusView() = %q, want model phase", got)
	}
	if !strings.Contains(got, "Queued message pending") {
		t.Fatalf("workingStatusView() = %q, want queued input detail", got)
	}
}

func TestWorkingStatusViewShowsMainPhaseWithActiveSubagentSuffix(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusRunning),
		Messages: []core.Message{{
			ID:        "assistant",
			SessionID: "session",
			RunID:     "run",
			Role:      core.MessageRoleAssistant,
			Parts: []core.MessagePart{{
				Kind:      core.MessagePartKindReasoning,
				Reasoning: &core.ReasoningPart{Text: "thinking"},
			}},
		}},
		Subagents: []core.SubagentTask{
			{
				ID:          "subagent_1",
				AgentName:   "Neo",
				DisplayName: "Repo scan",
				Mode:        core.SubagentTaskModeAsync,
				Status:      core.SubagentTaskStatusRunning,
				CreatedAt:   now.Add(-2 * time.Minute),
			},
			{
				ID:          "subagent_2",
				AgentName:   "Trinity",
				DisplayName: "Tests",
				Mode:        core.SubagentTaskModeAsync,
				Status:      core.SubagentTaskStatusWaitingApproval,
				CreatedAt:   now.Add(-time.Minute),
			},
			{ID: "subagent_3", AgentName: "Morpheus", DisplayName: "Done", Mode: core.SubagentTaskModeAsync, Status: core.SubagentTaskStatusCompleted, CreatedAt: now},
		},
	})
	m.width = 100
	m.busy = true

	got := m.workingStatusView()
	for _, want := range []string{"Thinking", "Subagents: Neo, Trinity"} {
		if !strings.Contains(got, want) {
			t.Fatalf("workingStatusView() = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "Morpheus") || strings.Contains(got, "Done") {
		t.Fatalf("workingStatusView() = %q, completed subagent should be omitted", got)
	}
}

func TestWorkingStatusViewShowsActiveSubagentsWhenParentIdle(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	m := newWorkingStatusTestModel(core.ClientSnapshot{
		SessionID: "session",
		Run:       workingStatusTestRun(core.RunStatusCompleted),
		Subagents: []core.SubagentTask{
			{
				ID:          "subagent_1",
				AgentName:   "Neo",
				DisplayName: "Repo scan",
				Runtime:     "matrixclaw",
				Status:      core.SubagentTaskStatusRunning,
				CreatedAt:   now.Add(-2 * time.Minute),
				UpdatedAt:   now.Add(-40 * time.Second),
			},
			{
				ID:          "subagent_2",
				AgentName:   "Trinity",
				DisplayName: "Tests",
				Status:      core.SubagentTaskStatusWaitingApproval,
				CreatedAt:   now.Add(-time.Minute),
			},
			{ID: "subagent_3", AgentName: "Morpheus", DisplayName: "Done", Status: core.SubagentTaskStatusCompleted, CreatedAt: now},
		},
	})
	m.width = 100
	m.busy = false
	m.now = now

	got := m.workingStatusView()
	for _, want := range []string{"Subagents: Neo, Trinity"} {
		if !strings.Contains(got, want) {
			t.Fatalf("workingStatusView() = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "Morpheus") || strings.Contains(got, "Done") {
		t.Fatalf("workingStatusView() = %q, completed subagent should be omitted", got)
	}
}

func newWorkingStatusTestModel(snapshot core.ClientSnapshot) *appModel {
	m := newApp(context.Background(), nil)
	m.read = viewmodel.NewReadModel(snapshot)
	return m
}

func workingStatusTestRun(status core.RunStatus) *core.Run {
	return &core.Run{
		ID:        "run",
		SessionID: "session",
		Status:    status,
		StartedAt: time.Unix(1, 0),
	}
}
