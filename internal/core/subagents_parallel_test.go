package core_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
				_ = os.Remove(filepath.Dir(path))
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

	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	first, second := waitPendingApproval(t, db, session.ID, "mutate-alpha"), waitPendingApproval(t, db, session.ID, "mutate-beta")
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusRunning)

	if _, err := app.ResolveApproval(context.Background(), first.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, first.RunID, core.RunStatusCompleted)
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusRunning)

	if _, err := app.ResolveApproval(context.Background(), second.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "the parent"); err != nil {
		t.Fatal(err)
	}
	starter.wait(t)
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
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
		task, err := taskOfCall(db, session.ID, run.ID, id)
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

// writingChildren runs a parent whose one reply starts two blocking children,
// alpha and beta, with args; each child starts, makes one edit and reports,
// telling on each step.
func writingChildren(t *testing.T, args string, dir string, on func(task string, step string, call tools.Call)) {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	editTool := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		on(strings.TrimPrefix(call.ToolCallID, "edit-"), "edits", call)
		return tools.Result{Content: "mutated"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), editTool)...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		answered := len(toolResults(request)) > 0
		switch task := childTask(request); {
		case task != "" && !answered:
			on(task, "starts", tools.Call{})
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "edit-" + task, Name: "mutate_state", Arguments: json.RawMessage(`{}`)}}}, nil
		case task != "":
			on(task, "reports", tools.Call{})
			return providers.Response{Text: "changed " + task}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"A","prompt":"alpha"` + args + `}`)},
				{ID: "call-b", Name: "agent", Arguments: json.RawMessage(`{"description":"B","prompt":"beta"` + args + `}`)},
			}}, nil
		default:
			return providers.Response{Text: "Parent done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "writers", core.RunStatusAccepted, false)
	sessionIn(t, db, session, dir, core.PermissionModeFullAuto)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if a, b := storedToolResult(t, db, session.ID, "call-a"), storedToolResult(t, db, session.ID, "call-b"); a != "changed alpha" || b != "changed beta" {
		t.Fatalf("results = %q, %q", a, b)
	}
}

func TestChildrenWritingTheParentsDirectoryTakeTurns(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var order []string
	betaStarted := make(chan struct{})
	writingChildren(t, ``, t.TempDir(), func(task string, step string, _ tools.Call) {
		mu.Lock()
		order = append(order, task+" "+step)
		mu.Unlock()
		switch {
		case task == "beta" && step == "starts":
			close(betaStarted)
		case task == "alpha" && step == "edits":
			// Give beta the chance to start alongside, which it must not take.
			select {
			case <-betaStarted:
			case <-time.After(200 * time.Millisecond):
			}
		}
	})

	if got := strings.Join(order, ", "); got != "alpha starts, alpha edits, alpha reports, beta starts, beta edits, beta reports" {
		t.Fatalf("order = %s", got)
	}
}

func TestWorktreeChildrenWorkAtOnce(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	var both sync.WaitGroup
	both.Add(2)
	together := make(chan struct{})
	go func() { both.Wait(); close(together) }()
	var mu sync.Mutex
	dirs := map[string]string{}
	alone := false
	writingChildren(t, `,"isolation":"worktree"`, repo, func(task string, step string, call tools.Call) {
		if step != "edits" {
			return
		}
		mu.Lock()
		dirs[task] = call.WorkingDir
		mu.Unlock()
		both.Done()
		select {
		case <-together:
		case <-time.After(5 * time.Second):
			mu.Lock()
			alone = true
			mu.Unlock()
		}
	})

	if alone {
		t.Fatal("a worktree child edited while the other waited")
	}
	if dirs["alpha"] == repo || dirs["beta"] == repo || dirs["alpha"] == dirs["beta"] {
		t.Fatalf("children edited in %v, the parent in %s", dirs, repo)
	}
}
