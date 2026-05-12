package core

import "testing"

func TestPermissionModeForSessionApprovalUsesToolManifest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tool string
		want bool
	}{
		{name: "write", tool: "write", want: true},
		{name: "legacy multi edit alias", tool: "multi_edit", want: true},
		{name: "bash", tool: "bash", want: false},
		{name: "unknown", tool: "unknown", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mode, ok := PermissionModeForSessionApproval(tc.tool)
			if ok != tc.want {
				t.Fatalf("PermissionModeForSessionApproval(%q) ok = %v, want %v", tc.tool, ok, tc.want)
			}
			if tc.want && mode != PermissionModeAcceptEdits {
				t.Fatalf("PermissionModeForSessionApproval(%q) mode = %q, want %q", tc.tool, mode, PermissionModeAcceptEdits)
			}
		})
	}
}
