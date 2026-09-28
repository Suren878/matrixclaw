package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
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
