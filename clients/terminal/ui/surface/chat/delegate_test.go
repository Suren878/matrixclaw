package chat

import (
	"strings"
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestDelegateTaskPendingRendersSubagentWorkingLabel(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg_1", surfacemessage.ToolCall{
		ID:    "tool_1",
		Name:  "delegate_task",
		Input: `{"goal":"Inspect failing tests","runtime":"claude"}`,
	}, nil, false)

	rendered := item.RawRender(100)
	if !strings.Contains(rendered, "Subagent Claude Code is working") {
		t.Fatalf("pending delegate render = %q", rendered)
	}
}

func TestDelegateTaskCompletedRendersRuntimeGoalAndSummary(t *testing.T) {
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
		Status:     "success",
	}, false)

	rendered := item.RawRender(100)
	for _, want := range []string{"Subagent Codex", "Inspect failing tests", "Found one failing renderer test."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("completed delegate render missing %q:\n%s", want, rendered)
		}
	}
}
