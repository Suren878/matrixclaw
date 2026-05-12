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

func TestPermissionsHandleMouseClickAllowsPermission(t *testing.T) {
	t.Parallel()

	com := surfacecommon.DefaultCommon()
	perm := surfacepermission.PermissionRequest{
		ID:          "perm-1",
		SessionID:   "sess-1",
		ToolCallID:  "tool-1",
		ToolName:    toolNameBash,
		Description: "Execute command: echo hello",
		Action:      "execute",
		Params: tools.BashPermissionsParams{
			Command:     "echo hello",
			WorkingDir:  "/workspace/matrixclaw",
			Description: "Execute command: echo hello",
		},
		Path: "/workspace/matrixclaw",
	}

	p := NewPermissions(com, perm)
	scr := uv.NewScreenBuffer(120, 40)
	p.Draw(scr, scr.Bounds())

	lines := strings.Split(p.lastView, "\n")
	lineIdx := -1
	labelIdx := -1
	for i, line := range lines {
		plain := ansi.Strip(line)
		if idx := strings.Index(plain, "Allow"); idx >= 0 {
			lineIdx = i
			labelIdx = idx
			break
		}
	}
	if lineIdx < 0 {
		t.Fatal("could not find allow button in rendered dialog")
	}

	msg := tea.MouseClickMsg{
		X:      p.lastViewRect.Min.X + labelIdx + 1,
		Y:      p.lastViewRect.Min.Y + lineIdx,
		Button: tea.MouseLeft,
	}

	action := p.HandleMsg(msg)
	resp, ok := action.(ActionPermissionResponse)
	if !ok {
		t.Fatalf("expected ActionPermissionResponse, got %T", action)
	}
	if resp.Action != PermissionAllow {
		t.Fatalf("expected allow action, got %q", resp.Action)
	}
	if resp.Permission.ID != perm.ID {
		t.Fatalf("unexpected permission ID: %q", resp.Permission.ID)
	}
}

func TestPermissionsDiffDefaultsToUnifiedView(t *testing.T) {
	t.Parallel()

	com := surfacecommon.DefaultCommon()
	perm := surfacepermission.PermissionRequest{
		ID:       "perm-1",
		ToolName: toolNameWrite,
		Path:     "notes.txt",
		Params: tools.WritePermissionsParams{
			FilePath:   "notes.txt",
			OldContent: "old\n",
			NewContent: "new\n",
		},
	}

	p := NewPermissions(com, perm)
	scr := uv.NewScreenBuffer(180, 50)
	p.Draw(scr, scr.Bounds())

	if p.isSplitMode() {
		t.Fatal("expected unified diff to be the default view")
	}
}

func TestPermissionsAllowSessionOnlyForEdits(t *testing.T) {
	t.Parallel()

	com := surfacecommon.DefaultCommon()
	edit := NewPermissions(com, surfacepermission.PermissionRequest{
		ID:       "perm-edit",
		ToolName: toolNameWrite,
		Params: tools.WritePermissionsParams{
			FilesystemPathMetadata: tools.FilesystemPathMetadata{WithinWorkingDir: true},
		},
	})
	if !edit.canAllowSession() {
		t.Fatal("write permission should allow session auto-edits")
	}

	bash := NewPermissions(com, surfacepermission.PermissionRequest{
		ID:       "perm-bash",
		ToolName: toolNameBash,
	})
	if bash.canAllowSession() {
		t.Fatal("bash permission should not allow session auto-edits")
	}

	outsideEdit := NewPermissions(com, surfacepermission.PermissionRequest{
		ID:       "perm-outside-edit",
		ToolName: toolNameWrite,
		Params: tools.WritePermissionsParams{
			FilesystemPathMetadata: tools.FilesystemPathMetadata{WithinWorkingDir: false},
		},
	})
	if outsideEdit.canAllowSession() {
		t.Fatal("outside-root write permission should not allow session auto-edits")
	}
}

func TestPermissionsRenderBashAsRun(t *testing.T) {
	t.Parallel()

	com := surfacecommon.DefaultCommon()
	perm := surfacepermission.PermissionRequest{
		ID:       "perm-run",
		ToolName: toolNameBash,
		Path:     "/workspace/matrixclaw",
		Params: tools.BashPermissionsParams{
			Command:    "go test ./...",
			WorkingDir: "/workspace/matrixclaw",
		},
	}

	p := NewPermissions(com, perm)
	scr := uv.NewScreenBuffer(120, 40)
	p.Draw(scr, scr.Bounds())

	plain := ansi.Strip(p.lastView)
	if !strings.Contains(plain, "Tool Run") {
		t.Fatalf("expected permission tool label to render as Run, got %q", plain)
	}
	if strings.Contains(plain, "Tool bash") {
		t.Fatalf("expected bash implementation name to be hidden, got %q", plain)
	}
}

func TestDiffPreviewDefaultsToUnifiedView(t *testing.T) {
	t.Parallel()

	com := surfacecommon.DefaultCommon()
	d := NewDiffPreview(com, DiffPreviewData{
		FilePath:   "notes.txt",
		OldContent: "old\n",
		NewContent: "new\n",
	})
	scr := uv.NewScreenBuffer(180, 50)
	d.Draw(scr, scr.Bounds())

	if d.isSplitMode() {
		t.Fatal("expected unified diff to be the default view")
	}
}
