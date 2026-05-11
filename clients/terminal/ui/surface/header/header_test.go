package header

import (
	"strings"
	"testing"

	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestHeaderViewShowsCompactChrome(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	header := New(&styles, "")

	view := header.View(120, true, Data{
		UsageText: "Context: ~42 tokens",
	})

	if !strings.Contains(view, "matrixclaw v0.1.0") {
		t.Fatalf("view does not contain product title: %q", view)
	}
	if strings.Contains(view, "ctrl+d") {
		t.Fatalf("view contains removed details shortcut: %q", view)
	}
	if !strings.Contains(view, "Context: ~42 tokens") {
		t.Fatalf("view does not contain context usage: %q", view)
	}
}

func TestHeaderViewDoesNotFallbackToProcessWorkingDir(t *testing.T) {
	styles := surfacestyles.DefaultStyles()
	header := New(&styles, "")

	view := header.View(120, true, Data{})

	if strings.Contains(view, "/") {
		t.Fatalf("view unexpectedly contains a filesystem path: %q", view)
	}
	if strings.Contains(view, "ctrl+d") {
		t.Fatalf("view contains removed details shortcut: %q", view)
	}
}
