package chat

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestExtractMessageItemsPreservesTextAroundTools(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	message := surfacemessage.Message{
		ID:   "msg-assistant",
		Role: surfacemessage.Assistant,
		Parts: []surfacemessage.ContentPart{
			surfacemessage.TextContent{Text: "Checking the folder."},
			surfacemessage.ToolCall{
				ID:       "tool-1",
				Name:     "bash",
				Input:    `{"command":"ls"}`,
				Finished: true,
			},
			surfacemessage.ToolResult{
				ToolCallID: "tool-1",
				Name:       "bash",
				Content:    "file.txt\n",
				Status:     "success",
			},
			surfacemessage.TextContent{Text: "The folder contains file.txt."},
		},
	}

	items := ExtractMessageItems(&styles, &message, BuildToolResultMap([]surfacemessage.Message{message}))
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want assistant, tool, assistant", len(items))
	}
	if _, ok := items[0].(*AssistantMessageItem); !ok {
		t.Fatalf("items[0] = %T, want AssistantMessageItem", items[0])
	}
	if _, ok := items[1].(ToolMessageItem); !ok {
		t.Fatalf("items[1] = %T, want ToolMessageItem", items[1])
	}
	if _, ok := items[2].(*AssistantMessageItem); !ok {
		t.Fatalf("items[2] = %T, want AssistantMessageItem", items[2])
	}

	first := xansi.Strip(items[0].Render(80))
	last := xansi.Strip(items[2].Render(80))
	if !strings.Contains(first, "Checking the folder.") {
		t.Fatalf("first assistant item = %q, want before-tool text", first)
	}
	if !strings.Contains(last, "The folder contains file.txt.") {
		t.Fatalf("last assistant item = %q, want after-tool text", last)
	}
}
