package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// textTool is a tool that answers with its own name.
func textTool(id string, effect tools.Effect) funcTool {
	return funcTool{spec: recoveryToolSpec(id, effect), fn: func(context.Context, tools.Call) (tools.Result, error) {
		return tools.Result{Content: id + " ran"}, nil
	}}
}

func toolNames(request providers.Request) []string {
	names := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestTypedChildrenGetTheirProfilesTools(t *testing.T) {
	t.Parallel()
	for agentType, want := range map[string]string{"explore": "bash,read", "verify": "bash,read", "general": "bash,read,write"} {
		t.Run(agentType, func(t *testing.T) {
			t.Parallel()
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			bash := textTool("bash", tools.EffectMutation)
			bash.spec.Category = tools.CategoryShell
			app.SetTools(tools.NewRegistry(append(core.AgentToolExecutors(app), textTool("read", tools.EffectReadOnly), textTool("write", tools.EffectMutation), bash)...))
			var mu sync.Mutex
			var seen []string
			var writeResult string
			app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				results := toolResults(request)
				switch task := childTask(request); {
				case task != "" && len(results) == 0:
					seen = toolNames(request)
					return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-write", Name: "write", Arguments: json.RawMessage(`{}`)}}}, nil
				case task != "":
					writeResult = results[len(results)-1]
					return providers.Response{Text: "child done"}, nil
				case len(results) == 0:
					return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child", Name: "agent", Arguments: json.RawMessage(`{"description":"Typed","prompt":"typed task","agent_type":"` + agentType + `"}`)}}}, nil
				default:
					return providers.Response{Text: "Parent done."}, nil
				}
			})})
			session, run := saveCrashRecoveryRun(t, db, "typed", core.RunStatusAccepted, false)
			sessionIn(t, db, session, t.TempDir(), core.PermissionModeFullAuto)

			if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
				t.Fatal(err)
			}

			mu.Lock()
			defer mu.Unlock()
			if got := strings.Join(seen, ","); got != want {
				t.Fatalf("child tools = %s, want %s", got, want)
			}
			if wrote := writeResult == "child-write=write ran"; wrote != (agentType == "general") {
				t.Fatalf("write as %s: %q", agentType, writeResult)
			}
			task, err := taskOfCall(db, session.ID, run.ID, "call-child")
			if err != nil || task.Agent.Name != agentType || task.Readonly != (agentType == "explore") {
				t.Fatalf("task = %+v, %v", task, err)
			}
		})
	}
}

func TestUnknownAgentTypeIsRefused(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	_, run := saveCrashRecoveryRun(t, db, "unknown_type", core.RunStatusAccepted, false)
	_, err := app.RunAgent(context.Background(), core.AgentInput{ParentSessionID: run.SessionID, ParentRunID: run.ID, ParentToolCallID: "call", Description: "x", Prompt: "x", AgentType: "wizard"})
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("err = %v, want invalid input", err)
	}
}

