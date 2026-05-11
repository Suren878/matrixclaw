package chat

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestBashToolRendersAsRunWithoutInlineStatusDot(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &BashToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-bash-1",
			Name:     "bash",
			Input:    `{"command":"go test ./..."}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-bash-1",
			Name:       "bash",
			Content:    "ok",
			Metadata:   `{"output":"ok"}`,
		},
		Status: ToolStatusSuccess,
	}

	plain := xansi.Strip(renderer.RenderTool(&sty, 100, opts))
	if !strings.Contains(plain, "Run go test ./...") {
		t.Fatalf("expected bash to render as Run, got %q", plain)
	}
	if strings.Contains(plain, "● Run") {
		t.Fatalf("expected status marker to be rendered by the message wrapper only, got %q", plain)
	}
	if strings.Contains(plain, "Bash") {
		t.Fatalf("expected bash label to be hidden, got %q", plain)
	}
}

func TestBashToolMessageUsesOneStatusMarker(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg-1", surfacemessage.ToolCall{
		ID:       "tool-bash-1",
		Name:     "bash",
		Input:    `{"command":"go test ./..."}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool-bash-1",
		Name:       "bash",
		Content:    "ok",
		Metadata:   `{"output":"ok"}`,
	}, false)

	plain := xansi.Strip(item.Render(100))
	if strings.Count(plain, "●") != 1 {
		t.Fatalf("expected exactly one run marker, got %q", plain)
	}
	if !strings.Contains(plain, "● Run go test ./...") {
		t.Fatalf("expected run marker and label, got %q", plain)
	}
}

func TestBashToolMessageTreatsEmptyProcessProbeAsSuccess(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(&sty, "msg-1", surfacemessage.ToolCall{
		ID:       "tool-bash-1",
		Name:     "bash",
		Input:    `{"command":"ps aux | grep -E 'missing-process' | grep -v grep"}`,
		Finished: true,
	}, &surfacemessage.ToolResult{
		ToolCallID: "tool-bash-1",
		Name:       "bash",
		Content:    "",
		Metadata:   `{"output":"","exit_code":1}`,
		IsError:    true,
	}, false)

	if got := item.(*baseToolMessageItem).computeStatus(); got != ToolStatusSuccess {
		t.Fatalf("Status() = %v, want success for empty process probe", got)
	}
}
