package core_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// gitRepo is a git repository with one commit; the worktrees made from it are
// removed after the test.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "init")
	t.Cleanup(func() {
		for _, line := range strings.Split(git("worktree", "list", "--porcelain"), "\n") {
			if path, ok := strings.CutPrefix(line, "worktree "); ok && path != dir {
				_ = os.RemoveAll(path)
			}
		}
	})
	return dir
}

// childTask is the word after "Delegated task:" in a child's request.
func childTask(request providers.Request) string {
	if fields := strings.Fields(childPrompt(request)); len(fields) > 2 {
		return fields[2]
	}
	return ""
}

func TestDecidingOneOfTwoAskingChildrenLeavesTheParentWaiting(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), mutate)...))
	var mu sync.Mutex
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		answered := len(toolResults(request)) > 0
		switch task := childTask(request); {
		case task != "" && !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "mutate-" + task, Name: "mutate_state", Arguments: json.RawMessage(`{}`)}}}, nil
		case task != "":
			return providers.Response{Text: "changed " + task}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"A","prompt":"alpha","isolation":"worktree"}`)},
				{ID: "call-b", Name: "agent", Arguments: json.RawMessage(`{"description":"B","prompt":"beta","isolation":"worktree"}`)},
			}}, nil
		default:
			return providers.Response{Text: "Parent done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "two_asking", core.RunStatusAccepted, false)
	sessionIn(t, db, session, gitRepo(t), core.PermissionModeDefault)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	first, second := pendingApprovalFor(t, db, session.ID, "call-a"), pendingApprovalFor(t, db, session.ID, "call-b")

	if _, err := app.ResolveApproval(context.Background(), first.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	starter.wait(t)
	if got := starter.count(run.ID); got != 0 {
		t.Fatalf("parent started %d times while its other child still asks", got)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)

	if _, err := app.ResolveApproval(context.Background(), second.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, core.RunStatusCompleted)
	starter.wait(t)
	if a, b := storedToolResult(t, db, session.ID, "call-a"), storedToolResult(t, db, session.ID, "call-b"); a != "changed alpha" || b != "changed beta" {
		t.Fatalf("results = %q, %q", a, b)
	}
}

func TestBackgroundChildrenInTheParentsDirectoryRunTogetherAndTakeTurnsPerEdit(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	firstEditing, bothAsked, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	editing, edits, asked := 0, 0, 0
	overlapped := false
	edit := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(context.Context, tools.Call) (tools.Result, error) {
		mu.Lock()
		editing++
		edits++
		overlapped = overlapped || editing > 1
		first := edits == 1
		mu.Unlock()
		if first {
			close(firstEditing)
			<-release
		}
		mu.Lock()
		editing--
		mu.Unlock()
		return tools.Result{Content: "mutated"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), edit)...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		answered := len(toolResults(request)) > 0
		switch task := childTask(request); {
		case task != "" && !answered:
			mu.Lock()
			if asked++; asked == 2 {
				close(bothAsked)
			}
			mu.Unlock()
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "edit-" + task, Name: "mutate_state", Arguments: json.RawMessage(`{}`)}}}, nil
		case task != "":
			return providers.Response{Text: "changed " + task}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"A","prompt":"alpha","background":true}`)},
				{ID: "call-b", Name: "agent", Arguments: json.RawMessage(`{"description":"B","prompt":"beta","background":true}`)},
			}}, nil
		default:
			return providers.Response{Text: "Started."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "background_writers", core.RunStatusAccepted, false)
	sessionIn(t, db, session, t.TempDir(), core.PermissionModeFullAuto)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	waitRecoverySignal(t, bothAsked, "both children at work")
	waitRecoverySignal(t, firstEditing, "the first edit")
	close(release)

	for _, id := range []string{"call-a", "call-b"} {
		task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		waitRunStatus(t, db, task.ChildRunID, core.RunStatusCompleted)
	}
	starter.wait(t)
	mu.Lock()
	defer mu.Unlock()
	if overlapped {
		t.Fatal("two children edited the directory at once")
	}
}
