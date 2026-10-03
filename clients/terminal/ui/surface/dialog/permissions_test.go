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

func TestPermissionsDialogKeepsAlwaysAllowOnlyAfterAConfirmingEnter(t *testing.T) {
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	suggested := surfacepermission.PermissionRequest{ID: "approval_1", ToolName: "bash", Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}}
	for key, want := range map[rune]PermissionAction{'s': PermissionAlwaysSession, 'g': PermissionAlwaysGlobal} {
		p := NewPermissions(surfacecommon.DefaultCommon(), suggested)
		if action := p.HandleMsg(tea.KeyPressMsg{Code: key, Text: string(key)}); action != nil {
			t.Fatalf("%c alone answered: %#v", key, action)
		}
		if response, ok := p.HandleMsg(enter).(ActionPermissionResponse); !ok || response.Action != want {
			t.Fatalf("%c, enter: want %s", key, want)
		}
	}
	plain := surfacepermission.PermissionRequest{ID: "approval_2", ToolName: "bash"}
	p := NewPermissions(surfacecommon.DefaultCommon(), plain)
	p.HandleMsg(tea.KeyPressMsg{Code: 's', Text: "s"})
	if response, ok := p.HandleMsg(enter).(ActionPermissionResponse); !ok || response.Action != PermissionDeny {
		t.Fatal("s, enter without a suggested rule did not deny")
	}
}

func TestPermissionsDialogEnterDeniesUntilAnotherChoiceIsMade(t *testing.T) {
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	request := surfacepermission.PermissionRequest{ID: "approval_1", ToolName: "bash", Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "rm *"}}
	if response, ok := NewPermissions(surfacecommon.DefaultCommon(), request).HandleMsg(enter).(ActionPermissionResponse); !ok || response.Action != PermissionDeny {
		t.Fatal("enter did not deny")
	}
	if response, ok := NewPermissions(surfacecommon.DefaultCommon(), request).HandleMsg(tea.KeyPressMsg{Code: 'a', Text: "a"}).(ActionPermissionResponse); !ok || response.Action != PermissionAllow {
		t.Fatal("a did not allow once")
	}
	p := NewPermissions(surfacecommon.DefaultCommon(), request)
	for range 3 {
		p.HandleMsg(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if response, ok := p.HandleMsg(enter).(ActionPermissionResponse); !ok || response.Action != PermissionAllow {
		t.Fatal("three steps left and enter did not allow")
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
