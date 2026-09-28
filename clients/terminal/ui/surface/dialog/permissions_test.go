package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/permission"
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