// offeredAgentTypes is the agent_type enum of the agent tool a parent run
// in dir is offered; replyOnce makes it answer a guest's reply-once message.
func offeredAgentTypes(t *testing.T, dir string, replyOnce bool) []string {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.SetTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	var request providers.Request
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, seen providers.Request) (providers.Response, error) {
		request = seen
		return providers.Response{Text: "Hi."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "offered", core.RunStatusAccepted, false)
	sessionIn(t, db, session, dir, core.PermissionModeDefault)
	if replyOnce {
		delivery := core.ClientDelivery{ID: "delivery_guest", Type: core.ClientDeliveryTypeRun, Client: "telegram", ExternalKey: "42", SessionID: session.ID, RunID: run.ID, Address: json.RawMessage(`{"kind":"inline","inline_message_id":"im"}`), ReplyOnce: true, Status: core.ClientDeliveryStatusPending}
		if err := db.CreateClientDelivery(context.Background(), delivery); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	for _, tool := range request.Tools {
		if tool.Name != "agent" {
			continue
		}
		var schema struct {
			Properties struct {
				AgentType struct {
					Enum []string `json:"enum"`
				} `json:"agent_type"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		return schema.Properties.AgentType.Enum
	}
	t.Fatal("no agent tool offered")
	return nil
}

func TestProjectAgentTypesAreOfferedToCodingRunsOnly(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".matrixclaw", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".matrixclaw", "agents", "db.md"), []byte("---\ndescription: database expert\n---\nYou know the schema."), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := offeredAgentTypes(t, repo, false); !slices.Contains(got, "db") {
		t.Fatalf("a coding run is offered %v, want the project's db type", got)
	}
	if got := offeredAgentTypes(t, repo, true); slices.Contains(got, "db") || !slices.Contains(got, "explore") {
		t.Fatalf("a guest's reply-once run is offered %v, want only the builtins", got)
	}
}

// schemaChild runs a parent whose child must answer with {"count": n} and
// replies with answers in turn; it returns the parent's tool result, the
// child's task and how many requests the child made.
func schemaChild(t *testing.T, answers ...string) (string, core.Task, int) {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.SetTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	var mu sync.Mutex
	childRequests := 0
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case childTask(request) != "":
			answer := answers[min(childRequests, len(answers)-1)]
			childRequests++
			return providers.Response{Text: answer}, nil
		case len(toolResults(request)) == 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child", Name: "agent", Arguments: json.RawMessage(`{"description":"Count","prompt":"count things","output_schema":{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}}`)}}}, nil
		default:
			return providers.Response{Text: "Parent done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "schema", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	task, err := taskOfCall(db, session.ID, run.ID, "call-child")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return storedToolResult(t, db, session.ID, "call-child"), task, childRequests
}

func TestOutputSchemaGetsOneCorrection(t *testing.T) {
	t.Parallel()
	result, task, requests := schemaChild(t, "About three.", "```json\n{\"count\": 3}\n```")
	if result != `{"count":3}` || task.Status != core.TaskStatusCompleted || requests != 2 {
		t.Fatalf("result = %q, task %s, %d child requests", result, task.Status, requests)
	}

	result, task, requests = schemaChild(t, "About three.")
	if !strings.HasPrefix(result, "About three.") || task.Status != core.TaskStatusFailed || requests != 2 {
		t.Fatalf("result = %q, task %s, %d child requests; want one correction, then a failed task", result, task.Status, requests)
	}
}

func TestAgentSteerReachesARunningBackgroundChild(t *testing.T) {
	t.Parallel()
	db, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	app, starter := executingCore(core.Config{Store: db})
	waiting, release := make(chan struct{}), make(chan struct{})
	wait := funcTool{spec: recoveryToolSpec("wait_here", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		close(waiting)
		<-release
		return tools.Result{Content: "waited"}, nil
	}}
	app.SetTools(tools.NewRegistry(append(core.AgentToolExecutors(app), wait)...))
	steered := make(chan bool, 1)
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		answered := len(toolResults(request)) > 0
		switch {
		case childTask(request) != "" && !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-wait", Name: "wait_here", Arguments: json.RawMessage(`{}`)}}}, nil
		case childTask(request) != "":
			found := slices.ContainsFunc(request.Messages, func(m providers.Message) bool { return strings.Contains(m.Content, "look in pkg/db first") })
			steered <- found
			return providers.Response{Text: "child done"}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-bg", Name: "agent", Arguments: json.RawMessage(`{"description":"Search","prompt":"search things","background":true}`)}}}, nil
		default:
			return providers.Response{Text: "Started."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "steer", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	task, err := taskOfCall(db, session.ID, run.ID, "call-bg")
	if err != nil {
		t.Fatal(err)
	}
	<-waiting

	if _, err := app.SteerSubagent(context.Background(), "session_other", task.ID, "hi"); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("steer from another session: %v", err)
	}
	if _, err := app.SteerSubagent(context.Background(), session.ID, task.ID, "look in pkg/db first"); err != nil {
		t.Fatal(err)
	}
	close(release)

	select {
	case found := <-steered:
		if !found {
			t.Fatal("the child's next request does not hold the steer")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the child never asked again")
	}
	starter.wait(t)
	if _, err := app.SteerSubagent(context.Background(), session.ID, task.ID, "too late"); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("steer after the end: %v", err)
	}
}

var taskIDPattern = regexp.MustCompile(`"task_id":"(task_[^"]+)"`)

// worktreeChildren runs a parent in repo that starts a worktree child per
// prompt, each writing <prompt>.txt, then makes the follow-up call next
// returns for each child result; it returns the core and store.
func worktreeChildren(t *testing.T, repo string, worktrees string, prompts []string, next func(prompt string, taskID string) *providers.ToolCall) (*core.Core, *store.SQLiteStore, core.Session) {
	t.Helper()
	db, cleanup := newCrashRecoveryStore(t)
	t.Cleanup(cleanup)
	app := core.New(core.Config{Store: db, Worktrees: worktrees})
	write := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		name := strings.TrimPrefix(call.ToolCallID, "edit-")
		return tools.Result{Content: "wrote"}, os.WriteFile(filepath.Join(call.WorkingDir, name+".txt"), []byte(name+"\n"), 0o644)
	}}
	app.SetTools(tools.NewRegistry(append(core.AgentToolExecutors(app), write)...))
	var mu sync.Mutex
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		results := toolResults(request)
		switch task := childTask(request); {
		case task != "" && len(results) == 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "edit-" + task, Name: "mutate_state", Arguments: json.RawMessage(`{}`)}}}, nil
		case task != "":
			return providers.Response{Text: "wrote " + task}, nil
		case len(results) == 0:
			var calls []providers.ToolCall
			for _, prompt := range prompts {
				calls = append(calls, providers.ToolCall{ID: "call-" + prompt, Name: "agent", Arguments: json.RawMessage(`{"description":"W","prompt":"` + prompt + `","isolation":"worktree"}`)})
			}
			return providers.Response{ToolCalls: calls}, nil
		case len(results) == len(prompts):
			var calls []providers.ToolCall
			for _, result := range results {
				callID, content, _ := strings.Cut(result, "=")
				match := taskIDPattern.FindStringSubmatch(content)
				if match == nil {
					continue
				}
				if call := next(strings.TrimPrefix(callID, "call-"), match[1]); call != nil {
					calls = append(calls, *call)
				}
			}
			if len(calls) > 0 {
				return providers.Response{ToolCalls: calls}, nil
			}
			return providers.Response{Text: "Parent done."}, nil
		default:
			return providers.Response{Text: "Parent done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "worktrees", core.RunStatusAccepted, false)
	sessionIn(t, db, session, repo, core.PermissionModeFullAuto)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	return app, db, session
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeChildrenAreMergedOrDiscarded(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktrees := t.TempDir()
	_, db, session := worktreeChildren(t, repo, worktrees, []string{"alpha", "beta"}, func(prompt string, taskID string) *providers.ToolCall {
		tool := map[string]string{"alpha": "worktree_merge", "beta": "worktree_discard"}[prompt]
		return &providers.ToolCall{ID: "follow-" + prompt, Name: tool, Arguments: json.RawMessage(`{"task_id":"` + taskID + `"}`)}
	})

	if got, err := os.ReadFile(filepath.Join(repo, "alpha.txt")); err != nil || string(got) != "alpha\n" {
		t.Fatalf("merged alpha.txt = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "beta.txt")); !os.IsNotExist(err) {
		t.Fatalf("discarded beta.txt is in the parent's tree: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "notes.txt")); string(got) != "uncommitted\n" {
		t.Fatalf("the parent's own change = %q", got)
	}
	if staged := gitOut(t, repo, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("the merge staged %q in the parent's index", staged)
	}
	if branches := gitOut(t, repo, "branch", "--list", "mc/*"); branches != "" {
		t.Fatalf("branches left: %q", branches)
	}
	if result := storedToolResult(t, db, session.ID, "follow-alpha"); !strings.HasPrefix(result, "Merged mc/task_") {
		t.Fatalf("merge result = %q", result)
	}
	entries, _ := filepath.Glob(filepath.Join(worktrees, "*", "task_*"))
	if len(entries) != 0 {
		t.Fatalf("worktrees left: %v", entries)
	}
}

func TestStaleWorktreesArePrunedAtStart(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	worktrees := t.TempDir()
	app, db, session := worktreeChildren(t, repo, worktrees, []string{"kept"}, func(string, string) *providers.ToolCall { return nil })
	tasks, err := db.ListTasks(context.Background(), core.TaskFilter{SessionID: session.ID, Kind: core.TaskKindSubagent})
	if err != nil || len(tasks) != 1 || tasks[0].WorktreeBase == "" {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}
	kept := tasks[0]
	orphan := filepath.Join(filepath.Dir(kept.WorkingDir), "task_orphan")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := app.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept.WorkingDir); err != nil {
		t.Fatalf("a fresh worktree was pruned: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the orphan worktree is left: %v", err)
	}

	later := core.New(core.Config{Store: db, Worktrees: worktrees, Clock: func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }})
	if err := later.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept.WorkingDir); !os.IsNotExist(err) {
		t.Fatalf("the stale worktree is left: %v", err)
	}
	if branches := gitOut(t, repo, "branch", "--list", "mc/*"); branches != "" {
		t.Fatalf("branches left: %q", branches)
	}
	if task, err := db.GetTask(context.Background(), kept.ID); err != nil || task.WorktreeBase != "" {
		t.Fatalf("task = %+v, %v", task, err)
	}
	if worktree := gitOut(t, repo, "worktree", "list", "--porcelain"); strings.Contains(worktree, worktrees) {
		t.Fatalf("git still lists %s", worktree)
	}
}

func TestWorktreeMergeReportsConflictsAndKeepsTheBranch(t *testing.T) {
	t.Parallel()
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, repo, "add", "shared.txt")
	gitOut(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "shared")
	_, db, session := worktreeChildren(t, repo, t.TempDir(), []string{"shared"}, func(_ string, taskID string) *providers.ToolCall {
		if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("parent\n"), 0o644); err != nil {
			t.Error(err)
		}
		return &providers.ToolCall{ID: "follow-merge", Name: "worktree_merge", Arguments: json.RawMessage(`{"task_id":"` + taskID + `"}`)}
	})

	if result := storedToolResult(t, db, session.ID, "follow-merge"); !strings.Contains(result, "conflicts") || !strings.Contains(result, "shared.txt") {
		t.Fatalf("merge result = %q", result)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "shared.txt")); !strings.Contains(string(got), "<<<<<<<") {
		t.Fatalf("shared.txt = %q, want conflict markers", got)
	}
	if branches := gitOut(t, repo, "branch", "--list", "mc/*"); branches == "" {
		t.Fatal("the branch of a conflicted merge was removed")
	}
	if staged := gitOut(t, repo, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("the merge staged %q in the parent's index", staged)
	}
}
