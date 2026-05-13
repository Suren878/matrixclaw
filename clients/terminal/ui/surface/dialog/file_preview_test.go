package dialog

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestWrapPreviewContentWrapsLongErrorLines(t *testing.T) {
	content := "ERROR provider rejected request because the requested model does not support reasoning_effort and the upstream response includes a long diagnostic line"

	rendered := wrapPreviewContent(content, 32)
	plain := ansi.Strip(rendered)

	if strings.Contains(plain, "…") {
		t.Fatalf("wrapped preview should not truncate with ellipsis: %q", plain)
	}
	if !strings.Contains(plain, "upstream response") {
		t.Fatalf("wrapped preview lost content: %q", plain)
	}
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 {
		t.Fatalf("wrapped preview stayed on one line: %q", plain)
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 32 {
			t.Fatalf("line width = %d, want <= 32 for line %q in %q", width, line, plain)
		}
	}
}
