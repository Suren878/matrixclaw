package common

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestFindPathTokenMatchesPreservesQuotes(t *testing.T) {
	content := `открой 'filename' и internal/api/server.go:12`

	got := findPathTokenMatches(content)

	if len(got) != 2 {
		t.Fatalf("expected 2 path token matches, got %d", len(got))
	}
	if token := content[got[0].start:got[0].end]; token != "filename" {
		t.Fatalf("first token = %q, want %q", token, "filename")
	}
	if token := content[got[1].start:got[1].end]; token != "internal/api/server.go" {
		t.Fatalf("second token = %q, want %q", token, "internal/api/server.go")
	}
}

func TestHighlightPlainPathsUsesGreenStyle(t *testing.T) {
	sty := styles.DefaultStyles()
	content := "смотри internal/api/server.go"

	got := HighlightPlainPaths(content, &sty)
	want := RenderANSIColoredText("internal/api/server.go", fileTokenColor(&sty))

	if !strings.Contains(got, want) {
		t.Fatalf("expected file-highlighted path, got %q", got)
	}
}

func TestRenderMessageTextUsesLegacyMarkdownRenderer(t *testing.T) {
	sty := styles.DefaultStyles()
	content := "список:\n- filename\n- internal/api/server.go"

	got := RenderMessageText(content, &sty, 120)

	if strings.Contains(got, "\x1b[38;2;0;255;178mfilename\x1b[0m") {
		t.Fatalf("expected legacy renderer to avoid assistant-only file token highlight, got %q", got)
	}
	if !strings.Contains(got, "filename") {
		t.Fatalf("expected markdown content to remain visible, got %q", got)
	}
}
