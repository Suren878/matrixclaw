package dialog

import (
	"strings"
	"testing"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
)

func TestPermissionSourceLabelAgent(t *testing.T) {
	permission := surfacepermission.PermissionRequest{
		ToolName: "bash",
		Params: map[string]any{
			"command": "echo hi",
		},
	}

	if got := permissionSourceLabel(permission); got != "Agent" {
		t.Fatalf("permissionSourceLabel() = %q, want Agent", got)
	}
}

func TestPermissionSourceLabelSubagent(t *testing.T) {
	permission := surfacepermission.PermissionRequest{
		ToolName: "delegate_task",
		Params: map[string]any{
			"source":         "subagent_approval_bridge",
			"subagent_title": "Subagent: Inspect repo",
			"runtime":        "MatrixClaw",
		},
	}

	if got := permissionSourceLabel(permission); got != "Subagent: Inspect repo" {
		t.Fatalf("permissionSourceLabel() = %q, want subagent title", got)
	}
}

func TestPermissionsHeaderRendersSource(t *testing.T) {
	permission := surfacepermission.PermissionRequest{
		ToolName: "delegate_task",
		Path:     "/tmp/project",
		Params: map[string]any{
			"source":  "subagent_approval_bridge",
			"runtime": "MatrixClaw",
		},
	}
	dialog := NewPermissions(surfacecommon.DefaultCommon(), permission)

	header := dialog.renderHeader(80)
	if !strings.Contains(header, "Source") || !strings.Contains(header, "Subagent: MatrixClaw") {
		t.Fatalf("header = %q, want subagent source", header)
	}
}
