package core_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ReadsOnly lets the fake bash pass plan mode's read-only check like the real one.
func (t *commandTool) ReadsOnly(call tools.Call) bool {
	return tools.CheckReadOnlyCall(call) == nil
}

func blocked(result core.ExecuteToolResult) bool {
	return result.Approval == nil && result.ToolResultMessage != nil && result.ToolResultMessage.Parts[0].ToolResult.IsError()
}

func TestPlanModeBlocksChangesAndLetsReadOnlyCallsRun(t *testing.T) {
	t.Parallel()
	app, db, bash, dir := permissionCore(t, core.Config{})
	plan := permissionSession(t, db, "session_plan", dir, core.PermissionModePlan, "")
	saveRule(t, db, "rule_bash", permission.Rule{Tool: "bash", Effect: permission.Allow, SessionID: plan.ID})

	for _, call := range []struct{ tool, args string }{
		{"write", `{"file_path":"new.txt","content":"x"}`},
		{"edit", `{"file_path":"notes.txt","old_string":"public","new_string":"private"}`},
		{"apply_patch", `{"input":"*** Begin Patch\n*** Add File: new.txt\n+x\n*** End Patch"}`},
		{"bash", `{"command":"go test ./..."}`},
		{"bash", `{"command":"git log > log.txt"}`},
	} {
		if result := executeTool(t, app, plan.ID, call.tool, call.args); !blocked(result) {
			t.Fatalf("%s %s ran in plan mode: %+v", call.tool, call.args, result)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("plan mode wrote a file: %v", err)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("plan mode ran %v", bash.ran)
	}

	for _, call := range []struct{ tool, args string }{
		{"read", `{"file_path":"notes.txt"}`},
		{"grep", `{"pattern":"public"}`},
		{"bash", `{"command":"git log --oneline"}`},
	} {
		result := executeTool(t, app, plan.ID, call.tool, call.args)
		if result.Approval != nil || result.ToolResultMessage == nil || result.ToolResultMessage.Parts[0].ToolResult.IsError() {
			t.Fatalf("%s %s in plan mode: %+v", call.tool, call.args, result)
		}
	}
	if !slices.Equal(bash.ran, []string{"git log --oneline"}) {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestApplyPatchMeetsEditRulesForEveryFile(t *testing.T) {
	t.Parallel()
	app, db, _, dir := permissionCore(t, core.Config{})
	edits := permissionSession(t, db, "session_patch", dir, core.PermissionModeAcceptEdits, "")
	inside := `{"input":"*** Begin Patch\n*** Add File: a.txt\n+a\n*** Add File: sub/b.txt\n+b\n*** End Patch"}`
	if result := executeTool(t, app, edits.ID, "apply_patch", inside); result.Approval != nil || blocked(result) {
		t.Fatalf("patch inside the working directory: %+v", result)
	}
	outside := filepath.Join(t.TempDir(), "c.txt")
	mixed := `{"input":"*** Begin Patch\n*** Add File: d.txt\n+d\n*** Add File: ` + outside + `\n+c\n*** End Patch"}`
	if result := executeTool(t, app, edits.ID, "apply_patch", mixed); result.Approval == nil {
		t.Fatalf("patch reaching outside the working directory ran without approval: %+v", result)
	}
	saveRule(t, db, "rule_deny_sub", permission.Rule{Tool: "edit", Pattern: filepath.Join(dir, "sub") + "/**", Effect: permission.Deny, SessionID: edits.ID})
	denied := `{"input":"*** Begin Patch\n*** Add File: e.txt\n+e\n*** Add File: sub/f.txt\n+f\n*** End Patch"}`
	if result := executeTool(t, app, edits.ID, "apply_patch", denied); !blocked(result) {
		t.Fatalf("an edit deny rule on one file did not block the patch: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "e.txt")); !os.IsNotExist(err) {
		t.Fatalf("a blocked patch wrote a file: %v", err)
	}
}

func TestApprovingThePlanSwitchesToAcceptEditsAndTheRunGoesOn(t *testing.T) {
	t.Parallel()
	app, db, _, dir := permissionCore(t, core.Config{RunStarter: &recordingRunStarter{}})
	app.SetTools(tools.NewRegistry(append(tools.CoreExecutors(), core.PlanModeToolExecutors(app)...)...))
	var offered [][]string
	calls := 0
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		var names []string
		for _, tool := range request.Tools {
			names = append(names, tool.Name)
		}
		offered = append(offered, names)
		calls++
		switch calls {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-plan-1", Name: "exit_plan_mode", Arguments: []byte(`{"plan":"1. Write notes."}`)}}}, nil
		case 2:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-plan-2", Name: "exit_plan_mode", Arguments: []byte(`{"plan":"1. Write plan.txt."}`)}}}, nil
		case 3:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-write", Name: "write", Arguments: []byte(`{"file_path":"plan.txt","content":"done"}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	session := permissionSession(t, db, "session_planrun", dir, core.PermissionModePlan, "")
	run := acceptRun(t, db, session, "planrun")
	ctx := context.Background()

	decide := func(decision core.ApprovalResolveRequest) {
		t.Helper()
		if err := app.ExecuteRun(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
		assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
		approvals, err := db.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
		if err != nil || len(approvals) != 1 || approvals[0].ToolName != "exit_plan_mode" || approvals[0].Suggestion != nil {
			t.Fatalf("pending approvals = %+v err = %v", approvals, err)
		}
		if _, err := app.ResolveApproval(ctx, approvals[0].ID, decision); err != nil {
			t.Fatal(err)
		}
	}
	decide(core.ApprovalResolveRequest{Reason: "write plan.txt instead"})
	if mode := sessionMode(t, db, session.ID); mode != core.PermissionModePlan {
		t.Fatalf("a rejected plan changed the mode to %s", mode)
	}
	decide(core.ApprovalResolveRequest{Approved: true})
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if mode := sessionMode(t, db, session.ID); mode != core.PermissionModeAcceptEdits {
		t.Fatalf("mode after approval = %s", mode)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "plan.txt")); err != nil || string(got) != "done" {
		t.Fatalf("plan.txt = %q, %v", got, err)
	}
	if !slices.Contains(offered[0], "exit_plan_mode") {
		t.Fatalf("plan mode did not offer exit_plan_mode: %v", offered[0])
	}
}

func acceptRun(t *testing.T, db *store.SQLiteStore, session core.Session, suffix string) core.Run {
	t.Helper()
	now := runRecoveryTestTime()
	run := core.Run{ID: "run_" + suffix, SessionID: session.ID, UserMessageID: "msg_user_" + suffix, Status: core.RunStatusAccepted, StartedAt: now, UpdatedAt: now}
	user := transcript.Message{
		ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser,
		Content: "task " + suffix, Parts: transcript.NormalizeMessageParts("task "+suffix, nil), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.AcceptMessage(context.Background(), user, run); err != nil {
		t.Fatal(err)
	}
	return run
}

func sessionMode(t *testing.T, db *store.SQLiteStore, id string) core.PermissionMode {
	t.Helper()
	session, err := db.GetSession(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return session.PermissionMode
}

func TestModelsTrainedOnApplyPatchGetItInsteadOfEdit(t *testing.T) {
	t.Parallel()
	for model, want := range map[string][]string{"gpt-5.1-codex": {"apply_patch"}, "claude-sonnet-4-5": {"edit", "multiedit"}} {
		app, db, _, dir := permissionCore(t, core.Config{})
		var offered []string
		app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
			for _, tool := range request.Tools {
				offered = append(offered, tool.Name)
			}
			return providers.Response{Text: "Done."}, nil
		})})
		session := permissionSession(t, db, "session_model", dir, core.PermissionModeDefault, "")
		session.ModelID = model
		if err := db.UpdateSession(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		run := acceptRun(t, db, session, "model")
		if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
			t.Fatal(err)
		}
		var edits []string
		for _, name := range offered {
			if name == "edit" || name == "multiedit" || name == "apply_patch" {
				edits = append(edits, name)
			}
		}
		if !slices.Equal(edits, want) {
			t.Errorf("%s: edit tools = %v, want %v", model, edits, want)
		}
	}
}
