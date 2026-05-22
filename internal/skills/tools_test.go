package skills

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestSkillManageCreateApprovalIncludesReadableDraftParams(t *testing.T) {
	input := tools.SkillManagePermissionsParams{
		Action:      "create",
		Name:        "Matrix UI",
		Description: "Helps edit Matrix UI consistently",
		Content:     "Use existing pickers.\nBack returns to the previous screen.",
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	result, err := (&manageTool{}).Execute(context.Background(), tools.Call{Args: raw})
	if err != nil {
		t.Fatal(err)
	}
	if result.Approval == nil {
		t.Fatalf("Approval = nil, result = %#v", result)
	}
	if result.Approval.Description != "Create quarantined skill: Matrix UI" {
		t.Fatalf("Description = %q", result.Approval.Description)
	}
	if result.Approval.Path != "skills/matrix-ui/SKILL.md" {
		t.Fatalf("Path = %q", result.Approval.Path)
	}
	params, ok := result.Approval.Params.(tools.SkillManagePermissionsParams)
	if !ok {
		t.Fatalf("Params type = %T", result.Approval.Params)
	}
	if params.Content != input.Content || params.Description != input.Description {
		t.Fatalf("Params = %#v", params)
	}
}
