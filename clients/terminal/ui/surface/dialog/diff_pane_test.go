package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func drawDialog(d Dialog) string {
	canvas := uv.NewScreenBuffer(140, 40)
	d.Draw(canvas, canvas.Bounds())
	return ansi.Strip(canvas.Render())
}

// splitShown reports whether the old and new line of a one-line edit sit side by side.
func splitShown(screen string) bool {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, "n := 1") && strings.Contains(line, "n := 2") {
			return true
		}
	}
	return false
}

func TestDiffDialogsToggleBetweenUnifiedAndSplit(t *testing.T) {
	com := surfacecommon.DefaultCommon()
	dialogs := map[string]Dialog{
		"permission": NewPermissions(com, surfacepermission.PermissionRequest{ID: "a1", ToolName: "edit", Params: tools.FileChange{Path: "parse.go", OldContent: "n := 1\n", NewContent: "n := 2\n"}}),
		"preview":    NewDiffPreview(com, DiffPreviewData{Title: "Edit Changes", FilePath: "parse.go", OldContent: "n := 1\n", NewContent: "n := 2\n", Additions: 1, Removals: 1}),
	}
	for name, d := range dialogs {
		if screen := drawDialog(d); !strings.Contains(screen, "n := 2") || splitShown(screen) {
			t.Fatalf("%s: unified diff expected first:\n%s", name, screen)
		}
		d.HandleMsg(tea.KeyPressMsg{Code: 't', Text: "t"})
		if screen := drawDialog(d); !splitShown(screen) {
			t.Fatalf("%s: t did not split the diff:\n%s", name, screen)
		}
	}
}
