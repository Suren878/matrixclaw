package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestPermissionsDialogOffersDenyWithReason(t *testing.T) {
	request := surfacepermission.PermissionRequest{ID: "approval_1", ToolName: "bash"}
	p := NewPermissions(surfacecommon.DefaultCommon(), request)

	action := p.HandleMsg(tea.KeyPressMsg{Code: 'r', Text: "r"})

	response, ok := action.(ActionPermissionResponse)
	if !ok || response.Action != PermissionDenyWithReason || response.Permission.ID != "approval_1" {
		t.Fatalf("action = %#v", action)
	}
}

func TestPermissionsDialogOffersAlwaysAllowOnlyWithASuggestedRule(t *testing.T) {
	suggested := surfacepermission.PermissionRequest{ID: "approval_1", ToolName: "bash", Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}}
	for key, want := range map[rune]PermissionAction{'s': PermissionAlwaysSession, 'g': PermissionAlwaysGlobal} {
		action := NewPermissions(surfacecommon.DefaultCommon(), suggested).HandleMsg(tea.KeyPressMsg{Code: key, Text: string(key)})
		if response, ok := action.(ActionPermissionResponse); !ok || response.Action != want {
			t.Fatalf("%c: action = %#v, want %s", key, action, want)
		}
	}
	plain := surfacepermission.PermissionRequest{ID: "approval_2", ToolName: "bash"}
	if action := NewPermissions(surfacecommon.DefaultCommon(), plain).HandleMsg(tea.KeyPressMsg{Code: 's', Text: "s"}); action != nil {
		t.Fatalf("always allow without a suggested rule: %#v", action)
	}
}

func TestPermissionsDialogNamesTheSubagentThatAsks(t *testing.T) {
	for request, want := range map[surfacepermission.PermissionRequest]string{
		{ID: "approval_1", ToolName: "bash"}:                   "Agent",
		{ID: "approval_2", ToolName: "bash", AgentName: "Neo"}: "Subagent: Neo",
	} {
		if got := permissionSourceLabel(request); got != want {
			t.Fatalf("source of %+v = %q, want %q", request, got, want)
		}
	}
}

func TestPermissionsDialogAnswersClicksOnItsButtonsOnly(t *testing.T) {
	request := surfacepermission.PermissionRequest{ID: "a1", ToolName: "bash", Params: tools.BashParams{Command: "echo Allow-list updated"}}
	p := NewPermissions(surfacecommon.DefaultCommon(), request)
	scr := uv.NewScreenBuffer(100, 30)
	p.Draw(scr, scr.Bounds())
	click := func(line int, text string) Action {
		x := strings.Index(ansi.Strip(strings.Split(p.lastView, "\n")[line]), text)
		return p.HandleMsg(tea.MouseClickMsg{X: p.lastViewRect.Min.X + x + 1, Y: p.lastViewRect.Min.Y + line, Button: tea.MouseLeft})
	}

	commandLine, buttonsLine := -1, -1
	for y, line := range strings.Split(ansi.Strip(p.lastView), "\n") {
		switch {
		case strings.Contains(line, "echo Allow-list"):
			commandLine = y
		case strings.Contains(line, "Deny with reason"):
			buttonsLine = y
		}
	}
	if commandLine < 0 || buttonsLine < 0 {
		t.Fatalf("command or buttons not drawn:\n%s", ansi.Strip(p.lastView))
	}
	if action := click(commandLine, "Allow-list"); action != nil {
		t.Fatalf("a click on the command answered: %#v", action)
	}
	if response, ok := click(buttonsLine, "Allow").(ActionPermissionResponse); !ok || response.Action != PermissionAllow {
		t.Fatal("a click on the Allow button did not allow")
	}
}
