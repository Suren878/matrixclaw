package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestAgentCallRendersAsASubagentCard(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	call := surfacemessage.ToolCall{ID: "call_1", Name: "agent", Input: `{"description":"Review the parser","prompt":"review the parser for bugs","readonly":true}`, Finished: true}
	result := &surfacemessage.ToolResult{ToolCallID: "call_1", Name: "agent", Content: "No bugs found.", Metadata: `{"agent_name":"Neo","description":"Review the parser","status":"completed","summary":"No bugs found."}`}

	rendered := ansi.Strip(NewToolMessageItem(&sty, call, result, false).RawRender(120))

	for _, want := range []string{"Neo completed", "Review the parser", "No bugs found."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("card lacks %q:\n%s", want, rendered)
		}
	}
}
