# Stage 6c — The Unified `agent` Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One tool, `agent{description, prompt, background, isolation: shared|worktree, readonly, runtime, model}`, replaces `delegate_task`, `spawn_subagent`, `list_subagents` and `read_subagent_result`: read-only children get read-only tools and run in parallel, children writing the parent's directory run one at a time, worktree children run in parallel, background children are subagent tasks whose results arrive as events (at most 4 per session, configurable); a blocking child's wall-clock is not charged to the parent's budget; canceling a run also stops its background commands, and a child's background commands stop when it finishes; the 24-hour in-memory resume watcher is gone.

**Architecture:** `core.RunAgent` merges `DelegateTask` and `SpawnSubagent`: it validates the parent, resumes a repeated call, enforces the background limit, and `startSubagent` creates the child session (readonly flag, optional git worktree), its run and the `SubagentTask` row (now with `Readonly` and `Model`); a blocking child runs inside the call (`ExecuteRun`) with the existing approval bridge, a background one is started with `startRun` and reaches the parent through the stage 6a/6b event path. The executor names the concurrency key per call (`subagents:<dir>` only for blocking shared writers), and `coreTools.Authorize` marks only those calls as barriers and every blocking agent call as `Decision.Delegated`, which the engine uses to leave the time a batch only waits for children out of the run's active time. Readonly children are enforced in the tool list and in `checkToolCall`.

**Tech Stack:** Go 1.26, SQLite (modernc), git worktrees, bubbletea v2 TUI, Telegram Bot API.

**Prerequisite / base:** Stages 6a and 6b (`docs/superpowers/plans/2026-09-29-stage6a-background-tasks.md`, `…-stage6b-await-and-wakeups.md`) are on `main`. This plan was written and every task built and tested in a scratch copy stacked on 6a and 6b on top of **`d290a40`**. Locate code by the declaration names given.

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session may commit in parallel: run `git status --short` before each commit and stage only the paths the task lists with explicit `git add <paths>` / `git rm <paths>` (never `-A`, `-u` or directories).
- Code blocks give complete new files, or for existing files the **whole new version of every declaration that changes** ("replace `func X` with"), the declarations to add, the declarations to delete by name, and the new import block when it changes. `func (T) M` names a method on `T` or `*T`. Non-Go files are given as diffs.
- Run `gofmt -w` on every Go file you touch. Every commit must pass `go build ./... && go vet ./... && go test ./...`.
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style; core tests call `t.Parallel()`.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger.
- Verified: every task below was committed in a scratch worktree (6a, 6b, then this plan, on `d290a40`); after each commit `go build ./... && go vet ./... && go test ./...` passed; on the final state also `go test -race` for `internal/core`, `internal/agent` and `clients/...`, and `deadcode -test ./...` reported nothing new.

## Decisions taken in this plan (owner should know)

1. **No shims:** `delegate_task`, `spawn_subagent`, `list_subagents`, `read_subagent_result`, `DelegateTask*`, `SpawnSubagent*`, `ListSubagents`, `ReadSubagentResult` and `internal/core/subagents_async.go` are deleted. `task_output(id)` returns a subagent task's status and result, the context note lists running ones, `/tasks` shows and stops them, and `await` waits for them. Old transcripts keep their old tool names (rendered by the generic tool card).
2. **Arguments:** `description` (a short label, the child's display name) and `prompt` (everything the child needs) are required; `working_dir`, `context` and `name` are gone — a child works in the parent's directory or in its worktree. `readonly:true` forces `isolation: shared`. Task IDs are `task_…` like shell tasks.
3. **Concurrency** (`agentTool.ConcurrencyKey`): only a blocking child that may write the parent's directory (`isolation shared`, not readonly) takes `subagents:<dir>` and is a barrier (its child may ask for approval, and later calls may depend on its edits). Readonly, worktree and background calls take no key and are no barriers, so several run at once; the parent resumes when every call of the batch is done and no approval is pending (the 4c batch). Children started by one reply get distinct names: naming and creation happen under the parent's session gate.
4. **Readonly** is stored on the task (`tasks.readonly`); `nativeToolSpecs` drops mutating tools for the child and `checkToolCall` refuses one (`tool "x" changes things, and this subagent is read-only`). A readonly **Codex/Claude Code** child is created in `default` permission mode, the runtime's read-only sandbox, instead of `full_auto`.
5. **Background:** `background:true` starts the child with `startRun` and returns `Subagent Neo started as background task task_…. Its result arrives as a message when it finishes; wait for it with await, or go on with other work.`; a repeated call returns the same task. At most `daemon.background_agents` (setup config, default 4) active background children per session (`WithBackgroundAgents`); the fifth call is an error the model reads.
6. **Budget:** blocking agent calls are `Decision.Delegated`; while a batch runs only delegated calls, the time belongs to the children (`run.delegated`, subtracted from `active()`); if the parent also runs its own calls, that time counts. The child's own time and steps are its run's (reported in `run_steps` and on the task's start/finish times), not the parent's.
7. **Resume without the watcher:** after a bridged approval is decided, `passDecisionToSubagent` starts the parent only if the child already ended (or mirrors its next approval); otherwise the child's run end (`afterRunExecution` → `syncBlockingSubagentTaskAfterRun`) writes the parent's call result and starts the parent. `waitForSubagentTerminalAndResumeParent`, `resumeParentAfterSubagentTerminal` and the 24-hour timeout are deleted.
8. **Cancel cascade:** `cancelRunRecords` (children and approvals since 2a) also stops the shell tasks the run started (`canceled with its run`; they stay events for the session's next run). When a subagent's run ends, its session's running shell tasks are stopped (`its subagent finished`) — nobody would read them. The child system prompt says so.
9. **Prompt:** the parent's "Subagents:" section is rewritten for the tool (readonly for research, worktree for parallel writers, background + await, the limit, no recursion); the child's system prompt adds that background commands stop when it finishes and, for readonly children, that it may not change anything; the child's assignment says where it works (read-only / own worktree).
10. **Clients:** the TUI renders `agent` calls with the existing subagent card (description, prompt, runtime); Telegram says "Subagent is working" or, for background calls, "Starting subagent" with the description.

## File structure

Created: `internal/agent/delegated_test.go`, `internal/core/agent_tool_test.go`, `clients/terminal/ui/surface/chat/delegate_test.go`.

Deleted: `internal/core/subagents_async.go`.

Modified: engine `ports.go`, `budget.go`, `engine.go`, `batch.go`, `agenttest`; core `subagents*.go`, `agent_tools.go`, `agent_prompts.go`, `tool_call_prepare.go`, `tool_approvals.go`, `run_execute.go`, `runs.go`, `shell_tasks.go`, `sessions.go`, `core.go`, `types_subagent.go`, `ports.go`; `internal/store/sqlite_subagents.go`; daemon config (`internal/setup/types.go`, `internal/daemoncmd/{bootstrap,run}.go`); TUI `surface/chat/{delegate,tool_preview,tools}.go`, `chat/runtime/{app_layout_views,conversation}.go`; Telegram `run_tools.go`; README, `docs/EXTERNAL_AGENTS.md`, the spec; tests that called the old tools.

---



### Task 1: Time spent only in delegated calls is not active time

**Files:**
- Modify: `internal/agent/agenttest/agenttest.go`
- Modify: `internal/agent/batch.go`
- Modify: `internal/agent/budget.go`
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/ports.go`
- Test (create): `internal/agent/delegated_test.go`

The engine side of "not charged to the parent": a call can be marked `Delegated`, and the batch tracks when only delegated calls run.

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/delegated_test.go`:

```go
package agent_test

import (
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestTimeSpentOnlyInDelegatedCallsIsNotActiveTime(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Delegated = map[string]bool{"agent": true}
	f.Tools.Funcs["agent"] = func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(10 * time.Minute)
		return tools.Result{Content: "child result"}
	}
	f.Tools.Funcs["bash"] = func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(time.Minute)
		return tools.Result{Content: "ok"}
	}
	model := agenttest.NewScriptedModel(calls(call("a1", "agent")), calls(call("b1", "bash")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Counters.Active != time.Minute {
		t.Fatalf("outcome = %+v, active %v", outcome, outcome.Counters.Active)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/agent -run '^(TestTimeSpentOnlyInDelegatedCallsIsNotActiveTime)$'
```

Expected: build fails (`Tools.Delegated`, `Decision.Delegated` undefined).

- [ ] **Step 3: Implement**

In `internal/agent/agenttest/agenttest.go`, replace `type Tools` with:

```go
// Tools authorizes registered names only (Mutating ones are barriers, Keys
// and Delegated fill the decision) and records executed calls in start order
// (read Calls after Run returns) and finished ones. OnExecute runs first, on
// the call's goroutine, and fails the call with its error; OnFinish likewise.
type Tools struct {
	Funcs     map[string]ToolFunc
	Mutating  map[string]bool
	Keys      map[string]string
	Delegated map[string]bool
	Calls     []tools.Call
	Finished  []string
	OnExecute func(ctx context.Context, name string, call tools.Call) error
	OnFinish  func(name string, call tools.Call) error
	// SpecReads counts tool listings.
	SpecReads int
	mu        sync.Mutex
}
```

In `internal/agent/agenttest/agenttest.go`, replace `func (Tools) Authorize` with:

```go
func (t *Tools) Authorize(_ context.Context, name string, _ tools.Call) (agent.Decision, error) {
	if _, ok := t.Funcs[name]; !ok {
		return agent.Decision{Reason: fmt.Sprintf("invalid input: unknown tool %q", name)}, nil
	}
	return agent.Decision{Allowed: true, Barrier: t.Mutating[name], Key: t.Keys[name], Delegated: t.Delegated[name]}, nil
}
```

In `internal/agent/batch.go`, replace `type batchCall` with:

```go
type batchCall struct {
	req       callRequest
	call      tools.Call
	key       string
	delegated bool
	state     callState
	result    tools.Result
}
```

In `internal/agent/batch.go`, replace `type batch` with:

```go
// batch is one runCalls: calls are admitted in call order, run concurrently by
// key and journaled in call order by the engine goroutine alone.
type batch struct {
	r     *run
	calls []*batchCall
	sched *toolsched.Batch[callOutcome]
	// next is the first call not admitted; barrier is the admitted barrier whose
	// approval is not known yet, or -1; held is set once a barrier asked.
	next, barrier int
	held          bool
	// journaled is the first admitted call whose result is not journaled.
	journaled int
	waiting   bool
	// delegating and working count the running delegated and other calls;
	// since is when only delegated calls began to run.
	delegating, working int
	since               time.Time
}
```

In `internal/agent/batch.go`, replace `func (batch) admit` with:

```go
// admit authorizes and journals calls in call order until one is a barrier
// whose approval is not known yet, journals the calls behind it deferred and
// checkpoints the batch; only then do the admitted calls start.
func (b *batch) admit(ctx context.Context) error {
	if b.held || b.barrier >= 0 || b.next == len(b.calls) {
		return nil
	}
	var started []int
	for b.next < len(b.calls) && b.barrier < 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		c := b.calls[b.next]
		decision, err := b.r.Tools.Authorize(ctx, c.req.name, c.call)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			c.state, c.result = callRejected, tools.Result{Content: decision.Reason, IsError: true}
			b.next++
			continue
		}
		if err := b.r.startCall(ctx, c.req); err != nil {
			return err
		}
		c.state, c.key, c.delegated = callRunning, decision.Key, decision.Delegated
		if decision.Barrier && !c.req.approved {
			b.barrier = b.next
		}
		started = append(started, b.next)
		b.next++
	}
	for _, c := range b.calls[b.next:] {
		if err := b.r.deferCall(ctx, c.req); err != nil {
			return err
		}
	}
	if err := b.r.checkpoint(ctx, PhaseToolBatch, b.checkpoint()); err != nil {
		return err
	}
	for _, i := range started {
		b.launch(i)
	}
	return nil
}
```

In `internal/agent/batch.go`, replace `func (batch) launch` with:

```go
// launch runs call i on its own goroutine, which only executes the tool.
func (b *batch) launch(i int) {
	c := b.calls[i]
	b.track(c, 1)
	name, call := c.req.name, c.call
	b.sched.Go(i, c.key, func(ctx context.Context) callOutcome {
		result, err := b.r.Tools.Execute(ctx, name, call)
		return callOutcome{result: result, err: err}
	})
}
```

In `internal/agent/batch.go`, replace `func (batch) settle` with:

```go
// settle takes a finished call: one that asks for approval is requested at
// once, and a barrier that asked holds back the calls not admitted yet. A call
// that returns after the run's context stopped counts as stopped.
func (b *batch) settle(ctx context.Context, done toolsched.Done[callOutcome]) error {
	c := b.calls[done.Index]
	b.track(c, -1)
	if err := ctx.Err(); err != nil {
		c.state = callStopped
		return err
	}
	asked, err := b.record(done)
	if err != nil {
		return err
	}
	if asked {
		if err := b.r.Approvals.Request(ctx, Pending{RunID: b.r.task.RunID, SessionID: b.r.task.SessionID, ToolCallID: c.req.id, ToolName: c.req.name, Request: *done.Value.result.Approval}); err != nil {
			return err
		}
		b.waiting = true
		b.held = b.held || done.Index == b.barrier
	}
	if done.Index == b.barrier {
		b.barrier = -1
	}
	return nil
}
```

In `internal/agent/batch.go`, replace `func (batch) drain` with:

```go
// drain waits for the calls still running once the batch stopped and drops
// what they return: they were stopped too.
func (b *batch) drain() {
	for b.sched.Running() > 0 {
		c := b.calls[b.sched.Next().Index]
		b.track(c, -1)
		c.state = callStopped
	}
}
```

In `internal/agent/batch.go`, add:

```go
// track counts a call that starts (+1) or ends (-1); the time in which only
// delegated calls run belongs to the agents they run, not to this run.
func (b *batch) track(c *batchCall, delta int) {
	now := b.r.Now()
	if !b.since.IsZero() {
		b.r.delegated += now.Sub(b.since)
		b.since = time.Time{}
	}
	if c.delegated {
		b.delegating += delta
	} else {
		b.working += delta
	}
	if b.delegating > 0 && b.working == 0 {
		b.since = now
	}
}
```

In `internal/agent/budget.go`, replace `func (run) active` with:

```go
// active is the run's working time: what it had used before plus this Run
// call, without the time it only waited for delegated calls.
func (r *run) active() time.Duration {
	return r.task.Resume.Active + r.Now().Sub(r.started) - r.delegated
}
```

In `internal/agent/engine.go`, replace `type run` with:

```go
type run struct {
	Config
	task     Task
	history  *history
	counters Counters
	started  time.Time
	// delegated is the time this Run call only waited for delegated calls.
	delegated  time.Duration
	anchor     *promptAnchor
	requestSeq int64
	// system, custom and tools are built once, so every request of the run
	// shares one prefix.
	system, custom string
	tools          []providers.ToolDefinition
}
```

In `internal/agent/ports.go`, replace `type Decision` with:

```go
// Decision says whether a requested tool call may run; Reason goes back to the model.
// A Barrier call that waits for approval holds back the calls after it in its batch.
// Calls sharing a non-empty Key run one at a time, across runs too. A Delegated
// call runs another agent, whose time is not this run's.
type Decision struct {
	Allowed   bool
	Reason    string
	Barrier   bool
	Key       string
	Delegated bool
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/agent -run '^(TestTimeSpentOnlyInDelegatedCallsIsNotActiveTime)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/agent/agenttest/agenttest.go \
  internal/agent/batch.go \
  internal/agent/budget.go \
  internal/agent/delegated_test.go \
  internal/agent/engine.go \
  internal/agent/ports.go
git commit -m "feat(agent): time a run only waits for delegated calls is not its active time

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 2: Subagent tasks keep readonly and model

**Files:**
- Modify: `internal/core/ports.go`
- Modify: `internal/core/types_subagent.go`
- Modify: `internal/store/sqlite_subagents.go`
- Test (modify): `internal/store/sqlite_tasks_test.go`

The task row keeps `readonly` and `model` (columns created in 6a) and can be found by its child session, which `checkToolCall` needs for a readonly child.

- [ ] **Step 1: Write the failing tests**

In `internal/store/sqlite_tasks_test.go`, add:

```go
func TestSubagentTaskKeepsReadonlyModelAndChildSession(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	task := core.SubagentTask{ID: "task_1", Mode: core.SubagentTaskModeAsync, Readonly: true, Model: "gpt-x", ParentSessionID: "s1", ChildSessionID: "child_1", Runtime: "matrixclaw", Goal: "Review", Status: core.TaskStatusRunning, CreatedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSubagentTaskByChildSession(ctx, "child_1")
	if err != nil || !got.Readonly || got.Model != "gpt-x" || got.ID != "task_1" {
		t.Fatalf("task = %+v, %v", got, err)
	}
	task.Readonly = false
	if err := st.UpdateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetSubagentTask(ctx, "task_1"); err != nil || got.Readonly {
		t.Fatalf("updated task = %+v, %v", got, err)
	}
	if _, err := st.GetSubagentTaskByChildSession(ctx, "nobody"); err != core.ErrNotFound {
		t.Fatalf("missing child session err = %v", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/store -run '^(TestSubagentTaskKeepsReadonlyModelAndChildSession)$'
```

Expected: build fails (`Readonly`, `Model`, `GetSubagentTaskByChildSession` undefined).

- [ ] **Step 3: Implement**

In `internal/core/ports.go`, replace `type SubagentTaskStore` with:

```go
type SubagentTaskStore interface {
	CreateSubagentTask(ctx context.Context, task SubagentTask) error
	UpdateSubagentTask(ctx context.Context, task SubagentTask) error
	GetSubagentTask(ctx context.Context, taskID string) (SubagentTask, error)
	GetSubagentTaskByParentToolCall(ctx context.Context, parentSessionID string, parentRunID string, parentToolCallID string) (SubagentTask, error)
	GetSubagentTaskByChildRun(ctx context.Context, childRunID string) (SubagentTask, error)
	GetSubagentTaskByChildSession(ctx context.Context, childSessionID string) (SubagentTask, error)
	ListSubagentTasks(ctx context.Context, filter SubagentTaskFilter) ([]SubagentTask, error)
	ListActiveSubagentTasksByParent(ctx context.Context, parentSessionID string) ([]SubagentTask, error)
}
```

In `internal/core/types_subagent.go`, replace `type SubagentTask` with:

```go
type SubagentTask struct {
	ID          string            `json:"id"`
	AgentName   string            `json:"agent_name,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	Mode        SubagentTaskMode  `json:"mode"`
	Isolation   SubagentIsolation `json:"isolation"`
	// Readonly children get read-only tools only.
	Readonly         bool       `json:"readonly,omitempty"`
	ParentSessionID  string     `json:"parent_session_id"`
	ParentRunID      string     `json:"parent_run_id,omitempty"`
	ParentToolCallID string     `json:"parent_tool_call_id,omitempty"`
	ChildSessionID   string     `json:"child_session_id,omitempty"`
	ChildRunID       string     `json:"child_run_id,omitempty"`
	Runtime          string     `json:"runtime"`
	Model            string     `json:"model,omitempty"`
	Goal             string     `json:"goal"`
	Status           TaskStatus `json:"status"`
	Summary          string     `json:"summary,omitempty"`
	Error            string     `json:"error,omitempty"`
	ResultMessageID  string     `json:"result_message_id,omitempty"`
	// DeliveredAt is when the parent was told the task finished, and
	// DeliveredRunID the run that told it; nil while that is due.
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	DeliveredRunID string     `json:"delivered_run_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}
```

In `internal/store/sqlite_subagents.go`, replace `func (SQLiteStore) CreateSubagentTask` with:

```go
func (s *SQLiteStore) CreateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	task = normalizeSubagentTaskForStore(task)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks(
    id, kind, agent_name, description, background, isolation, readonly, session_id, run_id, parent_tool_call_id,
    child_session_id, child_run_id, runtime, model, command_or_goal, status, summary, error, result_message_id,
    started_at, updated_at, finished_at
)
VALUES(?, 'subagent', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.AgentName,
		task.DisplayName,
		task.Mode == core.SubagentTaskModeAsync,
		string(task.Isolation),
		task.Readonly,
		task.ParentSessionID,
		task.ParentRunID,
		task.ParentToolCallID,
		task.ChildSessionID,
		task.ChildRunID,
		task.Runtime,
		task.Model,
		task.Goal,
		string(task.Status),
		task.Summary,
		task.Error,
		task.ResultMessageID,
		formatTime(task.CreatedAt),
		formatTime(task.UpdatedAt),
		nullableTime(task.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create subagent task: %w", err)
	}
	return nil
}
```

In `internal/store/sqlite_subagents.go`, replace `func (SQLiteStore) UpdateSubagentTask` with:

```go
// UpdateSubagentTask saves the task; whether and when it was delivered is kept
// by MarkTasksDelivered alone.
func (s *SQLiteStore) UpdateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	task = normalizeSubagentTaskForStore(task)
	result, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET agent_name = ?, description = ?, background = ?, isolation = ?, readonly = ?, session_id = ?, run_id = ?, parent_tool_call_id = ?,
    child_session_id = ?, child_run_id = ?, runtime = ?, model = ?, command_or_goal = ?, status = ?, summary = ?, error = ?,
    result_message_id = ?, updated_at = ?, finished_at = ?
WHERE id = ? AND kind = 'subagent'`,
		task.AgentName,
		task.DisplayName,
		task.Mode == core.SubagentTaskModeAsync,
		string(task.Isolation),
		task.Readonly,
		task.ParentSessionID,
		task.ParentRunID,
		task.ParentToolCallID,
		task.ChildSessionID,
		task.ChildRunID,
		task.Runtime,
		task.Model,
		task.Goal,
		string(task.Status),
		task.Summary,
		task.Error,
		task.ResultMessageID,
		formatTime(task.UpdatedAt),
		nullableTime(task.FinishedAt),
		task.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update subagent task: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: update subagent task rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}
```

In `internal/store/sqlite_subagents.go`, replace `const subagentTaskColumns` with:

```go
const subagentTaskColumns = `id, agent_name, description, background, isolation, readonly, session_id, run_id, parent_tool_call_id,
child_session_id, child_run_id, runtime, model, command_or_goal, status, summary, error, result_message_id,
delivered_at, delivered_run_id, started_at, updated_at, finished_at`
```

In `internal/store/sqlite_subagents.go`, replace `func scanSubagentTask` with:

```go
func scanSubagentTask(scanner subagentTaskScanner) (core.SubagentTask, error) {
	var task core.SubagentTask
	var status string
	var background bool
	var isolation string
	var createdAt string
	var updatedAt string
	var finishedAt sql.NullString
	var deliveredAt sql.NullString
	if err := scanner.Scan(
		&task.ID,
		&task.AgentName,
		&task.DisplayName,
		&background,
		&isolation,
		&task.Readonly,
		&task.ParentSessionID,
		&task.ParentRunID,
		&task.ParentToolCallID,
		&task.ChildSessionID,
		&task.ChildRunID,
		&task.Runtime,
		&task.Model,
		&task.Goal,
		&status,
		&task.Summary,
		&task.Error,
		&task.ResultMessageID,
		&deliveredAt,
		&task.DeliveredRunID,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.SubagentTask{}, err
		}
		return core.SubagentTask{}, fmt.Errorf("store: scan subagent task: %w", err)
	}
	task.Mode = core.SubagentTaskModeBlocking
	if background {
		task.Mode = core.SubagentTaskModeAsync
	}
	task.Isolation = core.SubagentIsolation(isolation)
	if task.Isolation == "" {
		task.Isolation = core.SubagentIsolationShared
	}
	task.Status = core.TaskStatus(status)
	task.CreatedAt = mustParseTime(createdAt)
	task.UpdatedAt = mustParseTime(updatedAt)
	task.DeliveredAt = parseNullableTime(deliveredAt)
	task.FinishedAt = parseNullableTime(finishedAt)
	return task, nil
}
```

In `internal/store/sqlite_subagents.go`, add:

```go
func (s *SQLiteStore) GetSubagentTaskByChildSession(ctx context.Context, childSessionID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM tasks
WHERE kind = 'subagent' AND child_session_id = ?
ORDER BY started_at ASC
LIMIT 1`, childSessionID)
	return scanOneSubagentTask(row)
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/store -run '^(TestSubagentTaskKeepsReadonlyModelAndChildSession)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/ports.go \
  internal/core/types_subagent.go \
  internal/store/sqlite_subagents.go \
  internal/store/sqlite_tasks_test.go
git commit -m "feat(store): subagent tasks keep readonly and model

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 3: One `agent` tool replaces the subagent tools

**Files:**
- Modify: `internal/core/agent_prompts.go`
- Modify: `internal/core/agent_tools.go`
- Modify: `internal/core/core.go`
- Modify: `internal/core/run_execute.go`
- Modify: `internal/core/subagents.go`
- Delete: `internal/core/subagents_async.go`
- Modify: `internal/core/subagents_lifecycle.go`
- Modify: `internal/core/subagents_presentation.go`
- Modify: `internal/core/subagents_prompt.go`
- Modify: `internal/core/subagents_runtime.go`
- Modify: `internal/core/subagents_tools.go`
- Modify: `internal/core/tool_call_prepare.go`
- Modify: `internal/daemoncmd/bootstrap.go`
- Modify: `internal/daemoncmd/run.go`
- Modify: `internal/setup/types.go`
- Test (create): `internal/core/agent_tool_test.go`
- Test (modify): `internal/core/concurrency_key_test.go`
- Test (modify): `internal/core/model_concurrency_test.go`
- Test (modify): `internal/core/native_run_characterization_test.go`
- Test (modify): `internal/core/run_budget_test.go`
- Test (modify): `internal/core/run_crash_recovery_integration_test.go`
- Test (modify): `internal/core/run_reschedule_test.go`
- Test (modify): `internal/core/subagents_approval_test.go`
- Test (modify): `internal/core/subagents_cancel_test.go`
- Test (modify): `internal/core/tool_batch_test.go`
- Test (modify): `internal/providers/ai/gemini/schema_test.go`

The tool itself. Existing tests that called `delegate_task` / `spawn_subagent` switch to `agent` (their arguments become `description` / `prompt`, `background:true` for spawn; the whole new versions follow in Step 1), and `SubagentToolExecutors` is renamed `AgentToolExecutors` everywhere — run this before Step 1:

```bash
grep -rl 'SubagentToolExecutors' --include=*.go . | xargs sed -i 's/SubagentToolExecutors/AgentToolExecutors/g'
```

The asynchronous-subagent scenario of stage 6b already expects a `wake` follow-up run; its model only needs to see `finished: completed.` in the child's note.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/agent_tool_test.go`:

```go
package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// childPrompt is the delegated prompt of a child's request.
func childPrompt(request providers.Request) string {
	for _, message := range request.Messages {
		if message.Role == "user" && strings.HasPrefix(message.Content, "Delegated task:") {
			return message.Content
		}
	}
	return ""
}

func TestReadonlyChildrenRunTogetherWithReadOnlyTools(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var started sync.WaitGroup
	started.Add(2)
	both := make(chan struct{})
	go func() { started.Wait(); close(both) }()
	probe := funcTool{spec: recoveryToolSpec("probe", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		started.Done()
		select {
		case <-both:
			return tools.Result{Content: "probed"}, nil
		case <-time.After(5 * time.Second):
			return tools.Result{Content: "the other child never ran alongside", IsError: true}, nil
		}
	}}
	edit := funcTool{spec: recoveryToolSpec("edit_file", tools.EffectMutation), fn: func(context.Context, tools.Call) (tools.Result, error) {
		return tools.Result{Content: "edited"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), probe, edit)...))
	var mu sync.Mutex
	var childTools [][]string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		prompt := childPrompt(request)
		answered := len(toolResults(request)) > 0
		switch {
		case prompt != "" && !answered:
			mu.Lock()
			var names []string
			for _, tool := range request.Tools {
				names = append(names, tool.Name)
			}
			childTools = append(childTools, names)
			mu.Unlock()
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "probe-" + strings.Fields(prompt)[3], Name: "probe", Arguments: json.RawMessage(`{}`)}}}, nil
		case prompt != "" && strings.HasSuffix(toolResults(request)[0], "=probed"):
			return providers.Response{Text: "looked at " + strings.Fields(prompt)[3]}, nil
		case prompt != "":
			return providers.Response{Text: "ran alone"}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"Review A","prompt":"review alpha","readonly":true}`)},
				{ID: "call-b", Name: "agent", Arguments: json.RawMessage(`{"description":"Review B","prompt":"review beta","readonly":true}`)},
			}}, nil
		default:
			return providers.Response{Text: "Both reviewed."}, nil
		}
	})})
	_, run := saveCrashRecoveryRun(t, db, "readonly", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if len(childTools) != 2 {
		t.Fatalf("child tool lists = %v", childTools)
	}
	for _, names := range childTools {
		if !slices.Contains(names, "probe") || slices.Contains(names, "edit_file") || slices.Contains(names, "agent") {
			t.Fatalf("a read-only child got %v", names)
		}
	}
	names := map[string]bool{}
	for _, id := range []string{"call-a", "call-b"} {
		task, err := db.GetSubagentTaskByParentToolCall(context.Background(), "session_readonly", run.ID, id)
		if err != nil || !task.Readonly || task.Status != core.TaskStatusCompleted || !strings.HasPrefix(task.Summary, "looked at") {
			t.Fatalf("task %s = %+v, %v", id, task, err)
		}
		names[task.AgentName] = true
	}
	if len(names) != 2 {
		t.Fatalf("children share a name: %v", names)
	}
}

