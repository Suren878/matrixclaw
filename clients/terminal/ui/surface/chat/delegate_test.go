package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestDelegateTaskPendingRendersAgentWorkingLabelAndTask(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:    "tool_1",
		Name:  "delegate_task",
		Input: `{"goal":"Inspect failing tests","runtime":"claude"}`,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool_1",
		Name:       "delegate_task",
		Metadata:   `{"agent_name":"Neo","display_name":"Inspect failing tests","goal":"Inspect failing tests","status":"running"}`,
		Status:     "neutral",
	}, false)

	rendered := item.RawRender(100)
	for _, want := range []string{"◇ Neo working...", "Inspect failing tests"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("pending delegate render missing %q:\n%s", want, rendered)
		}
	}
}

func TestDelegateTaskCompletedRendersAgentTaskAndSummary(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:       "tool_1",
		Name:     "delegate_task",
		Input:    `{"goal":"Inspect failing tests","runtime":"codex"}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool_1",
		Name:       "delegate_task",
		Content:    "Found one failing renderer test.",
		Metadata:   `{"agent_name":"Trinity","display_name":"Inspect failing tests","goal":"Inspect failing tests","status":"completed","summary":"Found one failing renderer test."}`,
		Status:     "success",
	}, false)

	rendered := item.RawRender(100)
	for _, want := range []string{"✓ Trinity completed", "Inspect failing tests", "Found one failing renderer test."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("completed delegate render missing %q:\n%s", want, rendered)
		}
	}
}

func TestSpawnSubagentRunningAfterToolResultRendersAgentWorking(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:       "tool_1",
		Name:     "spawn_subagent",
		Input:    `{"name":"Repo scan","goal":"Inspect failing tests","runtime":"matrixclaw"}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool_1",
		Name:       "spawn_subagent",
		Content:    "Subagent Neo started",
		Metadata:   `{"agent_name":"Neo","display_name":"Repo scan","goal":"Inspect failing tests","status":"running"}`,
		Status:     "neutral",
	}, false)

	rendered := item.RawRender(100)
	for _, want := range []string{"◇ Neo working...", "Repo scan", "Inspect failing tests"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("spawn running render missing %q:\n%s", want, rendered)
		}
	}
}

func TestSpawnSubagentTerminalStatesRenderAgentAndError(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		metadata string
		want     string
	}{
		{name: "failed", status: "error", metadata: `{"agent_name":"Morpheus","display_name":"Repo scan","goal":"Inspect failing tests","status":"failed","error":"child failed"}`, want: "✕ Morpheus failed"},
		{name: "canceled", status: "error", metadata: `{"agent_name":"Niobe","display_name":"Repo scan","goal":"Inspect failing tests","status":"canceled","error":"child canceled"}`, want: "✕ Niobe canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sty := surfacestyles.DefaultStyles()
			item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
				ID:       "tool_1",
				Name:     "spawn_subagent",
				Input:    `{"name":"Repo scan","goal":"Inspect failing tests"}`,
				Finished: true,
			}, &surfacemessage.ToolResult{
				ToolCallID: "tool_1",
				Name:       "spawn_subagent",
				Content:    "child failed",
				Metadata:   tt.metadata,
				Status:     tt.status,
				IsError:    true,
			}, false)

			rendered := item.RawRender(100)
			if !strings.Contains(rendered, tt.want) || !strings.Contains(rendered, "child") {
				t.Fatalf("terminal render = %q, want %q and error detail", rendered, tt.want)
			}
		})
	}
}

func TestSubagentCardExpansionIncludesFullTaskText(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:       "tool_1",
		Name:     "spawn_subagent",
		Input:    `{"name":"Repo scan","goal":"Inspect failing tests across the renderer and status bar"}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool_1",
		Name:       "spawn_subagent",
		Metadata:   `{"agent_name":"Neo","display_name":"Repo scan","goal":"Inspect failing tests across the renderer and status bar","status":"running"}`,
		Status:     "neutral",
	}, false)

	expandable, ok := item.(Expandable)
	if !ok {
		t.Fatalf("subagent item is not expandable")
	}
	expandable.ToggleExpanded()
	rendered := item.RawRender(100)
	for _, want := range []string{"Task: Repo scan", "Goal: Inspect failing tests across the renderer and status bar"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expanded render missing %q:\n%s", want, rendered)
		}
	}
}

func TestSubagentPreviewIncludesAgentTaskRuntimeStatusAndSummary(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:       "tool_1",
		Name:     "spawn_subagent",
		Input:    `{"name":"Repo scan","goal":"Inspect failing tests","runtime":"matrixclaw"}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool_1",
		Name:       "spawn_subagent",
		Content:    "Found one failing renderer test.",
		Metadata:   `{"agent_name":"Neo","display_name":"Repo scan","goal":"Inspect failing tests","runtime":"matrixclaw","status":"completed","summary":"Found one failing renderer test."}`,
		Status:     "success",
	}, false)

	handled, cmd := item.(KeyEventHandler).HandleKeyEvent(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if !handled || cmd == nil {
		t.Fatalf("HandleKeyEvent enter = (%v, %v), want preview command", handled, cmd)
	}
	msg, ok := cmd().(surfacedialog.ActionOpenFilePreview)
	if !ok {
		t.Fatalf("preview command returned %T, want ActionOpenFilePreview", cmd())
	}
	for _, want := range []string{"Name: Neo", "Task: Repo scan", "Goal: Inspect failing tests", "Runtime: matrixclaw", "Status: completed", "Found one failing renderer test."} {
		if !strings.Contains(msg.Data.Content, want) {
			t.Fatalf("preview content missing %q:\n%s", want, msg.Data.Content)
		}
	}
}

func TestOldSubagentMetadataWithoutAgentNameFallsBackToDisplayName(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:       "tool_1",
		Name:     "spawn_subagent",
		Input:    `{"name":"Repo scan","goal":"Inspect failing tests"}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool_1",
		Name:       "spawn_subagent",
		Metadata:   `{"display_name":"Repo scan","status":"running"}`,
		Status:     "neutral",
	}, false)

	rendered := item.RawRender(100)
	if !strings.Contains(rendered, "◇ Repo scan working...") {
		t.Fatalf("pending delegate render = %q", rendered)
	}
}

func TestSpawnSubagentCompletedRendersFinishedSummary(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:       "tool_1",
		Name:     "spawn_subagent",
		Input:    `{"name":"Repo scan","goal":"Inspect failing tests"}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool_1",
		Name:       "spawn_subagent",
		Content:    "Subagent Neo finished\n\nFound one failing renderer test.",
		Metadata:   `{"agent_name":"Neo","display_name":"Repo scan","goal":"Inspect failing tests","status":"completed","summary":"Found one failing renderer test."}`,
		Status:     "success",
	}, false)

	rendered := item.RawRender(100)
	for _, want := range []string{"✓ Neo completed", "Repo scan", "Found one failing renderer test."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("spawn completed render missing %q:\n%s", want, rendered)
		}
	}
}