func TestReadonlyChildCannotUseAMutatingTool(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	edit := funcTool{spec: recoveryToolSpec("edit_file", tools.EffectMutation), fn: func(context.Context, tools.Call) (tools.Result, error) {
		return tools.Result{Content: "edited"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), edit)...))
	var rejected string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results := toolResults(request)
		switch {
		case childPrompt(request) != "" && len(results) == 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-edit", Name: "edit_file", Arguments: json.RawMessage(`{}`)}}}, nil
		case childPrompt(request) != "":
			rejected = results[0]
			return providers.Response{Text: "could not edit"}, nil
		case len(results) == 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-a", Name: "agent", Arguments: json.RawMessage(`{"description":"Look","prompt":"look around","readonly":true}`)}}}, nil
		default:
			return providers.Response{Text: "Done."}, nil
		}
	})})
	_, run := saveCrashRecoveryRun(t, db, "readonly_edit", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(rejected, "this subagent is read-only") {
		t.Fatalf("the child's edit result = %q", rejected)
	}
}

func TestBackgroundChildrenAreLimitedPerSession(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithBackgroundAgents(1).WithRunStarter(&recordingRunStarter{})
	session, run := saveCrashRecoveryRun(t, db, "limit", core.RunStatusRunning, false)
	start := func(callID string) (core.AgentResult, error) {
		return app.RunAgent(context.Background(), core.AgentInput{ParentSessionID: session.ID, ParentRunID: run.ID, ParentToolCallID: callID, Description: "Scan", Prompt: "scan the tree", Background: true})
	}

	first, err := start("call-1")
	if err != nil || first.Task.Mode != core.SubagentTaskModeAsync || !strings.HasPrefix(first.Task.ID, "task_") {
		t.Fatalf("first = %+v, %v", first, err)
	}
	again, err := start("call-1")
	if err != nil || !again.Replayed || again.Task.ID != first.Task.ID {
		t.Fatalf("repeated call = %+v, %v", again, err)
	}
	if _, err := start("call-2"); !errors.Is(err, core.ErrInvalidInput) || !strings.Contains(err.Error(), "at most 1 background subagents") {
		t.Fatalf("second = %v", err)
	}
}
```

In `internal/core/concurrency_key_test.go`, add:

```go
func TestOnlyChildrenWritingTheParentsDirectoryHoldItsKey(t *testing.T) {
	t.Parallel()
	app, _, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	registry := tools.NewRegistry(core.AgentToolExecutors(app)...)
	for _, tc := range []struct {
		args, want string
	}{
		{`{"description":"Count","prompt":"count files"}`, "subagents:/work"},
		{`{"description":"Count","prompt":"count files","isolation":"shared"}`, "subagents:/work"},
		{`{"description":"Review","prompt":"review the diff","readonly":true}`, ""},
		{`{"description":"Fix","prompt":"fix the bug","isolation":"worktree"}`, ""},
		{`{"description":"Build","prompt":"build it","background":true}`, ""},
	} {
		if got := registry.ConcurrencyKey("agent", tools.Call{WorkingDir: "/work/", Args: []byte(tc.args)}); got != tc.want {
			t.Errorf("key for %s = %q, want %q", tc.args, got, tc.want)
		}
	}
}
```

In `internal/core/concurrency_key_test.go`, delete `func TestDelegatedChildrenHoldTheirOwnKeyPerDirectory`.

In `internal/core/model_concurrency_test.go`, replace `func TestBlockingSubagentRunsUnderASingleModelSlot` with:

```go
func TestBlockingSubagentRunsUnderASingleModelSlot(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithModelConcurrency(1)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child done"}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Count files","prompt":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "one_slot", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if parentCalls != 2 {
		t.Fatalf("parent model calls = %d, want 2", parentCalls)
	}
}
```

In `internal/core/native_run_characterization_test.go`, replace `func TestBlockingSubagentReturnsChildSummaryToParent` with:

```go
func TestBlockingSubagentReturnsChildSummaryToParent(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	parentCalls := 0
	var delegateResult string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child found 3 files"}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Count files","prompt":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		for _, message := range request.Messages {
			if message.ToolCallID == "call-delegate" {
				delegateResult = message.Content
			}
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "delegate", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if delegateResult != "child found 3 files" {
		t.Fatalf("delegate result = %q", delegateResult)
	}
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.TaskStatusCompleted || task.Summary != "child found 3 files" {
		t.Fatalf("task = %s %q", task.Status, task.Summary)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCompleted)
}
```

In `internal/core/native_run_characterization_test.go`, replace `func TestSubagentSummaryJoinsAReplyCutByTheOutputLimit` with:

```go
func TestSubagentSummaryJoinsAReplyCutByTheOutputLimit(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	parentCalls, childCalls := 0, 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			childCalls++
			if childCalls == 1 {
				return providers.Response{Text: "child fo", StopReason: providers.StopMaxTokens}, nil
			}
			return providers.Response{Text: "und 3 files"}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Count files","prompt":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "delegate-cut", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.TaskStatusCompleted || task.Summary != "child found 3 files" {
		t.Fatalf("task = %s %q, want the cut reply joined with its continuation", task.Status, task.Summary)
	}
}
```

In `internal/core/native_run_characterization_test.go`, replace `func runAsyncSubagentScenario` with:

```go
func runAsyncSubagentScenario(t *testing.T) asyncSubagentScenario {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	app.WithRunStarter(orchestration.NewStub(app))
	var mu sync.Mutex
	spawned := false
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child async result"}, nil
		}
		for _, message := range request.Messages {
			if message.Role == "user" && strings.HasPrefix(message.Content, "Subagent ") && strings.Contains(message.Content, "finished: completed.") {
				return providers.Response{Text: "Synthesized."}, nil
			}
		}
		if !spawned {
			spawned = true
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "agent", Arguments: []byte(`{"description":"Scanner","prompt":"scan the tree","background":true,"runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Spawned."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "async", core.RunStatusAccepted, false)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := app.SubscribeEvents(ctx, session.ID)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)

	deadline := time.Now().Add(10 * time.Second)
	for {
		runs, err := db.ListSessionRuns(context.Background(), session.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if runs[0].Trigger == core.RunTriggerWake && runs[0].Status == core.RunStatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subagent completion follow-up run did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return asyncSubagentScenario{db: db, session: session, run: run, events: drainEvents(events)}
}
```

In `internal/core/run_budget_test.go`, replace `func TestSubagentStoppedAtItsBudgetReportsAPartialResult` with:

```go
func TestSubagentStoppedAtItsBudgetReportsAPartialResult(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunBudgets(core.RunBudgets{User: agent.Budget{Steps: 5}, Subagent: agent.Budget{Steps: 1}})
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), changingTool("inspect_state"))...))
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			if request.ToolChoice == providers.ToolChoiceNone {
				return providers.Response{Text: "found 2 of 3 files"}, nil
			}
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-inspect", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Count files","prompt":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "delegate-budget", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(task.Summary, "Subagent stopped early (") || !strings.HasSuffix(task.Summary, "; partial result: found 2 of 3 files") {
		t.Fatalf("task summary = %q, want it marked as an early stop", task.Summary)
	}
	if strings.Contains(task.Summary, "/continue") {
		t.Fatalf("task summary = %q, want no user-facing /continue hint", task.Summary)
	}
}
```

In `internal/core/run_crash_recovery_integration_test.go`, replace `func TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate` with:

```go
func TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate(t *testing.T) {
	t.Parallel()
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "recovered completion"}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	app.WithRunStarter(orchestration.NewStub(app))
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))

	parentSession, parentRun := saveCrashRecoveryRun(t, sqliteStore, "parent", core.RunStatusRunning, false)
	childSession, childRun := saveCrashRecoveryRun(t, sqliteStore, "child", core.RunStatusRunning, true)
	parentRun.StartedAt = childRun.StartedAt.Add(-time.Minute)
	parentRun.UpdatedAt = parentRun.StartedAt
	if err := sqliteStore.UpdateRun(context.Background(), parentRun); err != nil {
		t.Fatal(err)
	}
	delegateArgs := `{"description":"Finish","prompt":"finish child work","runtime":"matrixclaw"}`
	saveInterruptedToolCallWithInput(t, sqliteStore, parentRun, "tool_delegate", "agent", delegateArgs)
	now := runRecoveryTestTime().Add(3 * time.Second)
	if err := sqliteStore.CreateSubagentTask(context.Background(), core.SubagentTask{
		ID: "subagent_recovery", AgentName: "Neo", DisplayName: "Recovery child",
		Mode: core.SubagentTaskModeBlocking, Isolation: core.SubagentIsolationShared,
		ParentSessionID: parentSession.ID, ParentRunID: parentRun.ID, ParentToolCallID: "tool_delegate",
		ChildSessionID: childSession.ID, ChildRunID: childRun.ID, Runtime: "matrixclaw",
		Goal: "finish child work", Status: core.TaskStatusRunning,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, childRun.ID, core.RunStatusCompleted)
	waitForRecoveryRunStatus(t, sqliteStore, parentRun.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, parentSession.ID, "tool_delegate", 1)
	task, err := sqliteStore.GetSubagentTask(context.Background(), "subagent_recovery")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.TaskStatusCompleted {
		t.Fatalf("subagent task status = %q, want completed", task.Status)
	}
}
```

In `internal/core/run_reschedule_test.go`, replace `func TestInterruptedParentAndBlockingChildAreBothRescheduledAndComplete` with:

```go
func TestInterruptedParentAndBlockingChildAreBothRescheduledAndComplete(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	childStarted := newStartSignal()
	var mu sync.Mutex
	childCalls := 0
	var delegateResult string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			childCalls++
			first := childCalls == 1
			mu.Unlock()
			if first {
				return blockUntilCanceled(ctx, childStarted)
			}
			return providers.Response{Text: "child found 3 files"}, nil
		}
		defer mu.Unlock()
		for _, message := range request.Messages {
			if message.ToolCallID == "call-delegate" {
				delegateResult = message.Content
				return providers.Response{Text: "Parent done."}, nil
			}
		}
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Count files","prompt":"count files","runtime":"matrixclaw"}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "interrupt-delegate", core.RunStatusAccepted, false)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(ctx, run.ID) }()
	waitRecoverySignal(t, childStarted.ch, "child generation")
	cancel()
	if err := waitRecoveryError(t, done, "interrupted parent"); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	waitForRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	starter.wait(t)
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCompleted)
	if task.Status != core.TaskStatusCompleted || task.Summary != "child found 3 files" {
		t.Fatalf("task = %s %q", task.Status, task.Summary)
	}
	if starter.count(run.ID) == 0 || starter.count(task.ChildRunID) == 0 {
		t.Fatalf("reschedules parent=%d child=%d, want both", starter.count(run.ID), starter.count(task.ChildRunID))
	}
	assertToolResultCount(t, db, session.ID, "call-delegate", 1)
	mu.Lock()
	defer mu.Unlock()
	if delegateResult != "child found 3 files" {
		t.Fatalf("delegate result seen by the parent = %q", delegateResult)
	}
}
```

In `internal/core/subagents_approval_test.go`, replace `func newBridgedChild` with:

```go
func newBridgedChild(t *testing.T, starter func(*core.Core) core.RunStarter, lead ...providers.ToolCall) *bridgedChild {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	parking := &parkingStore{SQLiteStore: db}
	app := core.New(parking)
	b := &bridgedChild{app: app, db: db, parking: parking}
	app.WithRunStarter(starter(app))
	mutate, _ := approvalTools(&b.mutations)
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), mutate, askingReadTool("ask_read"))...))
	childCalls, parentCalls := 0, 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			childCalls++
			if childCalls == 1 {
				return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
			}
			b.saw = toolResultContent(request, "call-child-mutate")
			return providers.Response{Text: "Child read: " + b.saw}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			delegate := providers.ToolCall{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Change the state","prompt":"change the state","runtime":"matrixclaw"}`)}
			return providers.Response{ToolCalls: append(slices.Clone(lead), delegate)}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	return b
}
```

In `internal/core/subagents_cancel_test.go`, replace `func TestCancelParentCancelsItsBlockingSubagent` with:

```go
func TestCancelParentCancelsItsBlockingSubagent(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	childStarted := newStartSignal()
	var mu sync.Mutex
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return blockUntilCanceled(ctx, childStarted)
		}
		mu.Lock()
		parentCalls++
		first := parentCalls == 1
		mu.Unlock()
		if first {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Count files","prompt":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cancel-delegate", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, childStarted.ch, "child generation")

	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled parent"); err != nil {
		t.Fatalf("ExecuteRun after cancel: %v", err)
	}
	starter.wait(t)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCanceled)
	assertTaskStatus(t, task, core.TaskStatusCanceled)
	if got := starter.count(task.ChildRunID) + starter.count(run.ID); got != 0 {
		t.Fatalf("reschedules = %d, want 0", got)
	}
}
```

In `internal/core/subagents_cancel_test.go`, replace `func TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp` with:

```go
func TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	childStarted := newStartSignal()
	parentWaiting := newStartSignal()
	var mu sync.Mutex
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return blockUntilCanceled(ctx, childStarted)
		}
		mu.Lock()
		parentCalls++
		first := parentCalls == 1
		mu.Unlock()
		if first {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "agent", Arguments: []byte(`{"description":"Scanner","prompt":"scan the tree","background":true,"runtime":"matrixclaw"}`)}}}, nil
		}
		return blockUntilCanceled(ctx, parentWaiting)
	})})
	session, run := saveCrashRecoveryRun(t, db, "cancel-spawn", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, childStarted.ch, "child generation")
	waitRecoverySignal(t, parentWaiting.ch, "parent generation")

	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled parent"); err != nil {
		t.Fatalf("ExecuteRun after cancel: %v", err)
	}
	starter.wait(t)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-spawn")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCanceled)
	assertTaskStatus(t, task, core.TaskStatusCanceled)
	if task.DeliveredAt == nil || task.DeliveredRunID != "" {
		t.Fatal("a canceled async subagent left a completion for its canceled parent")
	}
	if got := starter.count(task.ChildRunID); got != 1 {
		t.Fatalf("child starts = %d, want 1", got)
	}
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleUser && message.RunID != run.ID {
			t.Fatalf("parent session got a follow-up run %s after cancel", message.RunID)
		}
	}
}
```

In `internal/core/tool_batch_test.go`, replace `func TestDelegatedChildChangesTheParentsDirectoryWithoutDeadlock` with:

```go
func TestDelegatedChildChangesTheParentsDirectoryWithoutDeadlock(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	edit := recoveryToolSpec("mutate_state", tools.EffectMutation)
	edit.Risk, edit.ApprovalMode = tools.RiskSafe, tools.ApprovalNever
	edits := 0
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), funcTool{spec: edit, fn: func(context.Context, tools.Call) (tools.Result, error) {
		edits++
		return tools.Result{Content: "mutated"}, nil
	}})...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		answered := len(toolResults(request)) > 0
		switch {
		case strings.Contains(request.SystemPrompt, "Subagent mode:") && !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-edit", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		case strings.Contains(request.SystemPrompt, "Subagent mode:"):
			return providers.Response{Text: "child done"}, nil
		case !answered:
			args, _ := json.Marshal(map[string]string{"description": "Edit the file", "prompt": "edit the file", "runtime": "matrixclaw"})
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: args}}}, nil
		default:
			return providers.Response{Text: "Parent done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "delegate_edit", core.RunStatusAccepted, false)
	sessionIn(t, db, session, t.TempDir(), core.PermissionModeDefault)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()

	if err := waitRecoveryError(t, done, "delegation"); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if edits != 1 {
		t.Fatalf("child edits = %d, want 1", edits)
	}
}
```

In `internal/providers/ai/gemini/schema_test.go`, replace `func registeredToolDefinitions` with:

```go
// registeredToolDefinitions mirrors the daemon's registry, minus modules that import this package.
func registeredToolDefinitions(t *testing.T) []providers.ToolDefinition {
	t.Helper()
	app := core.New(nil)
	web := webtools.NewWebService(nil, nil)
	registry := tools.NewCoreCodingRegistry(
		automation.NewReminderTool(nil),
		automation.NewScheduledAITaskTool(nil),
		deliverymodule.NewSendFileTool(nil, nil),
		webtools.NewWebFetchExecutorWithService(web),
		webtools.NewWebSearchExecutorWithService(web),
	)
	storage, err := storagemodule.New(storagemodule.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, executors := range [][]tools.Executor{
		webtools.NewWebResearchExecutorsWithService(web),
		tools.NewShellExecutors(app),
		core.TodoToolExecutors(app),
		core.AwaitToolExecutors(app),
		core.MemoryToolExecutors(app),
		core.AgentToolExecutors(app),
		tools.NewOSMGeoExecutors(tools.NewOSMService(tools.OSMConfig{})),
		skills.ToolExecutors(nil),
	} {
		if err := registry.Register(executors...); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.RegisterTools(registry); err != nil {
		t.Fatal(err)
	}
	var definitions []providers.ToolDefinition
	for _, spec := range registry.List() {
		definitions = append(definitions, providers.ToolDefinition{Name: spec.ID, Description: spec.Description, InputSchema: spec.InputJSONSchema})
	}
	return definitions
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/core -run '^(TestReadonlyChildrenRunTogetherWithReadOnlyTools|TestReadonlyChildCannotUseAMutatingTool|TestBackgroundChildrenAreLimitedPerSession|TestOnlyChildrenWritingTheParentsDirectoryHoldItsKey|TestBlockingSubagentRunsUnderASingleModelSlot|TestBlockingSubagentReturnsChildSummaryToParent|TestSubagentSummaryJoinsAReplyCutByTheOutputLimit|TestSubagentStoppedAtItsBudgetReportsAPartialResult|TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate|TestInterruptedParentAndBlockingChildAreBothRescheduledAndComplete|TestCancelParentCancelsItsBlockingSubagent|TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp|TestDelegatedChildChangesTheParentsDirectoryWithoutDeadlock)$'
```

Expected: build fails (`core.AgentToolExecutors`, `core.AgentInput`, `WithBackgroundAgents` undefined).

- [ ] **Step 3: Implement**

In `internal/core/agent_prompts.go`, replace `func (Core) nativeSystemPrompt` with:

```go
// nativeSystemPrompt is the part of the prompt that stays fixed for a run.
func (c *Core) nativeSystemPrompt(ctx context.Context, turn nativeTurn, assistant AssistantProfile, memory string, history []transcript.Message) string {
	sections := []string{prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)}
	workingDir := strings.TrimSpace(turn.WorkingDir)
	if turn.Subagent {
		sections = append(sections, subagentSystemPrompt(turn.Readonly))
		if workingDir != "" {
			sections = append(sections, prompt.ProjectRoot(workingDir))
		}
		return prompt.JoinSections(sections...)
	}
	if turn.ToolUse && clientSupportsVoiceDelivery(turn.ClientCapabilities) {
		sections = append(sections, prompt.VoiceOutputGuidance())
	}
	if turn.ToolUse && clientSupportsDocumentDelivery(turn.ClientCapabilities) && c.fileDeliveryPromptAvailable() {
		sections = append(sections, prompt.FileDeliveryGuidance())
	}
	if turn.ToolUse && c.telephonyCallPromptAvailable() {
		sections = append(sections, prompt.TelephonyCallGuidance())
	}
	if turn.ToolUse {
		sections = append(sections, prompt.ToolUseDiscipline())
	}
	if workingDir != "" {
		sections = append(sections, prompt.ProjectRoot(workingDir))
	}
	if c.webResearchPromptAvailable() {
		sections = append(sections, prompt.WebResearchGuidance())
	}
	if c.agentPromptAvailable() {
		sections = append(sections, c.agentGuidancePrompt(ctx))
	}
	sections = append(sections, memory)
	if skillsPrompt := c.nativeSkillsPrompt(ctx, turn, history); skillsPrompt != "" {
		sections = append(sections, skillsPrompt)
	}
	return prompt.JoinSections(sections...)
}
```

In `internal/core/agent_prompts.go`, add:

```go
func (c *Core) agentPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec(agentToolName)
	return ok
}
```

In `internal/core/agent_prompts.go`, delete `func (Core) delegateTaskPromptAvailable`.

In `internal/core/agent_tools.go`, replace `type nativeTurn` with:

```go
// nativeTurn is what the per-step prompt and tool list of a native run depend
// on; Continues lists the runs this one continues, latest first.
type nativeTurn struct {
	RunID              string
	Continues          []string
	SessionID          string
	WorkingDir         string
	Subagent           bool
	Readonly           bool
	ClientCapabilities ClientCapabilities
	ToolUse            bool
}
```

In `internal/core/agent_tools.go`, replace `func (coreTools) Specs` with:

```go
func (t coreTools) Specs(ctx context.Context) []tools.Spec {
	specs := t.c.nativeToolSpecs(t.turn)
	for i := range specs {
		if specs[i].ID == agentToolName {
			specs[i].Description = t.c.agentToolDescription(ctx, specs[i].Description)
		}
	}
	return specs
}
```

In `internal/core/agent_tools.go`, replace `func (coreTools) Authorize` with:

```go
// Authorize rejects invalid calls and those a deny rule blocks; Execute acts on
// the verdict it kept for the call. A mutating call is a barrier unless a rule or
// the mode allows it, as it then cannot ask; a child writing the parent's
// directory still can. A blocking child's time is its own.
func (t coreTools) Authorize(ctx context.Context, name string, call tools.Call) (agent.Decision, error) {
	// A call ID owned by another session fails the run with a clear error instead of
	// a primary-key conflict on the first journal write.
	if _, err := t.c.isNewToolCallMessage(ctx, call.SessionID, call.ToolCallID); err != nil {
		return agent.Decision{}, err
	}
	session, spec, err := t.c.checkToolCall(ctx, call.SessionID, name)
	if errors.Is(err, ErrInvalidInput) {
		return agent.Decision{Reason: err.Error()}, nil
	}
	if err != nil {
		return agent.Decision{}, err
	}
	if call.WorkingDir = normalizeWorkingDir(call.WorkingDir); call.WorkingDir == "" {
		call.WorkingDir = session.WorkingDir
	}
	t.authorized.Delete(call.ToolCallID)
	check, err := t.c.checkPermission(ctx, call.SessionID, spec, call)
	if err != nil {
		return agent.Decision{}, err
	}
	if check.verdict.Effect == permission.Deny {
		return agent.Decision{Reason: blockedResult(check.verdict.Rule).Content}, nil
	}
	if call.ToolCallID != "" {
		t.authorized.Store(call.ToolCallID, check)
	}
	decision := agent.Decision{Allowed: true, Barrier: spec.Mutates() && check.verdict.Effect != permission.Allow, Key: t.c.tools.ConcurrencyKey(spec.ID, call)}
	if spec.ID == agentToolName {
		input := parseAgentInput(call.Args)
		decision.Barrier = agentCallWritesSharedDir(input)
		decision.Delegated = !input.Background
	}
	return decision, nil
}
```

In `internal/core/agent_tools.go`, replace `func (Core) nativeToolSpecs` with:

```go
// nativeToolSpecs lists the tools a native run may see; nil without a registry.
func (c *Core) nativeToolSpecs(turn nativeTurn) []tools.Spec {
	if c.tools == nil {
		return nil
	}
	specs := c.tools.List()
	out := make([]tools.Spec, 0, len(specs))
	for _, spec := range specs {
		if turn.Subagent && !subagentToolAllowed(spec) || turn.Readonly && spec.Mutates() {
			continue
		}
		if spec.ID == "text_to_speech" && !clientSupportsVoiceDelivery(turn.ClientCapabilities) {
			continue
		}
		if spec.ID == "send_file" && !clientSupportsDocumentDelivery(turn.ClientCapabilities) {
			continue
		}
		out = append(out, spec)
	}
	return out
}
```

In `internal/core/core.go`, replace `type Core` with:

```go
type Core struct {
	mu              sync.RWMutex
	store           Store
	workStore       work.Store
	runStarter      RunStarter
	llms            SessionLLMRegistry
	assistant       AssistantProfile
	attachments     agentcontext.AttachmentReader
	externalAgents  *externalagents.Registry
	externalStore   externalagents.AttachmentStore
	activeRuns      map[string]*activeRun
	scheduledRuns   map[string]time.Time
	sessionGates    map[string]*sync.Mutex
	tools           ToolExecutor
	skillsContext   SkillsPromptContextProvider
	runtimeStatus   RuntimeStatusContextProvider
	events          *eventBus
	now             func() time.Time
	newID           func(prefix string) string
	historyLimit    int
	lifetime        context.Context
	budgets         RunBudgets
	sessionFiles    string
	compactProvider string
	compactModel    string
	windowCap       int
	// modelSlots bounds the model requests of all native runs at once.
	modelSlots *toolsched.Semaphore
	// backgroundAgents bounds the background subagents of one session.
	backgroundAgents int
	// toolLocks serialises tool calls sharing a concurrency key across runs.
	toolLocks *toolsched.Locks
	// compactUnavailable is set while the compact model cannot be resolved,
	// so the failure is logged once.
	compactUnavailable atomic.Bool

	// badBoundaries holds the IDs of unreadable boundaries already logged.
	badBoundaries sync.Map
	// liveTasks are the shell tasks this daemon started that still run.
	tasksMu   sync.Mutex
	liveTasks map[string]*liveTask
}
```

In `internal/core/core.go`, replace `func New` with:

```go
func New(store Store) *Core {
	return &Core{
		store:            store,
		activeRuns:       map[string]*activeRun{},
		scheduledRuns:    map[string]time.Time{},
		sessionGates:     map[string]*sync.Mutex{},
		liveTasks:        map[string]*liveTask{},
		events:           newEventBus(),
		now:              time.Now,
		newID:            defaultID,
		historyLimit:     50,
		lifetime:         context.Background(),
		budgets:          DefaultRunBudgets(),
		modelSlots:       toolsched.NewSemaphore(DefaultModelConcurrency),
		backgroundAgents: DefaultBackgroundAgents,
		toolLocks:        toolsched.NewLocks(),
	}
}
```

In `internal/core/run_execute.go`, replace `func (Core) nativeEngine` with:

```go
// nativeEngine builds the engine and task of one native run; the task resumes the
// counters of the run's checkpoint, or else those the session's last run carried.
func (c *Core) nativeEngine(ctx context.Context, run Run, session Session, runtime providers.Runtime) (agent.Task, *agent.Engine, error) {
	resume, err := c.resumeCounters(ctx, run.ID)
	if err == nil && resume == (agent.Counters{}) {
		resume, err = c.carriedCounters(ctx, session.ID)
	}
	if err != nil {
		return agent.Task{}, nil, err
	}
	budget, err := c.runBudget(ctx, run, session)
	if err != nil {
		return agent.Task{}, nil, err
	}
	continues, err := c.continuedRuns(ctx, run)
	if err != nil {
		return agent.Task{}, nil, err
	}
	readonly, err := c.readonlySubagent(ctx, session.ID)
	if err != nil {
		return agent.Task{}, nil, err
	}
	turn := nativeTurn{
		RunID:              run.ID,
		Continues:          continues,
		SessionID:          session.ID,
		WorkingDir:         session.WorkingDir,
		Subagent:           isSubagentSession(session),
		Readonly:           readonly,
		ClientCapabilities: run.ClientCapabilities,
		ToolUse:            agent.ToolUseAllowed(runtime),
	}
	engine := agent.New(agent.Config{
		Journal:     coreJournal{c: c},
		Tools:       coreTools{c: c, turn: turn, authorized: &sync.Map{}},
		Approvals:   coreApprovals{c: c, sessionID: session.ID},
		Inbox:       coreInbox{c: c, session: session},
		Sink:        coreSink{c: c},
		Prompts:     &corePrompts{c: c, turn: turn},
		Todos:       coreTodos{c: c},
		Attachments: c.attachments,
		Now:         func() time.Time { return c.now().UTC() },
		NewID:       c.newID,
		ModelSlots:  c.modelSlots,
		Locks:       c.toolLocks,
	})
	task := agent.Task{
		RunID:        run.ID,
		SessionID:    session.ID,
		Client:       run.Client,
		ExternalKey:  run.ExternalKey,
		WorkingDir:   session.WorkingDir,
		Model:        runtime,
		WindowTokens: c.sessionContextWindowTokens(session),
		Budget:       budget,
		Resume:       resume,
		Continues:    continues,
	}
	if compact, windowTokens := c.compactRuntime(ctx); compact != nil {
		task.CompactModel, task.CompactWindowTokens = compact, windowTokens
	}
	return task, engine, nil
}
```

In `internal/core/subagents.go`, replace `func (Core) resumeSubagentTask` with:

```go
func (c *Core) resumeSubagentTask(ctx context.Context, task SubagentTask) (AgentResult, error) {
	if task.Status == TaskStatusCompleted {
		return AgentResult{Task: task, Summary: task.Summary}, nil
	}
	if task.Status == TaskStatusFailed && task.FinishedAt != nil {
		summary := strings.TrimSpace(task.Summary)
		if summary == "" {
			summary = strings.TrimSpace(task.Error)
		}
		return AgentResult{Task: task, Summary: summary, IsError: true}, nil
	}
	return c.finishOrBridgeSubagentTask(ctx, task, nil)
}
```

In `internal/core/subagents.go`, replace `func (Core) finishOrBridgeSubagentTask` with:

```go
func (c *Core) finishOrBridgeSubagentTask(ctx context.Context, task SubagentTask, execErr error) (AgentResult, error) {
	run, err := c.store.GetRun(ctx, task.ChildRunID)
	if err != nil {
		if execErr != nil {
			return c.finishSubagentTask(ctx, task, "Subagent failed: "+execErr.Error(), true)
		}
		return AgentResult{}, err
	}
	if execErr == nil && run.Status == RunStatusWaitingApproval {
		approval, err := c.pendingApprovalForRun(ctx, task.ChildSessionID, task.ChildRunID)
		if err == nil {
			return c.bridgeSubagentApproval(ctx, task, approval)
		}
		if !errors.Is(err, ErrNotFound) {
			return AgentResult{}, err
		}
	}
	if execErr == nil && !subagentRunStatusTerminal(run.Status) {
		if err := c.waitForSubagentStep(ctx, task); err != nil {
			return AgentResult{}, err
		}
		return c.finishOrBridgeSubagentTask(ctx, task, nil)
	}
	summary, failed := c.subagentRunSummary(ctx, task.ChildSessionID, task.ChildRunID, execErr)
	return c.finishSubagentTask(ctx, task, summary, failed)
}
```

In `internal/core/subagents.go`, replace `func (Core) bridgeSubagentApproval` with:

```go
func (c *Core) bridgeSubagentApproval(ctx context.Context, task SubagentTask, approval Approval) (AgentResult, error) {
	task, err := c.markSubagentTaskWaitingApproval(ctx, task)
	if err != nil {
		return AgentResult{}, err
	}
	request, err := c.subagentApprovalRequest(ctx, task, approval)
	if err != nil {
		return AgentResult{}, err
	}
	return AgentResult{
		Task:     task,
		Summary:  "Subagent is waiting for permission.",
		Approval: request,
	}, nil
}
```

In `internal/core/subagents.go`, replace `func (Core) finishSubagentTask` with:

```go
func (c *Core) finishSubagentTask(ctx context.Context, task SubagentTask, summary string, failed bool) (AgentResult, error) {
	status := TaskStatusCompleted
	errText := ""
	if failed {
		status = TaskStatusFailed
		errText = summary
	}
	task, err := c.finishSubagentTaskRecord(ctx, task, status, summary, errText, false)
	if err != nil {
		return AgentResult{}, err
	}
	return AgentResult{Task: task, Summary: summary, IsError: failed}, nil
}
```

In `internal/core/subagents.go`, replace `func (Core) subagentApprovalRequest` with:

```go
func (c *Core) subagentApprovalRequest(ctx context.Context, task SubagentTask, childApproval Approval) (*tools.ApprovalRequest, error) {
	child, err := c.store.GetSession(ctx, task.ChildSessionID)
	if err != nil {
		return nil, err
	}
	params := subagentApprovalBridgeParams{
		Source:              subagentApprovalBridgeSource,
		TaskID:              task.ID,
		ChildSessionID:      task.ChildSessionID,
		ChildRunID:          task.ChildRunID,
		ChildApprovalID:     childApproval.ID,
		ChildToolCallID:     childApproval.ToolCallRef,
		ChildToolName:       childApproval.ToolName,
		SubagentTitle:       firstNonEmpty(strings.TrimSpace(task.AgentName), firstNonEmpty(strings.TrimSpace(task.DisplayName), strings.TrimSpace(child.Title))),
		Runtime:             task.Runtime,
		OriginalAction:      childApproval.Action,
		OriginalDescription: childApproval.Description,
		OriginalParams:      childApproval.Params,
	}
	description := fmt.Sprintf("Subagent %q requested approval for %s", firstNonEmpty(subagentTaskAgentName(task), firstNonEmpty(child.Title, task.Runtime)), firstNonEmpty(childApproval.ToolName, "a tool"))
	if detail := strings.TrimSpace(childApproval.Description); detail != "" {
		description += ": " + detail
	}
	return &tools.ApprovalRequest{
		ToolID:      agentToolName,
		ToolCallID:  task.ParentToolCallID,
		Action:      childApproval.Action,
		Path:        childApproval.Path,
		Description: description,
		Params:      params,
		Suggestion:  childApproval.Suggestion,
	}, nil
}
```

In `internal/core/subagents.go`, replace `func (Core) mirrorPendingSubagentApproval` with:

```go
// mirrorPendingSubagentApproval asks the parent for the child's pending approval.
// It reports false when the child has none: its approval was just decided and
// the child has not resumed yet.
func (c *Core) mirrorPendingSubagentApproval(ctx context.Context, task SubagentTask) (bool, error) {
	childApproval, err := c.pendingApprovalForRun(ctx, task.ChildSessionID, task.ChildRunID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	existing, err := c.subagentBridgeApprovalForChild(ctx, task, childApproval.ID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(existing.ID) != "" {
		return true, nil
	}
	task, err = c.markSubagentTaskWaitingApproval(ctx, task)
	if err != nil {
		return false, err
	}
	request, err := c.subagentApprovalRequest(ctx, task, childApproval)
	if err != nil {
		return false, err
	}
	prepared := preparedToolCall{
		SessionID:  task.ParentSessionID,
		RunID:      task.ParentRunID,
		ToolName:   agentToolName,
		ToolCallID: task.ParentToolCallID,
	}
	if _, err := c.requestApproval(ctx, prepared, *request); err != nil {
		return true, err
	}
	if strings.TrimSpace(task.ParentRunID) == "" {
		return true, nil
	}
	run, err := c.store.GetRun(ctx, task.ParentRunID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	if subagentRunStatusTerminal(run.Status) {
		return true, nil
	}
	return true, c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
}
```

In `internal/core/subagents.go`, add:

```go
// DefaultBackgroundAgents is how many background subagents one session may run
// at once unless the daemon configures another limit.
const DefaultBackgroundAgents = 4
```

In `internal/core/subagents.go`, add:

```go
// WithBackgroundAgents bounds the background subagents one session runs at
// once; 0 or less keeps the default.
func (c *Core) WithBackgroundAgents(n int) *Core {
	if n <= 0 {
		n = DefaultBackgroundAgents
	}
	c.backgroundAgents = n
	return c
}
```

In `internal/core/subagents.go`, add:

```go
// AgentInput is one agent tool call: a child agent on a bounded task.
type AgentInput struct {
	ParentSessionID  string
	ParentRunID      string
	ParentToolCallID string
	Description      string
	Prompt           string
	Background       bool
	Isolation        string
	Readonly         bool
	Runtime          string
	Model            string
}
```

In `internal/core/subagents.go`, add:

```go
// AgentResult is how a blocking child ended, or the task a background child
// runs as; Approval asks the parent for a child's pending approval.
type AgentResult struct {
	Task     SubagentTask
	Summary  string
	IsError  bool
	Approval *tools.ApprovalRequest
	Replayed bool
}
```

In `internal/core/subagents.go`, add:

```go
// RunAgent runs a child agent for a parent's agent call. A blocking child runs
// inside the call and its result is the call's; a background child becomes a
// task whose result reaches the parent as an event. A repeated call resumes
// the child it started.
func (c *Core) RunAgent(ctx context.Context, input AgentInput) (AgentResult, error) {
	prompt := normalizeText(input.Prompt)
	if prompt == "" {
		return AgentResult{}, fmt.Errorf("%w: prompt is required", ErrInvalidInput)
	}
	parent, err := c.store.GetSession(ctx, normalizeText(input.ParentSessionID))
	if err != nil {
		return AgentResult{}, err
	}
	parent = c.decorateSessionLLM(parent)
	if CoreSessionIsExternalAgent(parent) {
		return AgentResult{}, fmt.Errorf("%w: the agent tool is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if isSubagentSession(parent) {
		return AgentResult{}, fmt.Errorf("%w: child subagents cannot start agents", ErrInvalidInput)
	}
	parentRunID := normalizeText(input.ParentRunID)
	parentToolCallID := normalizeText(input.ParentToolCallID)
	if parentRunID != "" && parentToolCallID != "" {
		existing, err := c.store.GetSubagentTaskByParentToolCall(ctx, parent.ID, parentRunID, parentToolCallID)
		switch {
		case err == nil && existing.Mode == SubagentTaskModeAsync:
			return AgentResult{Task: existing, Replayed: true}, nil
		case err == nil:
			return c.resumeSubagentTask(ctx, existing)
		case !errors.Is(err, ErrNotFound):
			return AgentResult{}, err
		}
	}
	if input.Background {
		active, err := c.store.ListActiveSubagentTasksByParent(ctx, parent.ID)
		if err != nil {
			return AgentResult{}, err
		}
		if len(active) >= c.backgroundAgents {
			return AgentResult{}, fmt.Errorf("%w: at most %d background subagents run at once in a session; await one of them first", ErrInvalidInput, c.backgroundAgents)
		}
	}
	task, run, err := c.startSubagent(ctx, parent, input, prompt, parentRunID, parentToolCallID)
	if err != nil {
		return AgentResult{}, err
	}
	if input.Background {
		if err := c.startRun(ctx, run.ID); err != nil {
			summary := "Subagent failed to start: " + err.Error()
			_, _ = c.finishSubagentTaskRecord(ctx, task, TaskStatusFailed, summary, summary, false)
			return AgentResult{}, err
		}
		return AgentResult{Task: task}, nil
	}
	execErr := c.ExecuteRun(ctx, run.ID)
	return c.finishOrBridgeSubagentTask(ctx, task, execErr)
}
```

In `internal/core/subagents.go`, add:

```go
// startSubagent creates the child session, its run and the task that links
// them to the parent's call; a worktree child gets its own git worktree.
func (c *Core) startSubagent(ctx context.Context, parent Session, input AgentInput, prompt string, parentRunID string, parentToolCallID string) (SubagentTask, Run, error) {
	runtime := normalizeSubagentRuntime(input.Runtime)
	isolation := normalizeSubagentIsolation(input.Isolation)
	if input.Readonly {
		isolation = SubagentIsolationShared
	}
	taskID := c.newID("task")
	workingDir := parent.WorkingDir
	if isolation == SubagentIsolationWorktree {
		dir, err := prepareSubagentWorktree(ctx, workingDir, taskID)
		if err != nil {
			return SubagentTask{}, Run{}, err
		}
		workingDir = dir
	}
	// Children started at once by one reply get different names.
	gate := c.sessionGate(parent.ID)
	gate.Lock()
	defer gate.Unlock()
	agentName, err := c.assignSubagentAgentName(ctx, parent.ID)
	if err != nil {
		return SubagentTask{}, Run{}, err
	}
	child, err := c.createSubagentSession(ctx, parent, runtime, input.Model, workingDir, agentName, input.Readonly)
	if err != nil {
		return SubagentTask{}, Run{}, err
	}
	run, err := c.createSubagentRun(ctx, child, subagentUserPrompt(prompt, workingDir, isolation, input.Readonly))
	if err != nil {
		return SubagentTask{}, Run{}, err
	}
	mode := SubagentTaskModeBlocking
	if input.Background {
		mode = SubagentTaskModeAsync
	}
	now := c.now().UTC()
	task := SubagentTask{
		ID:               taskID,
		AgentName:        agentName,
		DisplayName:      subagentDisplayName(input.Description, prompt),
		Mode:             mode,
		Isolation:        isolation,
		Readonly:         input.Readonly,
		ParentSessionID:  parent.ID,
		ParentRunID:      parentRunID,
		ParentToolCallID: parentToolCallID,
		ChildSessionID:   child.ID,
		ChildRunID:       run.ID,
		Runtime:          subagentTaskRuntimeLabel(runtime, child),
		Model:            normalizeText(input.Model),
		Goal:             prompt,
		Status:           TaskStatusRunning,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := c.createSubagentTaskRecord(ctx, task); err != nil {
		return SubagentTask{}, Run{}, err
	}
	return task, run, nil
}
```

In `internal/core/subagents.go`, delete `func (Core) DelegateTask`.

In `internal/core/subagents.go`, delete `type DelegateTaskInput`.

In `internal/core/subagents.go`, delete `type DelegateTaskResult`.

Delete `internal/core/subagents_async.go`: `git rm internal/core/subagents_async.go`.

In `internal/core/subagents_lifecycle.go`, replace `func (Core) publishSubagentToolUpdate` with:

```go
// publishSubagentToolUpdate tells clients the parent's agent call finished;
// the result itself reaches the parent through the completion run.
func (c *Core) publishSubagentToolUpdate(task SubagentTask) {
	resultMessageID := normalizeText(task.ResultMessageID)
	if resultMessageID == "" {
		return
	}
	c.publishToolUpdate(task.ParentSessionID, task.ParentRunID, ToolUpdate{
		ToolCallID:      task.ParentToolCallID,
		ToolName:        agentToolName,
		State:           subagentTaskToolLifecycleState(task),
		ResultStatus:    string(subagentTaskToolResultStatus(task)),
		RunID:           task.ParentRunID,
		SessionID:       task.ParentSessionID,
		ResultMessageID: resultMessageID,
		Error:           task.Error,
	})
}
```

In `internal/core/subagents_presentation.go`, replace `func subagentUserPrompt` with:

```go
// subagentUserPrompt is a child's assignment: the parent's prompt, where it
// works and what it may change.
func subagentUserPrompt(prompt string, workingDir string, isolation SubagentIsolation, readonly bool) string {
	lines := []string{"Delegated task:", strings.TrimSpace(prompt)}
	if workingDir = strings.TrimSpace(workingDir); workingDir != "" {
		lines = append(lines, "", "Working directory:", workingDir)
	}
	switch {
	case readonly:
		lines = append(lines, "", "Read-only: inspect and report; do not change files.")
	case isolation == SubagentIsolationWorktree:
		lines = append(lines, "", "This is your own git worktree; keep your changes inside it. The parent merges them.")
	}
	lines = append(lines, "", "Return a concise result for the parent agent. Include important files, findings, errors, and verification output. Do not ask the user questions.")
	return strings.Join(lines, "\n")
}
```

In `internal/core/subagents_presentation.go`, replace `func subagentSystemPrompt` with:

```go
func subagentSystemPrompt(readonly bool) string {
	lines := []string{
		"Subagent mode:",
		"- You are a child agent working for a parent Matrixclaw agent.",
		"- Complete only the delegated task from the user message.",
		"- Track multi-step work with todo_write.",
		"- Commands you start in the background are stopped when you finish.",
		"- Do not ask the user for input or approval.",
		"- Return a concise summary for the parent agent, listing important files, findings, errors, and verification output.",
	}
	if readonly {
		lines = append(lines, "- You have read-only tools: inspect, do not change anything.")
	}
	return strings.Join(lines, "\n")
}
```

In `internal/core/subagents_presentation.go`, replace `func subagentToolAllowed` with:

```go
// subagentToolAllowed says whether children get a tool: not the agent tool,
// await, memory, voice or other automation, storage and skill tools.
func subagentToolAllowed(spec tools.Spec) bool {
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	if id == todo.ToolName {
		return true
	}
	if id == "memory" || id == "text_to_speech" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(spec.Namespace)) == "core.memory" {
		return false
	}
	switch spec.Category {
	case tools.CategoryAutomation, tools.CategoryStorage, tools.CategorySkills:
		return false
	}
	return true
}
```

In `internal/core/subagents_presentation.go`, add:

```go
func agentResultContent(result AgentResult) string {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = "Subagent completed without a text summary."
	}
	return summary
}
```

In `internal/core/subagents_presentation.go`, add:

```go
func agentResultStatus(result AgentResult) tools.ResultStatus {
	if result.IsError {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}
```

In `internal/core/subagents_presentation.go`, add:

```go
// subagentDisplayName is the child's label: the call's description, or the
// start of its prompt.
func subagentDisplayName(description string, prompt string) string {
	if description = strings.Join(strings.Fields(description), " "); description != "" {
		return truncateForTitle(description, 48)
	}
	return truncateForTitle(prompt, 48)
}
```

In `internal/core/subagents_presentation.go`, add:

```go
// backgroundAgentContent tells the model the background task a child runs as.
func backgroundAgentContent(result AgentResult) string {
	task := result.Task
	state := "started"
	switch {
	case result.Replayed && taskStatusTerminal(task.Status):
		state = "already finished"
	case result.Replayed:
		state = "already running"
	}
	return fmt.Sprintf("Subagent %s %s as background task %s. Its result arrives as a message when it finishes; wait for it with await, or go on with other work.", subagentTaskAgentName(task), state, task.ID)
}
```

In `internal/core/subagents_presentation.go`, delete `func (Core) subagentTaskDetail`.

In `internal/core/subagents_presentation.go`, delete `func asyncSubagentUserPrompt`.

In `internal/core/subagents_presentation.go`, delete `func delegateTaskResultContent`.

In `internal/core/subagents_presentation.go`, delete `func delegateTaskResultStatus`.

In `internal/core/subagents_presentation.go`, delete `func formatSubagentTaskList`.

In `internal/core/subagents_presentation.go`, delete `func generatedSubagentDisplayName`.

In `internal/core/subagents_presentation.go`, delete `func normalizeSubagentDisplayName`.

In `internal/core/subagents_presentation.go`, delete `func spawnSubagentResultContent`.

In `internal/core/subagents_presentation.go`, delete `func subagentParentToolName`.

In `internal/core/subagents_prompt.go`, add:

```go
func (c *Core) agentGuidancePrompt(ctx context.Context) string {
	lines := []string{
		"Subagents:",
		"- The agent tool runs a child agent on a bounded task. Give it a short description and a prompt with everything it needs: the goal, the context and what to report back. It sees nothing of this conversation.",
		"- Use readonly:true for research, reviews and questions: the child gets read-only tools, and read-only children in one reply run in parallel.",
		"- A child that changes files works in your directory (isolation shared, one at a time) or in its own git worktree (isolation worktree, several at once; you merge their work).",
		"- Without background the call returns the child's result, and several such calls in one reply run together; their time does not count against your budget.",
		fmt.Sprintf("- With background:true the call returns a task id at once and the result arrives as a message when the child finishes; wait for it with await. At most %d background children run at once.", c.backgroundAgents),
		"- Children cannot start agents or await; they may use todo_write and background commands. Results return to you, the parent agent.",
		"- Available runtime configuration:",
	}
	runtimes := c.subagentRuntimeInfo(ctx)
	available := 0
	for _, runtime := range runtimes {
		if runtime.Available {
			available++
		}
		lines = append(lines, "- "+runtime.PromptLine())
	}
	if ids := availableSubagentRuntimeIDs(runtimes); len(ids) > 0 {
		lines = append(lines,
			"- Runtime IDs available for the agent tool: "+strings.Join(ids, ", ")+".",
			"- When asked which subagent runtimes are available, answer from that Runtime IDs list and include the native matrixclaw runtime.",
			"- Treat user questions in any language about available or connected subagents as questions about that Runtime IDs list, not only about your base model/provider.",
			"- For the current configuration, if asked which subagents or subagent runtimes are available or connected, answer exactly: "+strings.Join(ids, ", ")+".",
		)
	}
	lines = append(lines,
		"- Do not select unavailable runtimes.",
		"- If the user names an unavailable runtime, say it is unavailable and offer the available alternatives.",
	)
	if available <= 1 {
		lines = append(lines, "- If the user asks for a subagent without naming a runtime, use matrixclaw and do not ask which runtime.")
	} else {
		lines = append(lines, "- If the user asks for a subagent without naming a runtime, ask the user which runtime to use unless the request itself makes the runtime obvious.")
	}
	return strings.Join(lines, "\n")
}
```

In `internal/core/subagents_prompt.go`, add:

```go
func (c *Core) agentToolDescription(ctx context.Context, base string) string {
	runtimes := c.subagentRuntimeInfo(ctx)
	if len(runtimes) == 0 {
		return base
	}
	lines := []string{strings.TrimSpace(base), "Runtime choices:"}
	for _, runtime := range runtimes {
		lines = append(lines, "- "+runtime.PromptLine())
	}
	return strings.Join(lines, "\n")
}
```

In `internal/core/subagents_prompt.go`, delete `func (Core) delegateTaskGuidancePrompt`.

In `internal/core/subagents_prompt.go`, delete `func (Core) delegateTaskToolDescription`.

In `internal/core/subagents_runtime.go`, the imports become:

```go
import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)
```

In `internal/core/subagents_runtime.go`, replace `func (Core) createSubagentSession` with:

```go
// createSubagentSession creates a child's hidden session; a read-only external
// child runs in the default mode, its runtime's read-only sandbox.
func (c *Core) createSubagentSession(ctx context.Context, parent Session, runtime SubagentRuntime, model string, workingDir string, displayName string, readonly bool) (Session, error) {
	title := "Subagent: " + truncateForTitle(firstNonEmpty(displayName, "Task"), 64)
	switch runtime {
	case SubagentRuntimeCodex, SubagentRuntimeClaude:
		agentID := string(runtime)
		canonical, ok := c.ResolveExternalAgentID(agentID)
		if !ok {
			return Session{}, fmt.Errorf("%w: external agent %q is not configured", ErrExecutionUnavailable, agentID)
		}
		mode := PermissionModeFullAuto
		if readonly {
			mode = PermissionModeDefault
		}
		return c.CreateSession(ctx, CreateSessionInput{
			Title:           title,
			Kind:            SessionKindExternalAgent,
			RuntimeID:       SessionRuntimeExternalAgent,
			ParentSessionID: parent.ID,
			Hidden:          true,
			WorkingDir:      workingDir,
			ModelID:         normalizeText(model),
			PermissionMode:  mode,
			ExternalAgentID: canonical,
		})
	default:
		return c.CreateSession(ctx, CreateSessionInput{
			Title:           title,
			Kind:            SessionKindAssistant,
			RuntimeID:       SessionRuntimeMatrixClaw,
			ParentSessionID: parent.ID,
			Hidden:          true,
			WorkingDir:      workingDir,
			ProviderID:      parent.ProviderID,
			ModelID:         firstNonEmpty(normalizeText(model), parent.ModelID),
			PermissionMode:  parent.PermissionMode,
		})
	}
}
```

In `internal/core/subagents_runtime.go`, add:

```go
// readonlySubagent reports whether the child session was started read-only.
func (c *Core) readonlySubagent(ctx context.Context, sessionID string) (bool, error) {
	task, err := c.store.GetSubagentTaskByChildSession(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return task.Readonly, err
}
```

Replace the whole of `internal/core/subagents_tools.go` with:

```go
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const agentToolName = "agent"

type agentToolInput struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Background  bool   `json:"background,omitempty"`
	Isolation   string `json:"isolation,omitempty"`
	Readonly    bool   `json:"readonly,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	Model       string `json:"model,omitempty"`
}

type agentTool struct {
	app *Core
}

// AgentToolExecutors returns the agent tool.
func AgentToolExecutors(app *Core) []tools.Executor {
	return []tools.Executor{&agentTool{app: app}}
}

func (t *agentTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              agentToolName,
		Name:            "Agent",
		Description:     "Run a child agent on a bounded task and get its result, or start it in the background.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectMutation,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.subagents",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileCoding},
		OutputKind:      tools.OutputText,
		InputJSONSchema: agentToolSchema,
	}
}

// ConcurrencyKey lets one child at a time change the parent's directory; it is
// not the directory's own key, which the child's calls take while this call
// holds its key. Read-only, worktree and background children need none.
func (t *agentTool) ConcurrencyKey(call tools.Call) string {
	input := parseAgentInput(call.Args)
	if !agentCallWritesSharedDir(input) {
		return ""
	}
	return "subagents:" + filepath.Clean(call.WorkingDir)
}

// agentCallWritesSharedDir reports whether the call's child runs inside the
// call and may change the parent's working directory.
func agentCallWritesSharedDir(input agentToolInput) bool {
	return !input.Background && !input.Readonly && normalizeSubagentIsolation(input.Isolation) == SubagentIsolationShared
}

func parseAgentInput(args json.RawMessage) agentToolInput {
	var input agentToolInput
	_ = json.Unmarshal(args, &input)
	return input
}

func (t *agentTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if t == nil || t.app == nil {
		return tools.Result{}, fmt.Errorf("%w: agent core unavailable", ErrExecutionUnavailable)
	}
	var input agentToolInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{}, tools.InvalidArgs(agentToolName, err)
	}
	result, err := t.app.RunAgent(ctx, AgentInput{
		ParentSessionID:  call.SessionID,
		ParentRunID:      call.RunID,
		ParentToolCallID: call.ToolCallID,
		Description:      input.Description,
		Prompt:           input.Prompt,
		Background:       input.Background,
		Isolation:        input.Isolation,
		Readonly:         input.Readonly,
		Runtime:          input.Runtime,
		Model:            input.Model,
	})
	if err != nil {
		return tools.Result{}, err
	}
	if result.Task.Mode == SubagentTaskModeAsync {
		return tools.Result{Content: backgroundAgentContent(result), Metadata: result.Task, Status: tools.ResultStatusNeutral}, nil
	}
	out := tools.Result{Content: agentResultContent(result), Metadata: result.Task, IsError: result.IsError, Status: agentResultStatus(result)}
	if result.Approval != nil {
		out.Approval = result.Approval
	}
	return out, nil
}

var agentToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "description": {"type": "string", "description": "A short label for the task, 3-5 words."},
    "prompt": {"type": "string", "description": "Everything the child needs: the goal, the context and what to report back."},
    "background": {"type": "boolean", "description": "Start the child as a background task and go on; its result arrives as a message when it finishes."},
    "isolation": {"type": "string", "enum": ["shared", "worktree"], "description": "shared works in your directory, one writing child at a time; worktree gives the child its own git worktree, so several run at once."},
    "readonly": {"type": "boolean", "description": "Give the child read-only tools; read-only children run in parallel."},
    "runtime": {"type": "string", "enum": ["matrixclaw", "codex", "claude", "auto"], "description": "Child runtime. Defaults to matrixclaw."},
    "model": {"type": "string", "description": "Optional model for the child runtime."}
  },
  "required": ["description", "prompt"],
  "additionalProperties": false
}`)
```

In `internal/core/tool_call_prepare.go`, replace `func (Core) checkToolCall` with:

```go
// checkToolCall validates a tool call against its session before anything is written;
// ErrInvalidInput errors are returned to the model, others fail the run.
func (c *Core) checkToolCall(ctx context.Context, sessionID string, toolName string) (Session, tools.Spec, error) {
	if c.tools == nil {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tools are not configured", ErrExecutionUnavailable)
	}
	sessionID = normalizeText(sessionID)
	toolName = normalizeText(toolName)
	if sessionID == "" {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: session_id is required", ErrInvalidInput)
	}
	if toolName == "" {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tool_name is required", ErrInvalidInput)
	}
	spec, ok := c.tools.Spec(toolName)
	if !ok {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: unknown tool %q", ErrInvalidInput, toolName)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, tools.Spec{}, err
	}
	if toolName == agentToolName && CoreSessionIsExternalAgent(session) {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: the agent tool is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if !isSubagentSession(session) {
		return session, spec, nil
	}
	if !subagentToolAllowed(spec) {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tool %q is not available to child subagents", ErrInvalidInput, toolName)
	}
	if spec.Mutates() {
		readonly, err := c.readonlySubagent(ctx, session.ID)
		if err != nil {
			return Session{}, tools.Spec{}, err
		}
		if readonly {
			return Session{}, tools.Spec{}, fmt.Errorf("%w: tool %q changes things, and this subagent is read-only", ErrInvalidInput, toolName)
		}
	}
	return session, spec, nil
}
```

In `internal/daemoncmd/bootstrap.go`, replace `type bootstrapConfig` with:

```go
type bootstrapConfig struct {
	Addr           string
	DBPath         string
	SessionLLMs    core.SessionLLMRegistry
	Assistant      core.AssistantProfile
	SetupService   *setup.Service
	SetupPath      string
	Timezone       string
	APIToken       string
	Clients        map[string]setup.ClientBootstrap
	ExternalAgents setup.ModulesConfig
	Budgets        core.RunBudgets
	CompactModel   setup.CompactModelConfig
	WindowCap      int
	// ModelConcurrency bounds concurrent model requests; 0 keeps core's default.
	ModelConcurrency int
	// BackgroundAgents bounds a session's background subagents; 0 keeps core's default.
	BackgroundAgents int
}
```

In `internal/daemoncmd/bootstrap.go`, replace `func loadBootstrap` with:

```go
func loadBootstrap() (bootstrapConfig, error) {
	cfg := bootstrapConfig{
		Addr:    defaultDaemonAddr,
		DBPath:  setup.DefaultDBPath(),
		Budgets: core.DefaultRunBudgets(),
	}

	service, err := setup.NewDefaultService()
	if err != nil {
		return bootstrapConfig{}, fmt.Errorf("resolve setup service: %w", err)
	}
	cfg.SetupPath = service.Path()
	cfg.SetupService = service

	setupCfg, err := service.Load()
	switch {
	case errors.Is(err, setup.ErrConfigNotFound):
		return bootstrapConfig{}, fmt.Errorf("%w: run `matrixclaw setup` first (%s)", ErrSetupRequired, service.Path())
	case err != nil:
		return bootstrapConfig{}, fmt.Errorf("load setup config %s: %w", service.Path(), err)
	default:
		if strings.TrimSpace(setupCfg.Daemon.APIToken) == "" {
			setupCfg, err = service.EnsureDaemonAPIToken()
			if err != nil {
				return bootstrapConfig{}, fmt.Errorf("initialize daemon api token %s: %w", service.Path(), err)
			}
		}
		if addr := strings.TrimSpace(setupCfg.Daemon.HTTPAddr); addr != "" {
			cfg.Addr = addr
		}
		if dbPath := strings.TrimSpace(setupCfg.Daemon.DBPath); dbPath != "" {
			cfg.DBPath = dbPath
		}
		cfg.Timezone = strings.TrimSpace(setupCfg.Daemon.Timezone)
		cfg.APIToken = strings.TrimSpace(setupCfg.Daemon.APIToken)

		budgets, err := runBudgetsFromConfig(setupCfg.Daemon.Budgets)
		if err != nil {
			return bootstrapConfig{}, fmt.Errorf("load setup config %s: %w", service.Path(), err)
		}
		cfg.Budgets = budgets
		cfg.CompactModel = setupCfg.Daemon.CompactModel
		cfg.WindowCap = setupCfg.Daemon.ContextWindowCap
		cfg.ModelConcurrency = setupCfg.Daemon.ModelConcurrency
		cfg.BackgroundAgents = setupCfg.Daemon.BackgroundAgents

		if err := setup.ImportDaemonEnvironmentFile(service.Path(), setupCfg); err != nil {
			return bootstrapConfig{}, fmt.Errorf("load setup daemon environment %s: %w", setup.DaemonEnvironmentFilePath(service.Path()), err)
		}

		if activeProvider, ok := setup.ActiveProviderConfig(setupCfg); ok {
			if _, ok := setup.ProviderConfigWithResolvedAPIKey(activeProvider); !ok {
				return bootstrapConfig{}, fmt.Errorf("load setup config %s: %s API key is required; set api_key or %s", service.Path(), firstNonEmpty(activeProvider.Name, activeProvider.ID, activeProvider.Type), firstNonEmpty(activeProvider.APIKeyEnv, "the provider API key environment variable"))
			}
		}

		cfg.SessionLLMs = sessionllm.New(setupCfg.ActiveProviderID, sessionProviderSpecsFromSetup(setupCfg))
		cfg.Assistant = core.AssistantProfile{
			Name:               setupCfg.Assistant.Name,
			SystemPrompt:       setup.InitializeAssistantSystemPromptForConfig(setupCfg.Assistant.SystemPrompt, setupCfg),
			CustomInstructions: setupCfg.Assistant.CustomInstructions,
		}
		clients, err := setup.ClientBootstrapsFromConfig(setupCfg)
		if err != nil {
			return bootstrapConfig{}, fmt.Errorf("load setup config %s: %w", service.Path(), err)
		}
		cfg.Clients = clients
		cfg.ExternalAgents = setupCfg.Modules
	}

	if addr := strings.TrimSpace(os.Getenv("MATRIXCLAW_HTTP_ADDR")); addr != "" {
		cfg.Addr = addr
	}
	if dbPath := strings.TrimSpace(os.Getenv("MATRIXCLAW_DB_PATH")); dbPath != "" {
		cfg.DBPath = dbPath
	}
	if timezone := strings.TrimSpace(os.Getenv("MATRIXCLAW_TIMEZONE")); timezone != "" {
		cfg.Timezone = timezone
	}
	if token := strings.TrimSpace(os.Getenv("MATRIXCLAW_API_TOKEN")); token != "" {
		cfg.APIToken = token
	}
	if strings.TrimSpace(cfg.Timezone) == "" {
		cfg.Timezone = "UTC"
	}
	if !allowRemoteHTTP() && !isLoopbackHTTPAddr(cfg.Addr) {
		return bootstrapConfig{}, fmt.Errorf("refusing to bind daemon API to non-loopback address %q without MATRIXCLAW_ALLOW_REMOTE_HTTP=1", cfg.Addr)
	}
	if !isLoopbackHTTPAddr(cfg.Addr) && strings.TrimSpace(cfg.APIToken) == "" {
		return bootstrapConfig{}, fmt.Errorf("refusing to bind daemon API to non-loopback address %q without MATRIXCLAW_API_TOKEN or setup api_token", cfg.Addr)
	}

	return cfg, nil
}
```

In `internal/daemoncmd/run.go`, replace `func Run` with:

```go
func Run(ctx context.Context) error {
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}

	sqliteStore, err := store.NewSQLite(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = sqliteStore.Close() }()
	automationStore, err := automation.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = automationStore.Close() }()
	workStore, err := work.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = workStore.Close() }()
	webResearchStore := webresearch.NewStore(workStore)

	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	mcpModule, err := mcpmodule.New(ctx, mcpConfigWithBrowser(bootstrap.ExternalAgents))
	if err != nil {
		log.Printf("matrixclawd mcp module disabled: %v", err)
		mcpModule, _ = mcpmodule.New(ctx, setup.MCPConfig{})
	}
	defer func() { _ = mcpModule.Close() }()
	skillsModule, err := skillsmodule.New(skillsConfigFromBootstrap(bootstrap))
	if err != nil {
		return err
	}
	defer func() { _ = skillsModule.Close() }()
	moduleRegistry := modules.NewRegistry(storageModule, mcpModule, skillsModule)

	app := core.New(sqliteStore).
		WithSessionLLMs(bootstrap.SessionLLMs).
		WithRunBudgets(bootstrap.Budgets).
		WithSessionFiles(sessionFilesRoot(bootstrap.DBPath)).
		WithCompactModel(bootstrap.CompactModel.Provider, bootstrap.CompactModel.Model).
		WithContextWindowCap(bootstrap.WindowCap).
		WithModelConcurrency(bootstrap.ModelConcurrency).
		WithBackgroundAgents(bootstrap.BackgroundAgents).
		WithWorkStore(workStore).
		WithAttachmentReader(storageAttachmentReader{store: storageModule.Store()}).
		WithSkillsContext(skillsModule).
		WithRuntimeStatusContext(&setupRuntimeStatusContext{setup: bootstrap.SetupService, runtime: localruntime.New("")})
	// The workflow worker (below) may start executing persisted runs before
	// supervisor.ApplyBootstrap runs, so the profile is set here too, via the
	// same helper, ensuring module context is never missing.
	applyAssistantProfile(app, bootstrap.Assistant, moduleRegistry.Context)
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.ExternalAgents)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	webSearchConfig := webSearchProviderConfig(bootstrap.SetupService)
	webResearchEngine := newWebResearchEngine(bootstrap.DBPath, bootstrap.ExternalAgents.MCP, mcpModule, webResearchStore, webSearchConfig)
	webTools := webtools.NewWebService(webSearchConfig, webResearchEngine)
	osmGeo := tools.NewOSMServiceFromEnv()
	extraTools := []tools.Executor{
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
		deliverymodule.NewSendFileTool(storageModule.Store(), app),
		telephonymodule.NewCallTool(bootstrap.SetupService),
		telephonymodule.NewEndCallTool(bootstrap.SetupService),
		voicemodule.NewTextToSpeechTool(bootstrap.SetupService),
		webtools.NewWebFetchExecutorWithService(webTools),
		webtools.NewWebSearchExecutorWithService(webTools),
	}
	extraTools = append(extraTools, webtools.NewWebResearchExecutorsWithService(webTools)...)
	toolRegistry := tools.NewCoreCodingRegistry(extraTools...)
	if err := toolRegistry.Register(tools.NewShellExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.TodoToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.AwaitToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.MemoryToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.AgentToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Err(); err != nil {
		return err
	}
	if err := toolRegistry.Register(tools.NewOSMGeoExecutors(osmGeo)...); err != nil {
		return err
	}
	if err := moduleRegistry.RegisterTools(toolRegistry); err != nil {
		return err
	}
	app.WithTools(newSetupAwareToolExecutor(toolRegistry, bootstrap.SetupService))
	// Shell tasks do not survive a restart; this runs before any run can start new ones.
	if err := app.RecoverTasks(ctx); err != nil {
		log.Printf("matrixclawd background task recovery failed: %v", err)
	}
	// The workflow worker executes persisted runs as soon as it starts, so it
	// starts only once the core is fully wired and before anything accepts runs.
	lifetime, stopLifetime := context.WithCancel(ctx)
	defer stopLifetime()
	app.WithLifetime(lifetime)
	runStarter, err := goworkflows.NewForStore(bootstrap.DBPath, app)
	if err != nil {
		return err
	}
	defer func() {
		// End the lifetime first so runs interrupted by closing the worker are not rescheduled.
		stopLifetime()
		_ = runStarter.Close()
	}()
	app.WithRunStarter(runStarter)
	safego.Go("core.runWakeups", func() { app.RunWakeups(lifetime) })
	server := api.New(app)
	server.SetAPIToken(bootstrap.APIToken)
	server.SetAutomationService(automationService)
	server.SetStorageStore(storageModule.Store())
	server.SetSkillsService(skillsModule.Service())
	server.SetSetupService(bootstrap.SetupService)
	server.SetRealtimeVoiceService(newRealtimeVoiceManager(bootstrap.SetupService, app))
	supervisor := newSupervisor(ctx, server, app, osmGeo)
	supervisor.SetModuleContext(moduleRegistry.Context)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes)
	defer supervisor.CloseExternalAgents()
	httpServer := &http.Server{
		Addr:              bootstrap.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 2)
	safego.Go("daemon.httpServer", func() {
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	})

	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		return err
	}
	startConfiguredVoiceRuntimes(ctx, bootstrap.SetupService)
	safego.Go("automation.Run", func() { automationService.Run(ctx) })
	safego.Go("webresearch.Run", func() { webResearchEngine.Start(ctx) })
	safego.Go("supervisor.deliverStartupNotifications", func() {
		supervisor.DeliverPendingStartupNotifications(bootstrap)
	})
	safego.Go("core.recoverState", func() {
		if err := app.RecoverActiveRuns(context.Background()); err != nil {
			log.Printf("matrixclawd active run recovery failed: %v", err)
		}
		if err := app.RecoverSessionInputs(context.Background()); err != nil {
			log.Printf("matrixclawd session input recovery failed: %v", err)
		}
		if err := app.RecoverSubagentTasks(context.Background()); err != nil {
			log.Printf("matrixclawd subagent recovery failed: %v", err)
		}
		if err := app.RecoverTaskEvents(context.Background()); err != nil {
			log.Printf("matrixclawd background task event recovery failed: %v", err)
		}
	})

	log.Printf("matrixclawd bootstrap: setup=%s", bootstrap.SetupPath)
	log.Printf("matrixclawd listening on %s using %s", bootstrap.Addr, bootstrap.DBPath)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

In `internal/setup/types.go`, replace `type DaemonConfig` with:

```go
type DaemonConfig struct {
	HTTPAddr        string             `json:"http_addr"`
	DBPath          string             `json:"db_path"`
	Timezone        string             `json:"timezone,omitempty"`
	APIToken        string             `json:"api_token,omitempty"`
	AutostartOnBoot bool               `json:"autostart_on_boot"`
	Budgets         RunBudgetsConfig   `json:"budgets,omitzero"`
	CompactModel    CompactModelConfig `json:"compact_model,omitzero"`
	// ContextWindowCap bounds every model's context window in tokens; 0 keeps
	// the default of 200000. Set it to the model's window to disable the cap.
	ContextWindowCap int `json:"context_window_cap,omitempty"`
	// ModelConcurrency bounds the model requests all runs make at once; 0
	// keeps the default of 4.
	ModelConcurrency int `json:"model_concurrency,omitempty"`
	// BackgroundAgents bounds the background subagents one session runs at
	// once; 0 keeps the default of 4.
	BackgroundAgents int `json:"background_agents,omitempty"`
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/core -run '^(TestReadonlyChildrenRunTogetherWithReadOnlyTools|TestReadonlyChildCannotUseAMutatingTool|TestBackgroundChildrenAreLimitedPerSession|TestOnlyChildrenWritingTheParentsDirectoryHoldItsKey|TestBlockingSubagentRunsUnderASingleModelSlot|TestBlockingSubagentReturnsChildSummaryToParent|TestSubagentSummaryJoinsAReplyCutByTheOutputLimit|TestSubagentStoppedAtItsBudgetReportsAPartialResult|TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate|TestInterruptedParentAndBlockingChildAreBothRescheduledAndComplete|TestCancelParentCancelsItsBlockingSubagent|TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp|TestDelegatedChildChangesTheParentsDirectoryWithoutDeadlock)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/agent_prompts.go \
  internal/core/agent_tool_test.go \
  internal/core/agent_tools.go \
  internal/core/concurrency_key_test.go \
  internal/core/core.go \
  internal/core/model_concurrency_test.go \
  internal/core/native_run_characterization_test.go \
  internal/core/run_budget_test.go \
  internal/core/run_crash_recovery_integration_test.go \
  internal/core/run_execute.go \
  internal/core/run_reschedule_test.go \
  internal/core/subagents.go \
  internal/core/subagents_approval_test.go \
  internal/core/subagents_cancel_test.go \
  internal/core/subagents_lifecycle.go \
  internal/core/subagents_presentation.go \
  internal/core/subagents_prompt.go \
  internal/core/subagents_runtime.go \
  internal/core/subagents_tools.go \
  internal/core/tool_batch_test.go \
  internal/core/tool_call_prepare.go \
  internal/daemoncmd/bootstrap.go \
  internal/daemoncmd/run.go \
  internal/providers/ai/gemini/schema_test.go \
  internal/setup/types.go
git rm internal/core/subagents_async.go
git commit -m "feat(core): one agent tool replaces delegate_task and spawn_subagent

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 4: A child's run end resumes its parent; the watcher goes

**Files:**
- Modify: `internal/core/subagents.go`
- Modify: `internal/core/tool_approvals.go`

The in-memory watcher goes; the bridged approval tests of stage 4a (`TestDeniedBridgedApprovalLetsTheChildGoOn`, `TestAlwaysAllowOnABridgedApprovalKeepsTheRuleForTheParent` with executing run starters) cover the path that replaces it, so this task changes no test.

- [ ] **Step 1: Implement**

In `internal/core/subagents.go`, the imports become:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)
```

In `internal/core/subagents.go`, replace `func (Core) resumeParentForSubagentStatus` with:

```go
// resumeParentForSubagentStatus starts the parent of a finished child and asks
// it for a child's pending approval; a child still running resumes its parent
// when its run ends (syncBlockingSubagentTaskAfterRun).
func (c *Core) resumeParentForSubagentStatus(ctx context.Context, task SubagentTask) (bool, error) {
	status, err := c.subagentTaskRunStatus(ctx, task)
	if err != nil {
		return false, err
	}
	if subagentRunStatusTerminal(status) {
		return true, c.startRun(ctx, task.ParentRunID)
	}
	if status == RunStatusWaitingApproval {
		return c.mirrorPendingSubagentApproval(ctx, task)
	}
	return false, nil
}
```

In `internal/core/subagents.go`, replace `const block starting with subagentApprovalBridgeSource` with:

```go
const subagentApprovalBridgeSource = "subagent_approval_bridge"
```

In `internal/core/subagents.go`, delete `func (Core) resumeParentAfterSubagentTerminal`.

In `internal/core/subagents.go`, delete `func (Core) waitForSubagentTerminalAndResumeParent`.

In `internal/core/tool_approvals.go`, replace `func (Core) passDecisionToSubagent` with:

```go
// passDecisionToSubagent hands the decision to the child's own approval: the
// child runs its call or reads the denial and goes on, while the parent keeps
// waiting for the child.
func (c *Core) passDecisionToSubagent(ctx context.Context, bridge subagentApprovalBridgeParams, decision ApprovalResolveRequest) error {
	task, err := c.store.GetSubagentTask(ctx, bridge.TaskID)
	if err != nil {
		task, err = c.store.GetSubagentTaskByChildRun(ctx, bridge.ChildRunID)
	}
	if err != nil {
		return err
	}
	if _, err := c.ResolveApproval(ctx, bridge.ChildApprovalID, decision); err != nil {
		terminal, terminalErr := c.subagentTaskTerminal(ctx, task)
		if terminalErr != nil || !terminal {
			return err
		}
	}
	if latest, err := c.store.GetSubagentTask(ctx, task.ID); err == nil {
		task = latest
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if !taskStatusTerminal(task.Status) {
		if task, err = c.markSubagentTaskRunning(ctx, task); err != nil {
			return err
		}
	}
	if task.Mode == SubagentTaskModeAsync {
		return nil
	}
	_, err = c.resumeParentForSubagentStatus(ctx, task)
	return err
}
```

- [ ] **Step 2: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/subagents.go \
  internal/core/tool_approvals.go
git commit -m "refactor(core): a child's run end resumes its parent; drop the resume watcher

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 5: Cancel cascades to the run's commands; a finished child's commands stop

**Files:**
- Modify: `internal/core/runs.go`
- Modify: `internal/core/sessions.go`
- Modify: `internal/core/shell_tasks.go`
- Modify: `internal/core/subagents_lifecycle.go`
- Test (modify): `internal/core/agent_tool_test.go`

Canceling a run stops the commands it started; a finished child's commands stop. `stopSessionTasks` gains a run filter and a reason.

- [ ] **Step 1: Write the failing tests**

In `internal/core/agent_tool_test.go`, add:

```go
func TestCancelingARunStopsTheCommandsItStarted(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	now := runRecoveryTestTime()
	for _, id := range []string{"run_one", "run_two"} {
		if err := db.CreateRun(context.Background(), core.Run{ID: id, SessionID: session.ID, UserMessageID: "msg_" + id, Status: core.RunStatusRunning, StartedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	start := func(runID string) string {
		started, err := app.RunCommand(context.Background(), tools.Call{SessionID: session.ID, RunID: runID}, tools.Command{Command: "sleep 30", Background: true})
		if err != nil {
			t.Fatal(err)
		}
		return started.TaskID
	}
	mine, other := start("run_one"), start("run_two")
	t.Cleanup(func() { _, _ = app.CancelTask(context.Background(), other) })

	if _, err := app.CancelRun(context.Background(), "run_one"); err != nil {
		t.Fatal(err)
	}

	stopped := waitTaskStatus(t, db, mine, core.TaskStatusCanceled)
	if stopped.Error != "canceled with its run" {
		t.Fatalf("stopped = %+v", stopped)
	}
	waitProcessGone(t, stopped.PID)
	if running, err := db.GetTask(context.Background(), other); err != nil || running.Status != core.TaskStatusRunning {
		t.Fatalf("another run's task = %+v, %v", running, err)
	}
}
```

In `internal/core/agent_tool_test.go`, add:

```go
func TestAFinishedChildsBackgroundCommandsStop(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionFiles(t.TempDir())
	registry := tools.NewRegistry(core.AgentToolExecutors(app)...)
	if err := registry.Register(tools.NewShellExecutors(app)...); err != nil {
		t.Fatal(err)
	}
	app.WithTools(registry)
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		answered := len(toolResults(request)) > 0
		switch {
		case childPrompt(request) != "" && !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "child-serve", Name: "bash", Arguments: json.RawMessage(`{"command":"sleep 30","run_in_background":true}`)}}}, nil
		case childPrompt(request) != "":
			return providers.Response{Text: "started the server"}, nil
		case !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child", Name: "agent", Arguments: json.RawMessage(`{"description":"Serve","prompt":"start the server"}`)}}}, nil
		default:
			return providers.Response{Text: "Done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "child_bash", core.RunStatusAccepted, false)
	sessionIn(t, db, session, t.TempDir(), core.PermissionModeFullAuto)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	child, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-child")
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := db.ListTasks(context.Background(), core.TaskFilter{SessionID: child.ChildSessionID, Kind: core.TaskKindShell})
	if err != nil || len(tasks) != 1 || tasks[0].Status != core.TaskStatusCanceled || tasks[0].Error != "its subagent finished" {
		t.Fatalf("child tasks = %+v, %v", tasks, err)
	}
	waitProcessGone(t, tasks[0].PID)
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/core -run '^(TestCancelingARunStopsTheCommandsItStarted|TestAFinishedChildsBackgroundCommandsStop)$'
```

Expected: the tests fail: the run's task and the child's task keep running.

- [ ] **Step 3: Implement**

In `internal/core/runs.go`, replace `func (Core) cancelRunRecords` with:

```go
// cancelRunRecords marks the run and its active subagent children canceled before
// any of them is stopped, so a stopped child is not kept for recovery, and stops
// the background commands the run started. It returns the ids of the runs to stop.
func (c *Core) cancelRunRecords(ctx context.Context, run *Run) ([]string, error) {
	children, err := c.cancelSubagentChildren(ctx, *run)
	if err != nil {
		return nil, err
	}
	if err := c.stopSessionTasks(ctx, run.SessionID, run.ID, "canceled with its run"); err != nil {
		return nil, err
	}
	if err := c.rejectRunApprovals(ctx, *run); err != nil {
		return nil, err
	}
	if err := c.setRunStatus(ctx, run, RunStatusCanceled, "canceled by user"); err != nil {
		return nil, err
	}
	return append([]string{run.ID}, children...), nil
}
```

In `internal/core/sessions.go`, replace `func (Core) DeleteSession` with:

```go
func (c *Core) DeleteSession(ctx context.Context, sessionID string) error {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if err := c.stopSessionTasks(ctx, sessionID, "", "its session was deleted"); err != nil {
		return err
	}
	if err := c.store.DeleteSession(ctx, sessionID); err != nil {
		return err
	}
	if err := c.removeSessionFiles(sessionID); err != nil {
		log.Printf("core: remove files of deleted session %q: %v", sessionID, err)
	}
	return nil
}
```

In `internal/core/shell_tasks.go`, replace `func (Core) stopSessionTasks` with:

```go
// stopSessionTasks kills the shell tasks a session runs, or those one run of it
// started when runID is set.
func (c *Core) stopSessionTasks(ctx context.Context, sessionID string, runID string, reason string) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if runID != "" && task.RunID != runID {
			continue
		}
		if _, err := c.cancelTask(ctx, task, reason); err != nil {
			return err
		}
	}
	return nil
}
```

In `internal/core/subagents_lifecycle.go`, replace `func (Core) afterRunExecution` with:

```go
func (c *Core) afterRunExecution(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	if runID == "" || c == nil || c.store == nil {
		return nil
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		if ignoreMissing(err) {
			return nil
		}
		return err
	}
	if task, err := c.store.GetSubagentTaskByChildRun(ctx, runID); err == nil {
		switch task.Mode {
		case SubagentTaskModeAsync:
			if syncErr := c.syncAsyncSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		case SubagentTaskModeBlocking:
			if syncErr := c.syncBlockingSubagentTaskAfterRun(ctx, task, run); syncErr != nil {
				return syncErr
			}
		}
	} else if !ignoreMissing(err) {
		return err
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		if ignoreMissing(err) {
			return nil
		}
		return err
	}
	if subagentRunStatusTerminal(run.Status) && isSubagentSession(session) {
		// Nobody reads a child's background commands once it has finished.
		if err := c.stopSessionTasks(ctx, session.ID, "", "its subagent finished"); err != nil {
			return err
		}
	}
	if subagentRunStatusTerminal(run.Status) {
		if err := c.queuePendingSteersForRun(ctx, session.ID, run.ID); err != nil {
			return err
		}
		startedInput, err := c.startNextPendingSessionInput(ctx, session.ID)
		if err != nil || startedInput {
			return err
		}
	}
	if subagentRunStatusTerminal(run.Status) {
		return c.wakeSession(ctx, session.ID, nil)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/core -run '^(TestCancelingARunStopsTheCommandsItStarted|TestAFinishedChildsBackgroundCommandsStop)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/agent_tool_test.go \
  internal/core/runs.go \
  internal/core/sessions.go \
  internal/core/shell_tasks.go \
  internal/core/subagents_lifecycle.go
git commit -m "feat(core): canceling a run stops its commands; a finished child's commands stop

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 6: Clients render agent calls

**Files:**
- Modify: `clients/telegram/run_tools.go`
- Modify: `clients/terminal/chat/runtime/app_layout_views.go`
- Modify: `clients/terminal/chat/runtime/conversation.go`
- Modify: `clients/terminal/ui/surface/chat/delegate.go`
- Modify: `clients/terminal/ui/surface/chat/tool_preview.go`
- Modify: `clients/terminal/ui/surface/chat/tools.go`
- Test (modify): `clients/telegram/run_render_test.go`
- Test (create): `clients/terminal/ui/surface/chat/delegate_test.go`

Clients render the new tool name.

- [ ] **Step 1: Write the failing tests**

In `clients/telegram/run_render_test.go`, add:

```go
func TestAgentStatusExplainsWhatTheSubagentDoes(t *testing.T) {
	action, detail := telegramToolAction(transcript.ToolCallPart{
		Name:  "agent",
		Input: `{"description":"Inspect the supervisor","prompt":"inspect the supervisor loop","runtime":"codex"}`,
	})
	if action != "Subagent is working" || detail != "Inspect the supervisor" {
		t.Fatalf("action = %q, detail = %q", action, detail)
	}
	started, _ := telegramToolAction(transcript.ToolCallPart{Name: "agent", Input: `{"description":"Scan","prompt":"scan","background":true}`})
	if started != "Starting subagent" {
		t.Fatalf("background action = %q", started)
	}
}
```

In `clients/telegram/run_render_test.go`, delete `func TestDelegateTaskStatusExplainsThatSubagentIsWorking`.

Create `clients/terminal/ui/surface/chat/delegate_test.go`:

```go
package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

func TestAgentCallRendersAsASubagentCard(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	call := surfacemessage.ToolCall{ID: "call_1", Name: "agent", Input: `{"description":"Review the parser","prompt":"review the parser for bugs","readonly":true}`, Finished: true}
	result := &surfacemessage.ToolResult{ToolCallID: "call_1", Name: "agent", Content: "No bugs found.", Metadata: `{"agent_name":"Neo","display_name":"Review the parser","status":"completed","summary":"No bugs found."}`}

	rendered := ansi.Strip(NewToolMessageItem(&sty, "msg_1", call, result, false).RawRender(120))

	for _, want := range []string{"Neo completed", "Review the parser", "No bugs found."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("card lacks %q:\n%s", want, rendered)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./clients/telegram -run '^(TestAgentStatusExplainsWhatTheSubagentDoes)$'
go test ./clients/terminal/ui/surface/chat -run '^(TestAgentCallRendersAsASubagentCard)$'
```

Expected: build fails (`NewAgentToolMessageItem` undefined) and the Telegram test gets no action for `agent`.

- [ ] **Step 3: Implement**

In `clients/telegram/run_tools.go`, replace `func telegramToolAction` with:

```go
func telegramToolAction(call transcript.ToolCallPart) (string, string) {
	params := decodeTelegramToolParams(call.Input)
	name := strings.ToLower(strings.TrimSpace(call.Name))
	switch name {
	case "agent":
		action := "Subagent is working"
		if telegramParam(params, "background") == "true" {
			action = "Starting subagent"
		}
		return action, firstNonEmpty(telegramParam(params, "description"), telegramParam(params, "prompt"))
	case "web_search":
		return "Searching web", telegramParam(params, "query")
	case "web_fetch":
		return "Fetching page", telegramParam(params, "url")
	case "web_research":
		return "Researching web", firstNonEmpty(telegramParam(params, "query"), telegramParam(params, "task"), telegramParam(params, "urls"))
	case "web_research_ask":
		return "Checking research", telegramParam(params, "question")
	case "web_research_status":
		return "Checking research", telegramParam(params, "research_id")
	case "reverse_geocode_osm":
		return "Checking address", telegramCoordinatesDetail(params)
	case "nearby_places_osm":
		return "Checking nearby places", firstNonEmpty(telegramCoordinatesDetail(params), telegramParam(params, "radius_m"))
	case "session_search":
		return "Searching sessions", telegramParam(params, "query")
	case "skill_search":
		return "Searching skills", telegramParam(params, "query")
	case "skill_view":
		return "Viewing skill", telegramParam(params, "id")
	case "skill_use":
		return "Loading skill", telegramParam(params, "id")
	case "memory":
		return "Using memory", firstNonEmpty(telegramParam(params, "action"), telegramParam(params, "query"), telegramParam(params, "content"))
	}
	if strings.HasPrefix(name, "mcp_browser_") {
		return telegramBrowserToolAction(name), firstNonEmpty(telegramParam(params, "url"), telegramParam(params, "text"), telegramParam(params, "selector"), telegramParam(params, "element"), telegramParam(params, "query"), telegramParam(params, "ref"))
	}
	return "Using " + telegramPrettyToolName(call.Name), firstNonEmpty(telegramParam(params, "query"), telegramParam(params, "url"), telegramParam(params, "path"), telegramParam(params, "file_path"), telegramParam(params, "action"), telegramParam(params, "name"), telegramParam(params, "id"), telegramParam(params, "text"))
}
```

In `clients/terminal/chat/runtime/app_layout_views.go`, replace `func isSubagentToolName` with:

```go
func isSubagentToolName(name string) bool {
	return strings.ToLower(strings.TrimSpace(name)) == "agent"
}
```

In `clients/terminal/chat/runtime/conversation.go`, replace `func mergeSubagentToolResults` with:

```go
func mergeSubagentToolResults(results map[string]surfacemessage.ToolResult, tasks []core.SubagentTask) {
	for _, task := range tasks {
		toolCallID := strings.TrimSpace(task.ParentToolCallID)
		if toolCallID == "" {
			continue
		}
		result := results[toolCallID]
		result.ToolCallID = toolCallID
		result.Name = "agent"
		if metadata, err := json.Marshal(task); err == nil {
			result.Metadata = string(metadata)
		}
		result.Status = subagentSurfaceResultStatus(task)
		result.IsError = task.Status == core.TaskStatusFailed || task.Status == core.TaskStatusCanceled || strings.TrimSpace(task.Error) != ""
		if strings.TrimSpace(result.Content) == "" || subagentTaskTerminal(task) {
			result.Content = subagentSurfaceResultContent(task)
		}
		results[toolCallID] = result
	}
}
```

In `clients/terminal/chat/runtime/conversation.go`, replace `func mergeSubagentToolUpdates` with:

```go
func mergeSubagentToolUpdates(updates map[string]core.ToolUpdate, tasks []core.SubagentTask) {
	for _, task := range tasks {
		toolCallID := strings.TrimSpace(task.ParentToolCallID)
		if toolCallID == "" {
			continue
		}
		update := updates[toolCallID]
		update.ToolCallID = toolCallID
		update.ToolName = "agent"
		update.State = subagentToolLifecycleState(task)
		update.ResultStatus = subagentSurfaceResultStatus(task)
		update.RunID = strings.TrimSpace(task.ParentRunID)
		update.SessionID = strings.TrimSpace(task.ParentSessionID)
		update.Error = strings.TrimSpace(task.Error)
		updates[toolCallID] = update
	}
}
```

In `clients/terminal/chat/runtime/conversation.go`, delete `func subagentToolName`.

In `clients/terminal/ui/surface/chat/delegate.go`, replace `func renderSubagentTool` with:

```go
func renderSubagentTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts, params agentRenderParams) string {
	metadata := parseSubagentTaskMetadata(opts.Result)
	agentName := strings.Join(strings.Fields(firstNonEmptyLocal(metadata.AgentName, metadata.DisplayName, params.Description, delegateRuntimeLabel(params.Runtime))), " ")
	taskLabel := strings.Join(strings.Fields(firstNonEmptyLocal(metadata.DisplayName, params.Description)), " ")
	goal := strings.Join(strings.Fields(firstNonEmptyLocal(metadata.Goal, params.Prompt)), " ")
	status := subagentRenderStatus(metadata.Status, opts)
	header := toolHeader(sty, opts.Status, subagentRenderLabel(agentName, status), width, opts.Compact, subagentTaskPreview(taskLabel, goal))
	if opts.Compact {
		return header
	}
	bodyText := subagentBodyText(opts, metadata, taskLabel, goal, status)
	if bodyText == "" {
		return header
	}
	bodyWidth := width - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, bodyText, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}
```

In `clients/terminal/ui/surface/chat/delegate.go`, replace `func isSubagentToolNameLocal` with:

```go
func isSubagentToolNameLocal(name string) bool {
	return strings.ToLower(strings.TrimSpace(name)) == "agent"
}
```

In `clients/terminal/ui/surface/chat/delegate.go`, add:

```go
type AgentToolMessageItem struct{ *baseToolMessageItem }
```

In `clients/terminal/ui/surface/chat/delegate.go`, add:

```go
func NewAgentToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &AgentToolRenderContext{}, canceled)
}
```

In `clients/terminal/ui/surface/chat/delegate.go`, add:

```go
type AgentToolRenderContext struct{}
```

In `clients/terminal/ui/surface/chat/delegate.go`, add:

```go
type agentRenderParams struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Runtime     string `json:"runtime"`
}
```

In `clients/terminal/ui/surface/chat/delegate.go`, add:

```go
func (d *AgentToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	params := parseAgentParams(opts.ToolCall.Input)
	return renderSubagentTool(sty, cappedWidth, opts, params)
}
```

In `clients/terminal/ui/surface/chat/delegate.go`, add:

```go
func parseAgentParams(input string) agentRenderParams {
	var params agentRenderParams
	_ = json.Unmarshal([]byte(input), &params)
	return params
}
```

In `clients/terminal/ui/surface/chat/delegate.go`, delete `func (DelegateTaskToolRenderContext) RenderTool`.

In `clients/terminal/ui/surface/chat/delegate.go`, delete `func NewDelegateTaskToolMessageItem`.

In `clients/terminal/ui/surface/chat/delegate.go`, delete `func parseDelegateTaskParams`.

In `clients/terminal/ui/surface/chat/delegate.go`, delete `type DelegateTaskToolMessageItem`.

In `clients/terminal/ui/surface/chat/delegate.go`, delete `type DelegateTaskToolRenderContext`.

In `clients/terminal/ui/surface/chat/delegate.go`, delete `type delegateTaskRenderParams`.

In `clients/terminal/ui/surface/chat/tool_preview.go`, replace `func (baseToolMessageItem) subagentPreviewData` with:

```go
func (t *baseToolMessageItem) subagentPreviewData() (surfacedialog.FilePreviewData, bool) {
	if !isSubagentToolNameLocal(t.toolCall.Name) {
		return surfacedialog.FilePreviewData{}, false
	}
	params := parseAgentParams(t.toolCall.Input)
	metadata := parseSubagentTaskMetadata(t.result)
	var out strings.Builder
	if name := firstNonEmptyLocal(metadata.AgentName, metadata.DisplayName, params.Description); name != "" {
		_, _ = fmt.Fprintf(&out, "Name: %s\n", name)
	}
	if task := firstNonEmptyLocal(metadata.DisplayName, params.Description); task != "" {
		_, _ = fmt.Fprintf(&out, "Task: %s\n", task)
	}
	if goal := firstNonEmptyLocal(metadata.Goal, params.Prompt); goal != "" {
		_, _ = fmt.Fprintf(&out, "Goal: %s\n", goal)
	}
	if runtime := firstNonEmptyLocal(metadata.Runtime, params.Runtime); runtime != "" {
		_, _ = fmt.Fprintf(&out, "Runtime: %s\n", runtime)
	}
	if status := firstNonEmptyLocal(metadata.Status, subagentPreviewResultStatus(t.result)); status != "" {
		_, _ = fmt.Fprintf(&out, "Status: %s\n", status)
	}
	if summary := strings.TrimSpace(metadata.Summary); summary != "" {
		out.WriteString("\nSummary:\n")
		out.WriteString(summary)
		out.WriteString("\n")
	}
	if errText := strings.TrimSpace(metadata.Error); errText != "" {
		out.WriteString("\nError:\n")
		out.WriteString(errText)
		out.WriteString("\n")
	}
	if t.result != nil {
		if content := strings.TrimSpace(t.result.Content); content != "" && !strings.Contains(out.String(), content) {
			out.WriteString("\nResult:\n")
			out.WriteString(content)
			out.WriteString("\n")
		}
	}
	content := strings.TrimSpace(out.String())
	if content == "" {
		content = "Subagent details are not available yet."
	}
	return surfacedialog.FilePreviewData{
		Title:   "Subagent Details",
		Content: content,
	}, true
}
```

In `clients/terminal/ui/surface/chat/tools.go`, replace `func NewToolMessageItem` with:

```go
func NewToolMessageItem(
	sty *surfacestyles.Styles,
	messageID string,
	toolCall surfacemessage.ToolCall,
	result *surfacemessage.ToolResult,
	canceled bool,
) ToolMessageItem {
	var item ToolMessageItem
	switch normalizedToolName(toolCall.Name) {
	case "bash":
		item = NewBashToolMessageItem(sty, toolCall, result, canceled)
	case "task_output":
		item = NewTaskOutputToolMessageItem(sty, toolCall, result, canceled)
	case "task_kill":
		item = NewTaskKillToolMessageItem(sty, toolCall, result, canceled)
	case "read":
		item = NewReadToolMessageItem(sty, toolCall, result, canceled)
	case "write":
		item = NewWriteToolMessageItem(sty, toolCall, result, canceled)
	case "edit":
		item = NewEditToolMessageItem(sty, toolCall, result, canceled)
	case "multiedit":
		item = NewMultiEditToolMessageItem(sty, toolCall, result, canceled)
	case "glob":
		item = NewGlobToolMessageItem(sty, toolCall, result, canceled)
	case "grep":
		item = NewGrepToolMessageItem(sty, toolCall, result, canceled)
	case "ls":
		item = NewLSToolMessageItem(sty, toolCall, result, canceled)
	case "agent":
		item = NewAgentToolMessageItem(sty, toolCall, result, canceled)
	default:
		item = NewGenericToolMessageItem(sty, toolCall, result, canceled)
	}
	item.SetMessageID(messageID)
	return item
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./clients/telegram -run '^(TestAgentStatusExplainsWhatTheSubagentDoes)$'
go test ./clients/terminal/ui/surface/chat -run '^(TestAgentCallRendersAsASubagentCard)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add clients/telegram/run_render_test.go \
  clients/telegram/run_tools.go \
  clients/terminal/chat/runtime/app_layout_views.go \
  clients/terminal/chat/runtime/conversation.go \
  clients/terminal/ui/surface/chat/delegate.go \
  clients/terminal/ui/surface/chat/delegate_test.go \
  clients/terminal/ui/surface/chat/tool_preview.go \
  clients/terminal/ui/surface/chat/tools.go
git commit -m "feat(clients): render agent calls as subagent cards

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 7: Docs, spec notes and the parent prompt test

**Files:**
- Modify: `README.md`
- Modify: `docs/EXTERNAL_AGENTS.md`
- Modify: `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`
- Test (modify): `internal/core/agent_tool_test.go`

README, external-agent docs, the spec notes, and a test that the parent's system prompt explains the tool.

- [ ] **Step 1: Implement**

In `README.md`:

```diff
diff --git a/README.md b/README.md
index 9206c8c..fd04d22 100644
--- a/README.md
+++ b/README.md
@@ -46,8 +46,9 @@ runtime through Terminal, Telegram, or MCP.
 - **Local-first state:** sessions, runs, approvals, files, todo lists, usage, and provider choices live in SQLite.
 - **Provider switching:** OpenAI-compatible APIs, OpenAI Codex subscription OAuth, Anthropic, Gemini, Chinese provider presets, and custom endpoints.
 - **External agents:** Codex app-server and Claude Code sessions attach to the same session model.
-- **Subagents:** MatrixClaw sessions can delegate bounded tasks to hidden child
-  runs through `delegate_task`, including MatrixClaw, Codex, or Claude Code runtimes.
+- **Subagents:** MatrixClaw sessions can hand bounded tasks to hidden child
+  runs through the `agent` tool, in the foreground or the background, including
+  MatrixClaw, Codex, or Claude Code runtimes.
 - **Tools with approvals:** file and shell tools pause before risky changes.
 - **Todo list:** the assistant tracks multi-step work with `todo_write`; a run that stops with open items is asked once to finish them or say why.
 - **Memory and search:** the assistant can save approved durable memories and search previous sessions with `memory` and `session_search`.
@@ -532,10 +533,10 @@ Enable/Disable picker.
 
 ## Subagents
 
-MatrixClaw assistant sessions receive a `delegate_task` tool for bounded child
-work. The parent model stays in charge of the user-facing session; the child
-session is hidden from the normal session list and the parent receives only the
-tool result summary.
+MatrixClaw assistant sessions receive an `agent` tool for bounded child work.
+The parent model stays in charge of the user-facing session; the child session
+is hidden from the normal session list and the parent receives only the child's
+result.
 
 Subagents can run as native MatrixClaw child sessions or through enabled
 external agents. Built-in external subagent runtimes are Codex (`codex`) and
@@ -546,21 +547,27 @@ runtime to use when multiple external subagents are enabled.
 
 The tool accepts:
 
-- `goal` required
-- `context` optional
+- `description` required: a short label
+- `prompt` required: everything the child needs
+- `background`: start the child as a background task; its result reaches the
+  parent as a message when it finishes (at most `daemon.background_agents`,
+  default 4, per session)
+- `isolation`: `shared` (the parent's directory, one writing child at a time)
+  or `worktree` (a git worktree of its own, so several run at once)
+- `readonly`: read-only tools only; read-only children run in parallel
 - `runtime`: `matrixclaw`, `codex`, `claude`, or `auto` (`matrixclaw` default)
 - `model` optional
-- `working_dir` optional
 
-Subagent runs start with an isolated prompt built from the delegated
-goal/context. They do not inherit the parent chat history, todo list, skills
-prompt, or memory prompt. Child MatrixClaw runs use a restricted tool view: no
-recursive `delegate_task`, no `memory`, no TTS, and no automation/storage/skills
-category tools; they keep their own todo list with `todo_write`.
+Subagent runs start with an isolated prompt built from the call's prompt. They
+do not inherit the parent chat history, todo list, skills prompt, or memory
+prompt. Child MatrixClaw runs use a restricted tool view: no `agent` or
+`await`, no `memory`, no TTS, and no other automation/storage/skills category
+tools; they keep their own todo list with `todo_write` and may run background
+commands, which stop when the child finishes. A blocking child's time does not
+count against the parent's budget.
 
-If a child run reaches a permission approval, MatrixClaw does not open a
-separate user approval flow in this version. The delegated task finishes with a
-controlled error summary so the parent can decide what to do next.
+If a child run asks for a permission, the parent session shows the request; the
+child goes on with the decision, and the parent resumes once the child ends.
 
 ## Local Voice
 
```

In `docs/EXTERNAL_AGENTS.md`:

````diff
diff --git a/docs/EXTERNAL_AGENTS.md b/docs/EXTERNAL_AGENTS.md
index 1042f51..82ebdc3 100644
--- a/docs/EXTERNAL_AGENTS.md
+++ b/docs/EXTERNAL_AGENTS.md
@@ -18,7 +18,7 @@ matrixclaw agents
 ```
 
 Enabled adapters appear in the new-session picker and can also be used as
-external subagent runtimes by `delegate_task`.
+external subagent runtimes by the `agent` tool.
 
 ## Session Model
 
@@ -122,10 +122,11 @@ default       -> approvalPolicy: on-request, sandbox: read-only
 
 ## Subagents
 
-MatrixClaw assistant sessions receive a `delegate_task` tool for bounded child
-work. Child sessions are hidden from the normal session list, receive an
-isolated prompt built from the delegated goal/context, and return a compact
-summary to the parent run.
+MatrixClaw assistant sessions receive an `agent` tool for bounded child work.
+Child sessions are hidden from the normal session list, receive an isolated
+prompt built from the call's prompt, and return a compact summary to the parent
+run. A `readonly` external child runs in the runtime's read-only sandbox
+(`default` mode) instead of full access.
 
 Allowed runtimes are:
 
@@ -137,8 +138,8 @@ auto
 ```
 
 `auto` defaults to the native MatrixClaw child runtime unless enabled external
-runtimes make another choice explicit. External-agent sessions cannot delegate
-again.
+runtimes make another choice explicit. External-agent sessions cannot start
+subagents.
 
 For implementation details and removal boundaries, see
 `internal/externalagents/docs/`.
````

In `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`:

```diff
diff --git a/docs/superpowers/specs/2026-09-23-long-running-agent-design.md b/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
index e833ebc..f769720 100644
--- a/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
+++ b/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
@@ -624,6 +624,41 @@ runtime, model}`; `runtime`/`model` keep delegation to Codex and Claude Code.
   tasks"); Telegram shows progress, no typing, and "Waiting for background
   tasks..."; the iOS package decodes it as `.unknown`.
 
+### Implementation notes (as built, stage 6c)
+
+- **agent** `{description, prompt, background, isolation, readonly, runtime,
+  model}` replaces `delegate_task`, `spawn_subagent`, `list_subagents` and
+  `read_subagent_result` (task_output reads a subagent task's result, the
+  context note lists running ones, `/tasks` shows them). `working_dir` and
+  `context` are gone: children work in the parent's directory or a worktree,
+  and the prompt carries the context. Task IDs are `task_…`.
+- **Keys and barriers**: a blocking child that may change the parent's
+  directory (`isolation shared`, not readonly) takes `subagents:<dir>` and is a
+  barrier; readonly, worktree and background children take no key and are no
+  barriers, so several run at once. Children started together get different
+  names (the parent's session gate covers naming).
+- **Readonly**: `tasks.readonly`; the child sees no mutating tool and core
+  refuses one (`… this subagent is read-only`); a readonly Codex or Claude Code
+  child runs in `default` mode, its read-only sandbox. A readonly child is
+  always `shared`.
+- **Background**: a `subagent` task (`background` 1) started at once; its
+  completion is a 6a/6b event (note, await, idle wake). The limit is
+  `daemon.background_agents` (default 4) per session; a repeated call returns
+  the task it started.
+- **Budget**: `Decision.Delegated` marks blocking agent calls; the time in
+  which only delegated calls of a batch run is left out of the parent's active
+  time. The child's time is its own run's.
+- **Resume**: the 24-hour watcher is gone. After a bridged decision the parent
+  is started if the child already ended; otherwise the child's run end
+  (`syncBlockingSubagentTaskAfterRun`) answers the parent's call and starts it,
+  and a new child approval is mirrored to the parent.
+- **Cancel** also stops the background commands the run started; a child's
+  background commands stop when the child's run ends.
+- **Prompt**: the parent's subagent guidance and the child's system prompt
+  (todo, background commands stop, read-only note) are rewritten for the tool.
+  Clients render `agent` calls as subagent cards; Telegram says "Starting
+  subagent" for background calls.
+
 ## 5. Providers
 
 - `providers.Request` gains `MaxOutputTokens` (priority: provider config →
```

- [ ] **Step 2: Update the tests**

In `internal/core/agent_tool_test.go`, add:

```go
func TestParentPromptExplainsTheAgentTool(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithBackgroundAgents(2)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	var system string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		system = request.SystemPrompt
		return providers.Response{Text: "Hi."}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "guidance", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"The agent tool runs a child agent", "readonly:true", "isolation worktree", "At most 2 background children", "Runtime IDs available for the agent tool: matrixclaw."} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt lacks %q:\n%s", want, system)
		}
	}
}
```

- [ ] **Step 3: Run the tests**

Run:

```bash
go test ./internal/core -run '^(TestParentPromptExplainsTheAgentTool)$'
```

Expected: PASS.

- [ ] **Step 4: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add README.md \
  docs/EXTERNAL_AGENTS.md \
  docs/superpowers/specs/2026-09-23-long-running-agent-design.md \
  internal/core/agent_tool_test.go
git commit -m "docs: stage 6c as built; the agent tool in the README

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 8: Verification

- [ ] **Step 1: Race and dead code**

```bash
go test -race ./internal/core ./internal/agent ./clients/...
deadcode -test ./...   # compare with the output before Task 1: nothing new
grep -rn 'delegate_task\|spawn_subagent\|list_subagents\|read_subagent_result' --include=*.go . # only transcripts in tests may remain; expect none
```

- [ ] **Step 2: Manual check on the test stand** (never the production daemon): ask for two read-only reviews at once — two subagent cards run together and the parent answers once both are done; ask for a background build child and `await` it — the note arrives and the run resumes; cancel a run that started `sleep 300` in the background — `/tasks` shows it stopped.
