# Stage 4c — Parallel Tool Scheduler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The tool calls of one model reply run concurrently (up to 8 at once) while calls that share a concurrency key — the working directory for mutating file and shell tools, the server for MCP tools — take turns across every run and session of the daemon; results reach the transcript, the loop guard and clients in the model's call order; the 4a approval barrier still holds later calls back; only the engine goroutine writes checkpoints (`tool_batch{call_ids, deferred_ids}`); a crash, an interruption or a cancel in the middle of a batch leaves every call completed, in flight or not started; and a daemon-wide semaphore bounds concurrent model requests of all runs and subagents.

**Architecture:** A new leaf package `internal/agent/toolsched` provides a `Semaphore` (model requests), keyed `Locks` (one per daemon, owned by `core`) and a generic `Batch` that runs calls on goroutines: calls sharing a key run one at a time in start order holding the key's lock, at most 8 at once. The engine's `runCalls` (new `internal/agent/batch.go`) admits calls in call order on the engine goroutine — `Tools.Authorize` (now also returning the call's `Key`), journaling, a `tool_batch` checkpoint — starts them on the batch, and journals their results in call order as they finish; tool goroutines only run `Tools.Execute`. Keys come from `tools.Spec.ConcurrencyKey(call)` with an executor override (`delegate_task`). Core passes one `Locks` and one model `Semaphore` (`daemon.model_concurrency`, default 4) to every engine, stops writing a native run's checkpoint from approval requests and delegations, and recovers every in-flight call of a batch.

**Tech Stack:** Go 1.26, SQLite (modernc), existing `internal/safego`; no new dependencies.

**Prerequisite:** stages 0–4b are committed (`main` at `ea59fc6` or later): the engine in `internal/agent` with the 4a barrier and `deferred` calls, permission checks in `core.checkPermission` (4b).

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session may commit in parallel: **locate code by function name, not line number**, run `git status --short` before each commit and stage only the paths listed in the task with explicit `git add <paths>` (never `-A`, `-u` or directories). `git rm` for deleted files.
- Run `gofmt -w` on every Go file you touch (code blocks below are not guaranteed to be column-aligned). Every commit must pass `go build ./... && go vet ./... && go test ./...` — run the full suite before committing.
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger. When a pre-existing test fails only because an expectation this plan changes on purpose, update that expectation and name it in your report.
- Concurrency tests here never sleep to synchronise: they rendezvous on channels, and a `time.After` appears only as a failure timeout. Run the packages you touch with `-race` before committing (`go test -race ./internal/agent/... ./internal/core/` takes about three minutes).
- Every code block below was built and run in a scratch copy on top of `ea59fc6`: `go build ./... && go vet ./... && go test ./...` passed after each task, and after Task 9 also `go test -race` for `internal/agent/...`, `internal/core`, `internal/store`, `internal/tools` and `internal/daemoncmd`.

## Decisions taken in this plan (owner should know)

1. **Keys.** `tools.Spec.ConcurrencyKey(call tools.Call) string` takes the call, not bare args, because the default key needs the working directory. Defaults: read-only tools `""` (free); every MCP tool — read-only ones too — `mcp.<server>` (its namespace), so one shared browser is never driven twice at once; mutating filesystem and shell tools `dir:<working dir>` (`tools.WorkingDirKey`); **other mutating tools `tool:<namespace>`** (storage, memory, automation, `spawn_subagent`: the spec is silent; these are short writes that should not race each other). An executor may name its own key through the optional `tools.ConcurrencyKeyProvider` (same pattern as 4b's `SubjectProvider`); `core.ToolExecutor` gains `ConcurrencyKey(toolID, call)`.
2. **`delegate_task` takes `subagents:<dir>`, not `dir:<dir>`.** Delegations into one directory still run one at a time (the spec's "mutating shared-isolation children run one at a time"), but the child's own edits and shell commands take `dir:<dir>`; if the parent's call held that key the child would wait for its own parent forever (Task 5 has a test that deadlocks with the shared key). Today every delegation is shared-isolation; 6c's `readonly`/`worktree` children will return `""` / their worktree from the same method.
3. **Barrier (4a) refined.** A call is a barrier when it mutates **and a rule or the mode does not allow it** — an allowed call runs approved and cannot ask — except `delegate_task`, which always is (its child may ask). The calls after an admitted barrier are journaled `deferred` at once and start only when it returns without asking; if it asks they stay deferred exactly as in 4a. So in `default` mode `bash` still holds the rest of the reply back, while in `accept_edits`/`full_auto` allowed edits and commands run in parallel by key. Clients briefly show those calls as deferred.
4. **Calls of one reply are independent.** Only calls sharing a key are ordered; a `read` issued next to an `edit` of the same file may see the file before or after the edit. The new prompt line (Task 8) tells the model so. Serialising reads behind writes would also block every read of a directory while a 10-minute build runs there.
5. **Order and events (definition).** Calls are authorized and journaled (call messages, `message.created`) in call order before any of them runs. `tool.requested` is emitted in call order when a call starts (a call behind a barrier starts later). Results are journaled — result message, loop-guard observation, `Tools.Finish`, `tool.finished` — strictly in call order, so a finished call waits for the calls before it. The `requested` and `finished` streams interleave (call 3 may be requested after call 1 finished); per call `requested` precedes `finished`. Approval requests are made as soon as a call asks (completion order). The loop guard therefore counts deterministically.
6. **Checkpoints are engine-only.** Phases: `model` before each generation (unchanged), `tool_batch{call_ids, deferred_ids}` each time calls of a batch start (after they are journaled, before they run), and `model` when the run parks for approval, so a parked run keeps its counters. `agent.PhaseTool`, `agent.State.ToolCallID/ToolName` and every per-call engine checkpoint are deleted; `core.RunCheckpoint` gains `Batch *agent.ToolBatch` stored in the new column `run_checkpoints.tool_batch`. Approval requests of engine runs and of mirrored subagent bridges use the new `core.requestApproval`, which does not touch the checkpoint; `DelegateTask` no longer writes the parent's `waiting_subagent` checkpoint from the tool goroutine (nothing read it). Run-less `ExecuteTool` and crash recovery keep their checkpoint writes (`createPendingApproval`, `prepareToolCall`, `finishToolCall`).
7. **Crash recovery** stays transcript-driven: a call with a result is completed; a journaled call without one is in flight (read-only calls replay, mutating ones get a "retry after restart" approval — now **every** in-flight call of the batch, not only the first); a `deferred` call never started and runs when the run resumes. A call that was waiting for its key or a slot counts as in flight. When one in-flight call waits for a still-running blocking subagent, the run waits for the subagent first (its approvals stay pending and are found by the next recovery pass).
8. **Stop.** When the run's context stops mid-batch the engine waits for its goroutines, then journals — with a detached context, at most 5 s — the results of calls that finished before the stop; what a call returns after the stop is dropped. A **canceled** run (user cancel) answers every other call of the batch with `Canceled by user.` (with `tool.finished` only for calls that had started); an **interrupted** run (shutdown, heartbeat loss) leaves them to recovery, as today.
9. **A panicking tool** becomes the error result "The tool failed unexpectedly; the daemon log has the details." (`safego` logs the stack) instead of crashing the daemon; a tool returning an error to `Execute` still fails the run as before.
10. **Model slots.** `daemon.model_concurrency` in the setup config (default 4) bounds concurrent `Generate` calls of all native runs — parents, subagents and summary requests. A slot is held only inside `Generate` (released between retries), so a parent blocked in `delegate_task` holds none; waiting for a slot counts as active wall-clock but not as step latency.
11. **Limits are constants**: 8 calls per batch (`maxParallelCalls`); keyed locks are in memory, not fair beyond Go's channel queueing, and not persisted.
12. **`resumeDecided`** now answers denials first and then runs the granted calls as one parallel batch (4a interleaved them in decision order); results are unchanged.
13. **Clients need no change**: the TUI (`clientruntime.State.toolUpdates`) and Telegram (`runDeliveryState.toolCalls`) key tool state by call ID, so several running calls render already.

Out of scope: 6c's unified `agent` tool (readonly and worktree children, parent resume on the whole batch), a configurable per-batch limit, keying `web_research`'s internal browser fallback (it stays read-only and keyless; the MCP executor keeps its own per-tool mutex).

## File structure

Created:
- `internal/agent/toolsched/{semaphore,locks,batch}.go` and `{semaphore,locks,batch}_test.go`
- `internal/agent/batch.go`, `internal/agent/model_slots_test.go`, `internal/agent/parallel_test.go`
- `internal/tools/concurrency_key.go`, `internal/tools/concurrency_key_test.go`
- `internal/core/model_concurrency_test.go`, `internal/core/concurrency_key_test.go`, `internal/core/tool_batch_test.go`

Modified:
- `internal/agent/{engine,generate,ports,tools}.go`, `internal/agent/agenttest/agenttest.go`, `internal/agent/{engine,budget}_test.go`, `internal/agent/prompt/{guidance,guidance_test}.go`
- `internal/core/{core,run_execute,run_checkpoint,tool_call_approval,agent_inbox,agent_tools,subagents,subagents_tools,ports,run_recovery}.go`, `internal/core/{native_run_characterization,run_crash_recovery_integration}_test.go`
- `internal/store/{schema,sqlite_run_checkpoints}.go`, `internal/store/sqlite_runs_test.go`
- `internal/setup/types.go`, `internal/daemoncmd/{bootstrap,run,tool_visibility}.go`
- `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`

---
### Task 1: Daemon-wide model slots

A `toolsched.Semaphore` shared by every engine bounds concurrent model requests; `core` owns one (default 4, `daemon.model_concurrency`).

**Files:**
- Create: `internal/agent/toolsched/semaphore.go`, `internal/agent/toolsched/semaphore_test.go`
- Create: `internal/agent/model_slots_test.go`, `internal/core/model_concurrency_test.go`
- Modify: `internal/agent/engine.go` (`Config`), `internal/agent/generate.go` (`summaryModel.Generate`, `run.generate`), `internal/agent/agenttest/agenttest.go` (`Fixture`, `Fixture.Engine`)
- Modify: `internal/core/core.go` (`Core`, `New`), `internal/core/run_execute.go` (new constant and `WithModelConcurrency`, `nativeEngine`)
- Modify: `internal/setup/types.go` (`DaemonConfig`), `internal/daemoncmd/bootstrap.go` (`bootstrapConfig`, `loadBootstrap`), `internal/daemoncmd/run.go`

- [ ] **Step 1: Write the semaphore tests**

`internal/agent/toolsched/semaphore_test.go`:

```go
package toolsched

import (
	"context"
	"errors"
	"testing"
)

func TestSemaphoreLetsInAtMostNHolders(t *testing.T) {
	s := NewSemaphore(2)
	first, err := s.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	full, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Acquire(full); !errors.Is(err, context.Canceled) {
		t.Fatalf("third holder err = %v, want it kept out", err)
	}
	first()
	if _, err := s.Acquire(context.Background()); err != nil {
		t.Fatalf("a released slot was not reusable: %v", err)
	}
}

func TestSemaphoreWaiterLeavesWhenItsContextStops(t *testing.T) {
	s := NewSemaphore(1)
	if _, err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := s.Acquire(ctx)
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter err = %v", err)
	}
}

func TestNilSemaphoreNeverBlocks(t *testing.T) {
	var s *Semaphore
	for range 3 {
		release, err := s.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if NewSemaphore(0) != nil {
		t.Fatal("NewSemaphore(0) is not the unbounded nil semaphore")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/agent/toolsched/`
Expected: FAIL — `undefined: NewSemaphore` (the package has no source file yet).

- [ ] **Step 3: Write the semaphore**

`internal/agent/toolsched/semaphore.go`:

```go
// Package toolsched holds the daemon-wide scheduling primitives of native runs:
// a semaphore for model requests and keyed locks and batches for tool calls.
package toolsched

import "context"

// Semaphore lets at most n holders in at once; a nil Semaphore never blocks.
type Semaphore struct {
	slots chan struct{}
}

// NewSemaphore returns a semaphore with n slots, or nil when n <= 0.
func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		return nil
	}
	return &Semaphore{slots: make(chan struct{}, n)}
}

// Acquire waits for a free slot or until ctx stops; release gives the slot back.
func (s *Semaphore) Acquire(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return func() {}, nil
	}
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

Run: `go test -race ./internal/agent/toolsched/`
Expected: PASS.

- [ ] **Step 4: Write the engine test**

`internal/agent/model_slots_test.go`:

```go
package agent_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestModelSlotsBoundGenerationsAcrossRuns(t *testing.T) {
	slots := toolsched.NewSemaphore(1)
	var inFlight, most atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	model := agenttest.ModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		n := inFlight.Add(1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		entered <- struct{}{}
		<-release
		inFlight.Add(-1)
		return providers.Response{Text: "Done."}, nil
	})
	outcomes := make(chan agent.Outcome, 2)
	for range 2 {
		f := agenttest.NewFixture()
		f.ModelSlots = slots
		engine, task := f.Engine(), f.Task(model)
		go func() {
			outcome, _ := engine.Run(context.Background(), task)
			outcomes <- outcome
		}()
	}

	for range 2 {
		<-entered
		release <- struct{}{}
	}

	for range 2 {
		if outcome := <-outcomes; outcome.Status != agent.StatusCompleted {
			t.Fatalf("outcome = %+v", outcome)
		}
	}
	if most.Load() != 1 {
		t.Fatalf("%d generations ran at once, want 1", most.Load())
	}
}
```

Run: `go test ./internal/agent/ -run TestModelSlotsBoundGenerationsAcrossRuns`
Expected: FAIL to compile — `f.ModelSlots undefined`.

- [ ] **Step 5: Give the engine its model slots**

In `internal/agent/engine.go` import `"github.com/Suren878/matrixclaw/internal/agent/toolsched"` and add the last field of `Config`:

```go
// Config wires the ports of one run.
type Config struct {
	Journal     Journal
	Tools       Tools
	Approvals   Approvals
	Inbox       Inbox
	Sink        Sink
	Prompts     Prompts
	Attachments agentcontext.AttachmentReader
	Now         func() time.Time
	NewID       func(prefix string) string
	// Sleep waits d or until ctx stops; nil uses a real timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// ModelSlots bounds the model requests of every run sharing it; nil is unbounded.
	ModelSlots *toolsched.Semaphore
}
```

In `internal/agent/generate.go` replace `summaryModel.Generate` and `run.generate`:

```go
func (m summaryModel) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	for attempt := 0; ; attempt++ {
		release, err := m.r.ModelSlots.Acquire(ctx)
		if err != nil {
			return providers.Response{}, err
		}
		started := time.Now()
		response, err := m.model.Generate(ctx, request)
		release()
		if err == nil {
			return response, m.r.recordStep(ctx, response, compactStopReason, time.Since(started))
		}
		if ctx.Err() != nil || attempt >= len(retryBackoffs) || !providers.IsRetryableGenerationError(err) {
			return response, err
		}
		if err := m.r.Sleep(ctx, retryBackoffs[attempt]); err != nil {
			return response, err
		}
	}
}
```

```go
// generate streams one assistant turn, writing progress at most every flush interval.
func (r *run) generate(ctx context.Context, request providers.Request) (generation, error) {
	gen := generation{assistant: transcript.Message{
		ID:        r.NewID("msg"),
		SessionID: r.task.SessionID,
		RunID:     r.task.RunID,
		Role:      transcript.MessageRoleAssistant,
	}}
	dirty := false
	var lastFlush, lastCancelCheck time.Time
	flush := func(force bool) error {
		if !dirty {
			return nil
		}
		now := r.Now()
		if !force && gen.saved && !lastFlush.IsZero() && now.Sub(lastFlush) < progressFlushInterval {
			return nil
		}
		gen.assistant.Parts = transcript.NormalizeMessageParts(gen.assistant.Content, nil)
		gen.assistant.UpdatedAt = now
		if gen.saved {
			if err := r.history.stream(ctx, gen.assistant); err != nil {
				return err
			}
		} else {
			gen.assistant.CreatedAt = now
			if err := r.history.beginStreaming(ctx, gen.assistant); err != nil {
				return err
			}
			gen.saved = true
		}
		dirty = false
		lastFlush = now
		return nil
	}
	sanitizer := newAssistantStreamSanitizer()
	streamCtx := providers.WithTextStream(ctx, func(delta string) error {
		select {
		case <-ctx.Done():
			return errRunCanceled
		default:
		}
		now := r.Now()
		if lastCancelCheck.IsZero() || now.Sub(lastCancelCheck) >= progressFlushInterval {
			lastCancelCheck = now
			if r.canceled(ctx) {
				return errRunCanceled
			}
		}
		if !gen.saved && gen.assistant.Content == "" {
			delta = strings.TrimPrefix(delta, "\n")
		}
		if delta = sanitizer.Push(delta); delta == "" {
			return nil
		}
		gen.assistant.Content += delta
		dirty = true
		return flush(false)
	})
	release, err := r.ModelSlots.Acquire(ctx)
	if err != nil {
		return gen, err
	}
	started := time.Now()
	response, err := r.task.Model.Generate(streamCtx, request)
	release()
	if err == nil {
		err = r.recordStep(ctx, response, stepStopReason(response), time.Since(started))
	}
	if flushErr := flush(true); flushErr != nil {
		err = errors.Join(err, flushErr)
	}
	gen.response = response
	return gen, err
}
```

In `internal/agent/agenttest/agenttest.go` import `toolsched`, add the field before `ids` in `Fixture` and pass it in `Fixture.Engine`:

```go
	// ModelSlots is shared by the engines of fixtures that should compete for model requests.
	ModelSlots *toolsched.Semaphore
	ids        int
}
```

```go
// Engine returns an engine over the fixture's fakes with a fixed clock and sequential IDs.
func (f *Fixture) Engine() *agent.Engine {
	sleep := func(ctx context.Context, d time.Duration) error {
		f.Slept = append(f.Slept, d)
		return ctx.Err()
	}
	if f.RealSleep {
		sleep = nil
	}
	return agent.New(agent.Config{
		Journal:    f.Journal,
		Tools:      f.Tools,
		Approvals:  f.Approvals,
		Inbox:      f.Inbox,
		Sink:       f.Sink,
		Prompts:    f.Prompts,
		Now:        func() time.Time { return f.Clock },
		Sleep:      sleep,
		ModelSlots: f.ModelSlots,
		NewID: func(prefix string) string {
			f.ids++
			return fmt.Sprintf("%s_%d", prefix, f.ids)
		},
	})
}
```

Run: `go test -race ./internal/agent/...`
Expected: PASS.

- [ ] **Step 6: Write the core test**

`internal/core/model_concurrency_test.go` (a blocking subagent must finish under a single slot: the parent holds no slot while it waits in `delegate_task`):

```go
package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestBlockingSubagentRunsUnderASingleModelSlot(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithModelConcurrency(1)
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child done"}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: []byte(`{"goal":"count files","runtime":"matrixclaw"}`)}}}, nil
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

Run: `go test ./internal/core/ -run TestBlockingSubagentRunsUnderASingleModelSlot`
Expected: FAIL to compile — `app.WithModelConcurrency undefined`.

- [ ] **Step 7: Own the slots in core and wire the setting**

`internal/core/core.go`: import `"github.com/Suren878/matrixclaw/internal/agent/toolsched"`; in `Core`, after the `windowCap int` field, add

```go
	// modelSlots bounds the model requests of all native runs at once.
	modelSlots *toolsched.Semaphore
```

and replace `New`:

```go
func New(store Store) *Core {
	return &Core{
		store:         store,
		activeRuns:    map[string]*activeRun{},
		scheduledRuns: map[string]time.Time{},
		sessionGates:  map[string]*sync.Mutex{},
		events:        newEventBus(),
		now:           time.Now,
		newID:         defaultID,
		historyLimit:  50,
		lifetime:      context.Background(),
		budgets:       DefaultRunBudgets(),
		modelSlots:    toolsched.NewSemaphore(DefaultModelConcurrency),
	}
}
```

`internal/core/run_execute.go`: import `"github.com/Suren878/matrixclaw/internal/agent/toolsched"` and add below the imports:

```go
// DefaultModelConcurrency is how many model requests native runs make at once
// unless the daemon configures another limit.
const DefaultModelConcurrency = 4

// WithModelConcurrency bounds the model requests all native runs, subagents
// included, make at once; 0 or less keeps the default.
func (c *Core) WithModelConcurrency(n int) *Core {
	if n <= 0 {
		n = DefaultModelConcurrency
	}
	c.modelSlots = toolsched.NewSemaphore(n)
	return c
}
```

In `nativeEngine` the engine is now built as:

```go
	engine := agent.New(agent.Config{
		Journal:     coreJournal{c: c},
		Tools:       coreTools{c: c, turn: turn},
		Approvals:   coreApprovals{c: c, sessionID: session.ID},
		Inbox:       coreInbox{c: c, session: session},
		Sink:        coreSink{c: c},
		Prompts:     &corePrompts{c: c, turn: turn},
		Attachments: c.attachments,
		Now:         func() time.Time { return c.now().UTC() },
		NewID:       c.newID,
		ModelSlots:  c.modelSlots,
	})
```

`internal/setup/types.go`, at the end of `DaemonConfig`:

```go
	// ContextWindowCap bounds every model's context window in tokens; 0 keeps
	// the default of 200000. Set it to the model's window to disable the cap.
	ContextWindowCap int `json:"context_window_cap,omitempty"`
	// ModelConcurrency bounds the model requests all runs make at once; 0
	// keeps the default of 4.
	ModelConcurrency int `json:"model_concurrency,omitempty"`
}
```

`internal/daemoncmd/bootstrap.go`: add the last field of `bootstrapConfig`

```go
	WindowCap      int
	// ModelConcurrency bounds concurrent model requests; 0 keeps core's default.
	ModelConcurrency int
}
```

and in `loadBootstrap`, after `cfg.WindowCap = setupCfg.Daemon.ContextWindowCap`:

```go
		cfg.ModelConcurrency = setupCfg.Daemon.ModelConcurrency
```

`internal/daemoncmd/run.go`, in the `core.New(sqliteStore).…` builder chain after `WithContextWindowCap(bootstrap.WindowCap).`:

```go
		WithModelConcurrency(bootstrap.ModelConcurrency).
```

- [ ] **Step 8: Run the suite**

Run: `gofmt -w internal/agent internal/core internal/setup internal/daemoncmd && go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/agent/toolsched/semaphore.go internal/agent/toolsched/semaphore_test.go internal/agent/model_slots_test.go internal/agent/engine.go internal/agent/generate.go internal/agent/agenttest/agenttest.go internal/core/core.go internal/core/run_execute.go internal/core/model_concurrency_test.go internal/setup/types.go internal/daemoncmd/bootstrap.go internal/daemoncmd/run.go
git commit -m "feat(agent): bound concurrent model requests across runs

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---
### Task 2: Engine-only checkpoints with the `tool_batch` phase

The engine stops writing a checkpoint per call: it writes `tool_batch{call_ids, deferred_ids}` when calls of a batch start and `model` when the run parks. Core stops writing a native run's checkpoint from approval requests and from `DelegateTask`. Calls still run one after another in this task; the batch shape is what Task 5 parallelises.

**Files:**
- Modify: `internal/agent/ports.go` (`Phase`, `State`, new `ToolBatch`), `internal/agent/engine.go` (`step`, `settle`, `checkpoint`), `internal/agent/tools.go`
- Modify: `internal/agent/engine_test.go`, `internal/agent/budget_test.go`
- Modify: `internal/core/run_checkpoint.go`, `internal/core/tool_call_approval.go`, `internal/core/agent_inbox.go` (`coreApprovals.Request`), `internal/core/subagents.go` (`DelegateTask`, `mirrorPendingSubagentApproval`)
- Modify: `internal/store/schema.go`, `internal/store/sqlite_run_checkpoints.go`, `internal/store/sqlite_runs_test.go`
- Modify: `internal/core/native_run_characterization_test.go`

- [ ] **Step 1: Write the failing engine tests**

In `internal/agent/engine_test.go` replace `phases`:

```go
// phases renders checkpoints as phase[:call+call[/deferred+deferred]].
func phases(states []agent.State) string {
	out := make([]string, 0, len(states))
	for _, state := range states {
		phase := string(state.Phase)
		if state.Batch != nil {
			phase += ":" + strings.Join(state.Batch.CallIDs, "+")
			if len(state.Batch.DeferredIDs) > 0 {
				phase += "/" + strings.Join(state.Batch.DeferredIDs, "+")
			}
		}
		out = append(out, phase)
	}
	return strings.Join(out, ",")
}
```

In `TestToolRoundTripIsJournaledInOrder` the expected checkpoints become `"model,tool_batch:c1,model"`:

```go
	if got := phases(f.Journal.States); got != "model,tool_batch:c1,model" {
		t.Fatalf("checkpoints = %s", got)
	}
```

Add before `TestDeniedBarrierAnswersTheCallsItHeldBack`:

```go
func TestBatchCheckpointsNameTheCallsAndTheDeferredOnes(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Tools.Funcs["read"] = readTool
	f.Tools.Mutating = map[string]bool{"write": true}
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("w1", "write"), call("r2", "read")), text("Done."))

	run(t, f, model)

	if got := phases(f.Journal.States); got != "model,tool_batch:r1+w1+r2,tool_batch:r1+w1+r2/r2,model" {
		t.Fatalf("checkpoints = %s", got)
	}

	f.Approvals.Open = false
	f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)}}
	run(t, f, model)

	if got := phases(f.Journal.States[4:]); got != "tool_batch:w1,tool_batch:r2,model" {
		t.Fatalf("checkpoints after the grant = %s", got)
	}
}
```

In `internal/agent/budget_test.go`, `TestCheckpointsCarryTheRunCounters` expects the same shape:

```go
	if got := phases(f.Journal.States); got != "model,tool_batch:c1,model" {
		t.Fatalf("checkpoints = %s", got)
	}
```

Run: `go test ./internal/agent/`
Expected: FAIL to compile — `state.Batch undefined (type agent.State has no field or method Batch)`.

- [ ] **Step 2: Give `State` the batch**

In `internal/agent/ports.go` replace `Phase`, its constants and `State` with:

```go
// Phase is the durable execution boundary a run last reached.
type Phase string

const (
	PhaseModel     Phase = "model"
	PhaseToolBatch Phase = "tool_batch"
)

// State is the checkpoint the engine writes before each model call, when it
// starts calls of a tool batch and when the run parks; its Counters let a parked
// or restarted run keep its budget.
type State struct {
	RunID    string
	Phase    Phase
	Batch    *ToolBatch
	Counters Counters
}

// ToolBatch names the calls of the batch being run, in call order, and those
// of them held back (deferred) when the checkpoint was written.
type ToolBatch struct {
	CallIDs     []string `json:"call_ids"`
	DeferredIDs []string `json:"deferred_ids,omitempty"`
}
```

In `internal/agent/engine.go` replace `checkpoint`:

```go
// checkpoint records the durable phase together with the run's counters; only
// the engine goroutine writes it.
func (r *run) checkpoint(ctx context.Context, phase Phase, batch *ToolBatch) error {
	r.counters.Active = r.active()
	return r.Journal.Checkpoint(ctx, State{RunID: r.task.RunID, Phase: phase, Batch: batch, Counters: r.counters})
}
```

In `step` the checkpoint before the model call becomes:

```go
	if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
		return failedStep(err)
	}
```

and `settle` writes one when the run parks:

```go
func (r *run) settle(ctx context.Context, result stepResult) (Outcome, bool, error) {
	if result.assistant != nil && r.canceled(ctx) {
		return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, true, nil
	}
	if result.err != nil {
		outcome := Outcome{Status: StatusFailed, Err: result.err, StopReason: result.stop}
		if result.markErrored && result.assistant != nil {
			outcome.Assistant, outcome.AssistantSaved, outcome.MarkErrored = result.assistant, result.saved, true
		}
		return outcome, true, nil
	}
	switch result.kind {
	case stepWaitingApproval:
		pending, err := r.Approvals.Pending(ctx, r.task.RunID)
		if err != nil {
			return Outcome{}, true, err
		}
		if !pending {
			return Outcome{}, false, nil
		}
		if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Status: StatusWaitingApproval}, true, nil
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		return Outcome{Status: StatusCompleted, StopReason: result.stopReason(), Assistant: &reply, AssistantSaved: result.saved}, true, nil
	default:
		return Outcome{}, false, nil
	}
}
```

- [ ] **Step 3: Checkpoint batches instead of calls**

In `internal/agent/tools.go` replace `executeBatch`, `resumeDecided` and `runDeferred` (deleted) with:

```go
// executeBatch runs the response's tool calls.
func (r *run) executeBatch(ctx context.Context, response providers.Response) (bool, error) {
	requests, err := r.batchRequests(response)
	if err != nil {
		return false, err
	}
	return r.runCalls(ctx, requests)
}

// batchRequests turns the response's tool calls into requests, skipping repeats
// of a call already answered; a call ID reused for another call fails the run.
func (r *run) batchRequests(response providers.Response) ([]callRequest, error) {
	seen := make(map[string]providers.ToolCall)
	var requests []callRequest
	for _, toolCall := range response.ToolCalls {
		id := strings.TrimSpace(toolCall.ID)
		if id != "" {
			if prior, ok := seen[id]; ok {
				if !sameRequestedTool(prior.Name, prior.Arguments, toolCall) {
					return nil, fmt.Errorf("tool call ID %q reused with different arguments", id)
				}
				continue
			}
			if prior, ok := r.history.callMessage(id); ok {
				if prior.RunID != r.task.RunID {
					return nil, fmt.Errorf("tool call ID %q belongs to another run", id)
				}
				for _, part := range prior.Parts {
					if part.ToolCall != nil && part.ToolCall.ID == id && !sameRequestedTool(part.ToolCall.Name, []byte(part.ToolCall.Input), toolCall) {
						return nil, fmt.Errorf("tool call ID %q reused with different arguments", id)
					}
				}
				if r.history.hasResult(id) {
					continue
				}
			}
			seen[id] = toolCall
		}
		name := strings.TrimSpace(toolCall.Name)
		if name == "" {
			return nil, errors.New("provider returned tool call without a name")
		}
		if id == "" {
			id = r.NewID("tool")
		}
		requests = append(requests, callRequest{id: id, name: name, args: toolCall.Arguments, workingDir: r.task.WorkingDir})
	}
	return requests, nil
}

// runCalls runs calls of one batch in call order. A call waiting for approval
// parks while the rest runs, unless it is a barrier: every later call is then
// journaled deferred and runs once the run's approvals are decided.
func (r *run) runCalls(ctx context.Context, requests []callRequest) (bool, error) {
	if len(requests) == 0 {
		return false, nil
	}
	if err := r.checkpoint(ctx, PhaseToolBatch, batchOf(requests, nil)); err != nil {
		return false, err
	}
	waiting := false
	for i, request := range requests {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		state, err := r.runCall(ctx, request)
		if err != nil {
			return false, err
		}
		if state == callBarrier {
			held := requests[i+1:]
			for _, call := range held {
				if err := r.deferCall(ctx, call); err != nil {
					return false, err
				}
			}
			if len(held) > 0 {
				return true, r.checkpoint(ctx, PhaseToolBatch, batchOf(requests, held))
			}
			return true, nil
		}
		waiting = waiting || state == callPending
	}
	return waiting, nil
}

// batchOf is the checkpoint of a batch whose held calls are deferred.
func batchOf(requests, held []callRequest) *ToolBatch {
	batch := &ToolBatch{CallIDs: make([]string, 0, len(requests))}
	for _, request := range requests {
		batch.CallIDs = append(batch.CallIDs, request.id)
	}
	for _, request := range held {
		batch.DeferredIDs = append(batch.DeferredIDs, request.id)
	}
	return batch
}

// resumeDecided answers the run's decided approvals that have no result yet: a
// denied call gets the denial as its result and the calls it held back are not
// run, the granted ones run. Once none is pending, the calls deferred behind a
// barrier run. It reports whether the run still waits for approval.
func (r *run) resumeDecided(ctx context.Context) (bool, error) {
	decided, err := r.Inbox.Peek(ctx, r.task.RunID, InputDecided)
	if err != nil {
		return false, err
	}
	var granted []callRequest
	for _, input := range decided {
		if r.history.hasResult(input.ToolCallID) {
			continue
		}
		request := callRequest{id: input.ToolCallID, name: input.ToolName, args: input.Args, workingDir: input.WorkingDir, approved: true}
		if !input.Denied {
			granted = append(granted, request)
			continue
		}
		if err := r.denyCall(ctx, request, input.Reason); err != nil {
			return false, err
		}
	}
	if _, err := r.runCalls(ctx, granted); err != nil {
		return false, err
	}
	pending, err := r.Approvals.Pending(ctx, r.task.RunID)
	if err != nil || pending {
		return pending, err
	}
	return r.runCalls(ctx, r.deferredCalls(""))
}
```

Replace `runCall`, `rejectCall` and `finishCall`, which no longer checkpoint:

```go
// runCall authorizes, journals and executes one call and reports where it stands.
func (r *run) runCall(ctx context.Context, req callRequest) (callState, error) {
	call := r.toolCall(req)
	decision, err := r.Tools.Authorize(ctx, req.name, call)
	if err != nil {
		return callDone, err
	}
	if !decision.Allowed {
		return callDone, r.rejectCall(ctx, req, decision.Reason)
	}
	if err := r.startCall(ctx, req); err != nil {
		return callDone, err
	}
	result, err := r.Tools.Execute(ctx, req.name, call)
	if err != nil {
		return callDone, err
	}
	if result.Approval == nil || req.approved {
		return callDone, r.finishCall(ctx, req, call, result)
	}
	if err := r.Approvals.Request(ctx, Pending{RunID: r.task.RunID, SessionID: r.task.SessionID, ToolCallID: req.id, ToolName: req.name, Request: *result.Approval}); err != nil {
		return callDone, err
	}
	if decision.Barrier {
		return callBarrier, nil
	}
	return callPending, nil
}
```

```go
// rejectCall journals a call that may not run with its error as the result, so the
// model can correct it.
func (r *run) rejectCall(ctx context.Context, req callRequest, reason string) error {
	if err := r.writeCall(ctx, req, true); err != nil {
		return err
	}
	result := tools.Result{Content: reason, IsError: true}
	if _, err := r.appendResult(ctx, req, result); err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
	return nil
}
```

```go
func (r *run) finishCall(ctx context.Context, req callRequest, call tools.Call, result tools.Result) error {
	if err := r.writeCall(ctx, req, true); err != nil {
		return err
	}
	message, err := r.appendResult(ctx, req, result)
	if err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
	r.Sink.Emit(Event{Kind: EventToolFinished, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name, ResultMessageID: message.ID, Result: result})
	return r.Tools.Finish(ctx, req.name, call, result, message)
}
```

Run: `go test -race ./internal/agent/...`
Expected: PASS (`TestRejectedCallStreakSurvivesAnApprovalPark` now reads the park checkpoint).

- [ ] **Step 4: Write the failing store and core tests**

`internal/store/sqlite_runs_test.go` (import `"reflect"`), add after `TestRunCheckpointKeepsEngineState`:

```go
func TestRunCheckpointKeepsTheToolBatch(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r1")
	batch := &agent.ToolBatch{CallIDs: []string{"c1", "c2", "c3"}, DeferredIDs: []string{"c3"}}
	if err := st.SaveRunCheckpoint(ctx, core.RunCheckpoint{RunID: "r1", Phase: core.RunCheckpointPhase(agent.PhaseToolBatch), Batch: batch, UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	stored, err := st.GetRunCheckpoint(ctx, "r1")
	if err != nil || !reflect.DeepEqual(stored.Batch, batch) {
		t.Fatalf("stored checkpoint = %+v err = %v", stored, err)
	}

	if err := st.SaveRunCheckpoint(ctx, core.RunCheckpoint{RunID: "r1", Phase: core.RunCheckpointPhaseModel, UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	if stored, err := st.GetRunCheckpoint(ctx, "r1"); err != nil || stored.Batch != nil {
		t.Fatalf("checkpoint after the batch = %+v err = %v", stored, err)
	}
}
```

In `internal/core/native_run_characterization_test.go` rename `TestNativeRunCheckpointsModelAndToolPhases` to `TestNativeRunCheckpointsModelAndToolBatchPhases`, make its `record` print the batch and expect the new phases:

```go
func TestNativeRunCheckpointsModelAndToolBatchPhases(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var run core.Run
	var seen []string
	record := func(label string) {
		checkpoint, err := db.GetRunCheckpoint(context.Background(), run.ID)
		if err != nil {
			seen = append(seen, label+":none")
			return
		}
		calls := ""
		if checkpoint.Batch != nil {
			calls = strings.Join(checkpoint.Batch.CallIDs, "+")
		}
		seen = append(seen, fmt.Sprintf("%s:%s:%s", label, checkpoint.Phase, calls))
	}
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		record("tool")
		return tools.Result{Content: "inspected"}, nil
	}}))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		record(fmt.Sprintf("model%d", calls))
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	_, run = saveCrashRecoveryRun(t, db, "checkpoints", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	want := []string{"model1:model:", "tool:tool_batch:call-1", "model2:model:"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("checkpoints = %v, want %v", seen, want)
	}
	waitForRecoveryCheckpointGone(t, db, run.ID)
}
```

and add after it:

```go
func TestParkedRunKeepsTheEngineCheckpoint(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "parked_checkpoint", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	checkpoint, err := db.GetRunCheckpoint(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Phase != core.RunCheckpointPhaseModel || checkpoint.ToolCallID != "" || !strings.Contains(string(checkpoint.EngineState), `"steps":1`) {
		t.Fatalf("parked checkpoint = %+v (%s)", checkpoint, checkpoint.EngineState)
	}
}
```

Run: `go test ./internal/store/ ./internal/core/ -run 'Checkpoint'`
Expected: FAIL to compile — `core.RunCheckpoint` has no field `Batch`.

- [ ] **Step 5: Store the batch**

`internal/core/run_checkpoint.go` — replace `RunCheckpoint`, `saveRunCheckpoint`, `saveEngineCheckpoint` and `markRunRecovery`:

```go
type RunCheckpoint struct {
	RunID      string             `json:"run_id"`
	Phase      RunCheckpointPhase `json:"phase"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolName   string             `json:"tool_name,omitempty"`
	// Batch is set in the engine's tool_batch phase.
	Batch          *agent.ToolBatch `json:"tool_batch,omitempty"`
	RecoveryCount  int              `json:"recovery_count,omitempty"`
	RecoveryReason string           `json:"recovery_reason,omitempty"`
	EngineState    json.RawMessage  `json:"engine_state,omitempty"`
	UpdatedAt      time.Time        `json:"updated_at"`
}
```

```go
func (c *Core) saveRunCheckpoint(ctx context.Context, runID string, phase RunCheckpointPhase, toolCallID string, toolName string) error {
	return c.updateRunCheckpoint(ctx, runID, func(checkpoint *RunCheckpoint) {
		checkpoint.Phase = phase
		checkpoint.ToolCallID = normalizeText(toolCallID)
		checkpoint.ToolName = normalizeText(toolName)
		checkpoint.Batch = nil
	})
}
```

```go
// saveEngineCheckpoint stores the engine's phase and batch together with its
// run counters; while a native run is active only its engine calls it.
func (c *Core) saveEngineCheckpoint(ctx context.Context, state agent.State) error {
	counters, err := json.Marshal(state.Counters)
	if err != nil {
		return err
	}
	return c.updateRunCheckpoint(ctx, state.RunID, func(checkpoint *RunCheckpoint) {
		checkpoint.Phase = RunCheckpointPhase(state.Phase)
		checkpoint.ToolCallID = ""
		checkpoint.ToolName = ""
		checkpoint.Batch = state.Batch
		checkpoint.EngineState = counters
	})
}
```

```go
func (c *Core) markRunRecovery(ctx context.Context, runID string) (RunCheckpoint, error) {
	store, ok := c.store.(RunCheckpointStore)
	if !ok {
		return RunCheckpoint{RunID: normalizeText(runID), Phase: RunCheckpointPhaseRecovering, RecoveryCount: 1, RecoveryReason: runRecoveryReasonDaemonRestart, UpdatedAt: c.now().UTC()}, nil
	}
	runID = normalizeText(runID)
	checkpoint, err := store.GetRunCheckpoint(ctx, runID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return RunCheckpoint{}, err
	}
	checkpoint.RunID = runID
	checkpoint.Phase = RunCheckpointPhaseRecovering
	checkpoint.ToolCallID = ""
	checkpoint.ToolName = ""
	checkpoint.Batch = nil
	checkpoint.RecoveryCount++
	checkpoint.RecoveryReason = runRecoveryReasonDaemonRestart
	checkpoint.UpdatedAt = c.now().UTC()
	if err := store.SaveRunCheckpoint(ctx, checkpoint); err != nil {
		return RunCheckpoint{}, err
	}
	return checkpoint, nil
}
```

`internal/store/schema.go`, after the `run_checkpoints.engine_state` `ensureColumn`:

```go
	if err := ensureColumn(db, "run_checkpoints", "tool_batch", `ALTER TABLE run_checkpoints ADD COLUMN tool_batch TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
```

`internal/store/sqlite_run_checkpoints.go` — replace `SaveRunCheckpoint` and `GetRunCheckpoint`:

```go
func (s *SQLiteStore) SaveRunCheckpoint(ctx context.Context, checkpoint core.RunCheckpoint) error {
	batch := ""
	if checkpoint.Batch != nil {
		encoded, err := json.Marshal(checkpoint.Batch)
		if err != nil {
			return fmt.Errorf("store: encode run checkpoint batch: %w", err)
		}
		batch = string(encoded)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_checkpoints(run_id, phase, tool_call_id, tool_name, tool_batch, recovery_count, recovery_reason, engine_state, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    phase = excluded.phase,
    tool_call_id = excluded.tool_call_id,
    tool_name = excluded.tool_name,
    tool_batch = excluded.tool_batch,
    recovery_count = excluded.recovery_count,
    recovery_reason = excluded.recovery_reason,
    engine_state = excluded.engine_state,
    updated_at = excluded.updated_at`,
		checkpoint.RunID,
		string(checkpoint.Phase),
		checkpoint.ToolCallID,
		checkpoint.ToolName,
		batch,
		checkpoint.RecoveryCount,
		checkpoint.RecoveryReason,
		string(checkpoint.EngineState),
		formatTime(checkpoint.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save run checkpoint: %w", err)
	}
	return nil
}
```

```go
func (s *SQLiteStore) GetRunCheckpoint(ctx context.Context, runID string) (core.RunCheckpoint, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT run_id, phase, tool_call_id, tool_name, tool_batch, recovery_count, recovery_reason, engine_state, updated_at
FROM run_checkpoints
WHERE run_id = ?`, runID)
	var checkpoint core.RunCheckpoint
	var phase string
	var batch string
	var engineState string
	var updatedAt string
	if err := row.Scan(
		&checkpoint.RunID,
		&phase,
		&checkpoint.ToolCallID,
		&checkpoint.ToolName,
		&batch,
		&checkpoint.RecoveryCount,
		&checkpoint.RecoveryReason,
		&engineState,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.RunCheckpoint{}, core.ErrNotFound
		}
		return core.RunCheckpoint{}, fmt.Errorf("store: get run checkpoint: %w", err)
	}
	checkpoint.Phase = core.RunCheckpointPhase(phase)
	if batch != "" {
		if err := json.Unmarshal([]byte(batch), &checkpoint.Batch); err != nil {
			return core.RunCheckpoint{}, fmt.Errorf("store: decode run checkpoint batch: %w", err)
		}
	}
	if engineState != "" {
		checkpoint.EngineState = json.RawMessage(engineState)
	}
	checkpoint.UpdatedAt = mustParseTime(updatedAt)
	return checkpoint, nil
}
```

- [ ] **Step 6: Leave an active run's checkpoint to its engine**

Replace the whole of `internal/core/tool_call_approval.go` (`createPendingApproval` keeps its behaviour for run-less calls and recovery; the new `requestApproval` does not touch the checkpoint):

```go
package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// createPendingApproval records the approval a call outside an active engine
// asked for and marks its run's checkpoint as waiting for it.
func (c *Core) createPendingApproval(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput, result tools.Result, execErr error) (tools.Result, *Approval, bool, error) {
	if result.Approval == nil || input.Approved {
		return result, nil, false, execErr
	}
	approval, err := c.requestApproval(ctx, prepared, *result.Approval)
	if err != nil {
		return tools.Result{}, nil, false, err
	}
	if err := c.saveRunCheckpoint(ctx, prepared.RunID, RunCheckpointPhaseWaitingApproval, prepared.ToolCallID, prepared.ToolName); err != nil {
		return tools.Result{}, nil, false, err
	}
	return result, &approval, true, execErr
}

// requestApproval stores a pending approval for the call and announces it; it
// leaves the run's checkpoint to the engine.
func (c *Core) requestApproval(ctx context.Context, prepared preparedToolCall, request tools.ApprovalRequest) (Approval, error) {
	paramsRaw, err := marshalJSONRaw(request.Params)
	if err != nil {
		return Approval{}, err
	}
	approval := Approval{
		ID:          c.newID("approval"),
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallRef: prepared.ToolCallID,
		ToolName:    prepared.ToolName,
		Description: request.Description,
		Action:      request.Action,
		Params:      paramsRaw,
		Path:        request.Path,
		Suggestion:  request.Suggestion,
		State:       ApprovalStatePending,
		RequestedAt: c.now().UTC(),
	}
	if err := c.store.CreateApproval(ctx, approval); err != nil {
		return Approval{}, err
	}
	c.publishEvent(Event{
		Type:      EventApprovalRequest,
		SessionID: prepared.SessionID,
		RunID:     approval.RunID,
		Payload: PermissionRequest{
			ID:          approval.ID,
			SessionID:   approval.SessionID,
			ToolCallID:  prepared.ToolCallID,
			ToolName:    approval.ToolName,
			Description: approval.Description,
			Action:      approval.Action,
			Params:      approval.Params,
			Path:        approval.Path,
			Suggestion:  approval.Suggestion,
		},
	})
	c.publishToolUpdate(prepared.SessionID, approval.RunID, ToolUpdate{
		ToolCallID: prepared.ToolCallID,
		ToolName:   prepared.ToolName,
		State:      ToolLifecycleWaitingApproval,
		RunID:      approval.RunID,
		SessionID:  prepared.SessionID,
		ApprovalID: approval.ID,
	})
	return approval, nil
}
```

`internal/core/agent_inbox.go` — `coreApprovals.Request`:

```go
func (a coreApprovals) Request(ctx context.Context, p agent.Pending) error {
	prepared := preparedToolCall{SessionID: p.SessionID, RunID: p.RunID, ToolName: p.ToolName, ToolCallID: p.ToolCallID}
	_, err := a.c.requestApproval(ctx, prepared, p.Request)
	return err
}
```

`internal/core/subagents.go`: in `DelegateTask` delete the checkpoint write that followed `createSubagentTaskRecord` (it ran on the parent's tool goroutine and nothing read it):

```go
	if err := c.saveRunCheckpoint(ctx, parentRunID, RunCheckpointPhaseWaitingSubagent, parentToolCallID, delegateTaskToolName); err != nil {
		return DelegateTaskResult{}, err
	}
```

and in `mirrorPendingSubagentApproval` (it may run while the parent's engine is active) replace the `createPendingApproval` call with:

```go
	if _, err := c.requestApproval(ctx, prepared, *request); err != nil {
		return true, err
	}
```

- [ ] **Step 7: Run the suite**

Run: `gofmt -w internal/agent internal/core internal/store && go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/agent/ports.go internal/agent/engine.go internal/agent/tools.go internal/agent/engine_test.go internal/agent/budget_test.go internal/core/run_checkpoint.go internal/core/tool_call_approval.go internal/core/agent_inbox.go internal/core/subagents.go internal/core/native_run_characterization_test.go internal/store/schema.go internal/store/sqlite_run_checkpoints.go internal/store/sqlite_runs_test.go
git commit -m "refactor(agent): only the engine checkpoints a run, per tool batch

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---
### Task 3: Concurrency keys

Every call gets a concurrency key: from the executor if it names one, else from its spec. The engine receives it in `Decision.Key` (used from Task 5).

**Files:**
- Create: `internal/tools/concurrency_key.go`, `internal/tools/concurrency_key_test.go`, `internal/core/concurrency_key_test.go`
- Modify: `internal/core/ports.go` (`ToolExecutor`), `internal/daemoncmd/tool_visibility.go`, `internal/core/subagents_tools.go` (`delegateTaskTool`), `internal/agent/ports.go` (`Decision`), `internal/core/agent_tools.go` (`coreTools.Authorize`)

- [ ] **Step 1: Write the failing tests**

`internal/tools/concurrency_key_test.go`:

```go
package tools

import (
	"context"
	"testing"
)

func TestConcurrencyKeyDefaults(t *testing.T) {
	call := Call{WorkingDir: "/work/repo/"}
	for _, tc := range []struct {
		name string
		spec Spec
		want string
	}{
		{"read-only file tool", coreDefinitionSpec(readToolName), ""},
		{"grep", coreDefinitionSpec(grepToolName), ""},
		{"edit", coreDefinitionSpec(editToolName), "dir:/work/repo"},
		{"bash", coreDefinitionSpec(bashToolName), "dir:/work/repo"},
		{"read-only MCP tool", Spec{Namespace: "mcp.browser", Effect: EffectReadOnly}, "mcp.browser"},
		{"mutating MCP tool", Spec{Namespace: "MCP.Browser", Effect: EffectMutation, Category: CategoryWeb}, "mcp.browser"},
		{"other mutating tool", Spec{Namespace: "module.storage", Effect: EffectMutation, Category: CategoryStorage}, "tool:module.storage"},
		{"other read-only tool", Spec{Namespace: "module.storage", Effect: EffectReadOnly, Category: CategoryStorage}, ""},
	} {
		if got := tc.spec.ConcurrencyKey(call); got != tc.want {
			t.Errorf("%s: key = %q, want %q", tc.name, got, tc.want)
		}
	}
}

type keyedExecutor struct{ spec Spec }

func (e keyedExecutor) Spec() Spec { return e.spec }
func (e keyedExecutor) Execute(context.Context, Call) (Result, error) {
	return Result{}, nil
}
func (e keyedExecutor) ConcurrencyKey(call Call) string { return "own:" + call.WorkingDir }

func TestRegistryConcurrencyKeyPrefersTheExecutorsOwnKey(t *testing.T) {
	own := keyedExecutor{spec: Spec{ID: "own", Name: "Own", Description: "own key", Risk: RiskSafe, Namespace: "test", Effect: EffectMutation, Category: CategoryAutomation, Profiles: []Profile{ProfileCoding}, OutputKind: OutputText, InputJSONSchema: []byte(`{}`)}}
	registry := NewRegistry(NewBashExecutor(), own)
	if err := registry.Err(); err != nil {
		t.Fatal(err)
	}
	call := Call{WorkingDir: "/work"}

	if got := registry.ConcurrencyKey("bash", call); got != "dir:/work" {
		t.Fatalf("bash key = %q", got)
	}
	if got := registry.ConcurrencyKey("own", call); got != "own:/work" {
		t.Fatalf("own key = %q", got)
	}
	if got := registry.ConcurrencyKey("missing", call); got != "" {
		t.Fatalf("unknown tool key = %q", got)
	}
}
```

`internal/core/concurrency_key_test.go`:

```go
package core_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestDelegatedChildrenHoldTheirOwnKeyPerDirectory(t *testing.T) {
	app, _, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	registry := tools.NewRegistry(core.SubagentToolExecutors(app)...)
	for _, tc := range []struct {
		args, want string
	}{
		{`{"goal":"count files"}`, "subagents:/work"},
		{`{"goal":"count files","working_dir":"/other/"}`, "subagents:/other"},
	} {
		if got := registry.ConcurrencyKey("delegate_task", tools.Call{WorkingDir: "/work", Args: []byte(tc.args)}); got != tc.want {
			t.Errorf("key for %s = %q, want %q", tc.args, got, tc.want)
		}
	}
}
```

Run: `go test ./internal/tools/ ./internal/core/ -run 'ConcurrencyKey|OwnKey'`
Expected: FAIL to compile — `spec.ConcurrencyKey undefined`, `registry.ConcurrencyKey undefined`.

- [ ] **Step 2: Name the keys**

`internal/tools/concurrency_key.go`:

```go
package tools

import (
	"path/filepath"
	"strings"
)

// ConcurrencyKeyProvider is implemented by executors whose calls need another
// concurrency key than their spec's default.
type ConcurrencyKeyProvider interface {
	ConcurrencyKey(call Call) string
}

// ConcurrencyKey names what a call must not share with a concurrent call of the
// same key: "" runs freely (read-only tools), an MCP tool shares its server, a
// mutating filesystem or shell tool its working directory and any other
// mutating tool its namespace.
func (s Spec) ConcurrencyKey(call Call) string {
	namespace := strings.ToLower(strings.TrimSpace(s.Namespace))
	switch {
	case strings.HasPrefix(namespace, "mcp."):
		return namespace
	case !s.Mutates():
		return ""
	}
	switch normalizeCategory(s.Category) {
	case CategoryFilesystem, CategoryShell:
		return WorkingDirKey(call.WorkingDir)
	}
	return "tool:" + namespace
}

// WorkingDirKey is the concurrency key of calls that change dir.
func WorkingDirKey(dir string) string {
	return "dir:" + filepath.Clean(dir)
}

// ConcurrencyKey is the key a call of the tool runs under; "" for an unknown tool.
func (r *Registry) ConcurrencyKey(toolID string, call Call) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	r.mu.RUnlock()
	if !ok {
		return ""
	}
	if provider, provides := registered.executor.(ConcurrencyKeyProvider); provides {
		return provider.ConcurrencyKey(call)
	}
	return registered.spec.ConcurrencyKey(call)
}
```

`internal/core/subagents_tools.go` (import `"path/filepath"`), before `delegateTaskTool.Execute`:

```go
// ConcurrencyKey lets one delegated child at a time work in a directory. It is
// not the directory's own key, which the child's calls take while this call
// holds its key.
func (t *delegateTaskTool) ConcurrencyKey(call tools.Call) string {
	var input delegateTaskInput
	_ = json.Unmarshal(call.Args, &input)
	return "subagents:" + filepath.Clean(firstNonEmpty(normalizeWorkingDir(input.WorkingDir), call.WorkingDir))
}
```

`internal/core/ports.go`, at the end of `ToolExecutor`:

```go
	// Subject is what permission rules match of a call.
	Subject(toolID string, call tools.Call) permission.Subject
	// ConcurrencyKey names what the call must not share with a concurrent call.
	ConcurrencyKey(toolID string, call tools.Call) string
}
```

`internal/daemoncmd/tool_visibility.go`, after `Subject`:

```go
func (e *setupAwareToolExecutor) ConcurrencyKey(toolID string, call tools.Call) string {
	if e == nil || e.inner == nil {
		return ""
	}
	return e.inner.ConcurrencyKey(toolID, call)
}
```

Run: `go test ./internal/tools/ ./internal/core/ -run 'ConcurrencyKey|OwnKey'`
Expected: PASS.

- [ ] **Step 3: Hand the key to the engine**

`internal/agent/ports.go`:

```go
// Decision says whether a requested tool call may run; Reason goes back to the model.
// A Barrier call that waits for approval holds back the calls after it in its batch.
// Calls sharing a non-empty Key run one at a time, across runs too.
type Decision struct {
	Allowed bool
	Reason  string
	Barrier bool
	Key     string
}
```

In `coreTools.Authorize` (`internal/core/agent_tools.go`) the last line becomes:

```go
	return agent.Decision{Allowed: true, Barrier: spec.Mutates(), Key: t.c.tools.ConcurrencyKey(spec.ID, call)}, nil
```

`call` there is the copy whose working directory `Authorize` already defaulted to the session's.

- [ ] **Step 4: Run the suite**

Run: `gofmt -w internal/tools internal/core internal/agent internal/daemoncmd && go build ./... && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/concurrency_key.go internal/tools/concurrency_key_test.go internal/core/concurrency_key_test.go internal/core/ports.go internal/core/subagents_tools.go internal/core/agent_tools.go internal/daemoncmd/tool_visibility.go internal/agent/ports.go
git commit -m "feat(tools): name each call's concurrency key

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `toolsched` keyed locks and batches

Pure scheduling primitives, tested on their own: `Locks` (one per daemon) and the generic `Batch` the engine runs calls on.

**Files:**
- Create: `internal/agent/toolsched/locks.go`, `internal/agent/toolsched/locks_test.go`
- Create: `internal/agent/toolsched/batch.go`, `internal/agent/toolsched/batch_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/agent/toolsched/locks_test.go`:

```go
package toolsched

import (
	"context"
	"errors"
	"testing"
)

func TestLocksHoldOneKeyAtATime(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "dir:/work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locks.Lock(context.Background(), "dir:/other"); err != nil {
		t.Fatalf("another key waited: %v", err)
	}
	if _, err := locks.Lock(context.Background(), ""); err != nil {
		t.Fatalf("the empty key waited: %v", err)
	}
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := locks.Lock(stopped, "dir:/work"); !errors.Is(err, context.Canceled) {
		t.Fatalf("second holder err = %v, want it kept out", err)
	}
	unlock()
	again, err := locks.Lock(context.Background(), "dir:/work")
	if err != nil {
		t.Fatalf("a freed key stayed locked: %v", err)
	}
	again()
}

func TestLocksWaiterGetsTheKeyOnceItIsFreed(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan error)
	go func() {
		release, err := locks.Lock(context.Background(), "k")
		if err == nil {
			release()
		}
		got <- err
	}()
	unlock()
	if err := <-got; err != nil {
		t.Fatal(err)
	}
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if len(locks.held) != 0 {
		t.Fatalf("free keys kept: %v", locks.held)
	}
}
```

`internal/agent/toolsched/batch_test.go`:

```go
package toolsched

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// receive fails the test instead of hanging when a call never signals.
func receive(t *testing.T, ch <-chan int) int {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("a call never started")
		return 0
	}
}

func drain(b *Batch[int]) []Done[int] {
	var out []Done[int]
	for b.Running() > 0 {
		out = append(out, b.Next())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

func TestBatchRunsCallsOfDifferentKeysAtOnce(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 8)
	started, release := make(chan int), make(chan struct{})
	for i, key := range []string{"", "", "dir:/a", "mcp.browser"} {
		b.Go(i, key, func(context.Context) int {
			started <- i
			<-release
			return i * 10
		})
	}

	for range 4 {
		receive(t, started)
	}
	close(release)

	for i, done := range drain(b) {
		if done.Index != i || done.Err != nil || done.Value != i*10 {
			t.Fatalf("done %d = %+v", i, done)
		}
	}
}

func TestBatchRunsCallsOfOneKeyInStartOrder(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 8)
	var mu sync.Mutex
	var order []int
	var inFlight, most atomic.Int32
	for i := range 4 {
		b.Go(i, "dir:/work", func(context.Context) int {
			n := inFlight.Add(1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			inFlight.Add(-1)
			return i
		})
	}

	drain(b)

	if most.Load() != 1 || len(order) != 4 || order[0] != 0 || order[1] != 1 || order[2] != 2 || order[3] != 3 {
		t.Fatalf("order = %v, most at once = %d", order, most.Load())
	}
}

func TestBatchKeyWaitsForAnotherRunsHolder(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "dir:/work")
	if err != nil {
		t.Fatal(err)
	}
	var held atomic.Bool
	held.Store(true)
	b := NewBatch[int](context.Background(), locks, 8)
	b.Go(0, "dir:/work", func(context.Context) int {
		if held.Load() {
			return -1
		}
		return 1
	})

	held.Store(false)
	unlock()

	if done := b.Next(); done.Err != nil || done.Value != 1 {
		t.Fatalf("done = %+v, want the call to run after the other holder", done)
	}
}

func TestBatchLimitsCallsRunningAtOnce(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 2)
	started, release := make(chan int), make(chan struct{})
	var inFlight, most atomic.Int32
	for i := range 3 {
		b.Go(i, "", func(context.Context) int {
			n := inFlight.Add(1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			started <- i
			<-release
			inFlight.Add(-1)
			return i
		})
	}

	receive(t, started)
	receive(t, started)
	close(release)
	receive(t, started)
	drain(b)

	if most.Load() != 2 {
		t.Fatalf("%d calls ran at once, want 2", most.Load())
	}
}

func TestBatchCallStoppedBeforeItsTurnDoesNotRun(t *testing.T) {
	locks := NewLocks()
	unlock, err := locks.Lock(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	b := NewBatch[int](ctx, locks, 8)
	var ran atomic.Bool
	b.Go(0, "k", func(context.Context) int {
		ran.Store(true)
		return 1
	})
	b.Go(1, "k", func(context.Context) int {
		ran.Store(true)
		return 2
	})

	cancel()

	for _, done := range drain(b) {
		if !errors.Is(done.Err, context.Canceled) {
			t.Fatalf("done = %+v, want context.Canceled", done)
		}
	}
	if ran.Load() {
		t.Fatal("a call ran after its batch stopped")
	}
}

func TestBatchReportsAPanickingCallAndFreesItsKey(t *testing.T) {
	b := NewBatch[int](context.Background(), NewLocks(), 8)
	b.Go(0, "k", func(context.Context) int { panic("boom") })
	b.Go(1, "k", func(context.Context) int { return 7 })

	got := drain(b)

	if !errors.Is(got[0].Err, ErrPanicked) || got[1].Err != nil || got[1].Value != 7 {
		t.Fatalf("done = %+v", got)
	}
}
```

Run: `go test ./internal/agent/toolsched/`
Expected: FAIL to compile — `undefined: NewLocks`, `undefined: NewBatch`.

- [ ] **Step 2: Write the locks**

`internal/agent/toolsched/locks.go`:

```go
package toolsched

import (
	"context"
	"sync"
)

// Locks is a keyed mutex shared by every run of the daemon: holders of one key
// run one at a time; the empty key is never locked.
type Locks struct {
	mu   sync.Mutex
	held map[string]*keyLock
}

type keyLock struct {
	slot  chan struct{}
	users int
}

// NewLocks returns an empty set of keyed locks.
func NewLocks() *Locks {
	return &Locks{held: map[string]*keyLock{}}
}

// Lock waits until key is free or ctx stops; unlock frees the key.
func (l *Locks) Lock(ctx context.Context, key string) (unlock func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" {
		return func() {}, nil
	}
	l.mu.Lock()
	lock := l.held[key]
	if lock == nil {
		lock = &keyLock{slot: make(chan struct{}, 1)}
		l.held[key] = lock
	}
	lock.users++
	l.mu.Unlock()
	select {
	case lock.slot <- struct{}{}:
		return func() {
			<-lock.slot
			l.leave(key, lock)
		}, nil
	case <-ctx.Done():
		l.leave(key, lock)
		return nil, ctx.Err()
	}
}

// leave forgets a key nobody holds or waits for.
func (l *Locks) leave(key string, lock *keyLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lock.users--; lock.users == 0 {
		delete(l.held, key)
	}
}
```

- [ ] **Step 3: Write the batch**

`internal/agent/toolsched/batch.go` (a call runs after the previous call with its key finished, then takes the key's lock, then one of the batch's slots; `safego.Run` turns a panic into `ErrPanicked` and the deferred unlock still frees the key):

```go
package toolsched

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/safego"
)

// ErrPanicked reports a call that panicked; the batch goes on.
var ErrPanicked = errors.New("tool call panicked")

// Batch runs the calls of one model reply concurrently: calls sharing a key run
// one at a time in the order they were started, each holding its key in Locks,
// and at most limit calls run at once. Only the goroutine that created the batch
// may call its methods; Next reports every started call exactly once.
type Batch[T any] struct {
	ctx     context.Context
	locks   *Locks
	slots   *Semaphore
	tails   map[string]chan struct{}
	done    chan Done[T]
	running int
}

// Done is a finished call: Value is what it returned, or Err says why it did
// not run (its context stopped) or did not return (ErrPanicked).
type Done[T any] struct {
	Index int
	Value T
	Err   error
}

// NewBatch returns a batch whose calls run under ctx.
func NewBatch[T any](ctx context.Context, locks *Locks, limit int) *Batch[T] {
	return &Batch[T]{ctx: ctx, locks: locks, slots: NewSemaphore(limit), tails: map[string]chan struct{}{}, done: make(chan Done[T])}
}

// Go starts call index under key; run gets the batch's context.
func (b *Batch[T]) Go(index int, key string, run func(context.Context) T) {
	var after, finished chan struct{}
	if key != "" {
		after = b.tails[key]
		finished = make(chan struct{})
		b.tails[key] = finished
	}
	b.running++
	go func() {
		done := Done[T]{Index: index, Err: ErrPanicked}
		safego.Run("toolsched.call", func() {
			done.Value, done.Err = b.call(key, after, run)
		})
		if finished != nil {
			close(finished)
		}
		b.done <- done
	}()
}

func (b *Batch[T]) call(key string, after <-chan struct{}, run func(context.Context) T) (T, error) {
	var zero T
	if after != nil {
		select {
		case <-after:
		case <-b.ctx.Done():
			return zero, b.ctx.Err()
		}
	}
	unlock, err := b.locks.Lock(b.ctx, key)
	if err != nil {
		return zero, err
	}
	defer unlock()
	release, err := b.slots.Acquire(b.ctx)
	if err != nil {
		return zero, err
	}
	defer release()
	return run(b.ctx), nil
}

// Running is how many started calls Next has not reported yet.
func (b *Batch[T]) Running() int {
	return b.running
}

// Next waits until a started call finishes and reports it.
func (b *Batch[T]) Next() Done[T] {
	done := <-b.done
	b.running--
	return done
}
```

- [ ] **Step 4: Run the tests with the race detector, repeatedly**

Run: `go test -race -count=20 ./internal/agent/toolsched/`
Expected: PASS (the panic test logs a recovered stack trace; that is expected).

- [ ] **Step 5: Run the suite and commit**

Run: `gofmt -w internal/agent/toolsched && go build ./... && go vet ./... && go test ./...`
Expected: PASS.

```bash
git add internal/agent/toolsched/locks.go internal/agent/toolsched/locks_test.go internal/agent/toolsched/batch.go internal/agent/toolsched/batch_test.go
git commit -m "feat(agent): keyed locks and concurrent batches for tool calls

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---
### Task 5: Parallel batches in the engine

`runCalls` moves to the new `internal/agent/batch.go` and runs calls concurrently on a `toolsched.Batch`. Admission (authorize, journal, `tool_batch` checkpoint), approval requests, result journaling and the loop guard stay on the engine goroutine; tool goroutines only call `Tools.Execute`. Core hands every engine the daemon's `Locks` and makes allowed mutations non-barriers. When the run's context stops, finished results are kept and the other calls are left to recovery (a canceled run answers them in Task 6).

How `runCalls` works (engine goroutine):

1. **admit** — from the first call not admitted, in call order: `Authorize`; a refused call is journaled finished and its error waits for its turn; an allowed call is journaled started (`tool.requested`) and, if it is a barrier that has not been approved, admission stops there. The calls not admitted are journaled `deferred`, the batch is checkpointed (`tool_batch`), and only then do the admitted calls start on the batch.
2. **settle** — each call that returns is recorded: a result waits for its turn; an approval request is made at once; if the call was the barrier, admission continues (step 1) when it did not ask and stops for good (the rest stays deferred) when it did.
3. **journal** — results are written in call order up to the first call still running.

**Files:**
- Create: `internal/agent/batch.go`, `internal/agent/parallel_test.go`, `internal/core/tool_batch_test.go`
- Modify: `internal/agent/tools.go`, `internal/agent/engine.go` (`Config`, `New`), `internal/agent/ports.go` (`Tools` doc), `internal/agent/agenttest/agenttest.go` (`Tools`, `Fixture`), `internal/agent/engine_test.go`
- Modify: `internal/core/core.go` (`Core`, `New`), `internal/core/run_execute.go` (`nativeEngine`), `internal/core/agent_tools.go` (`coreTools.Authorize`)

- [ ] **Step 1: Write the failing engine tests**

`internal/agent/parallel_test.go`:

```go
package agent_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// signal fails the test instead of hanging when an expected signal never comes.
func signal(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(10 * time.Second):
		t.Fatal("a tool call never signalled")
		return ""
	}
}

// gatedTool announces each call on started, waits for its release channel and
// announces its return on returned.
type gatedTool struct {
	started, returned chan string
	release           map[string]chan struct{}
}

func newGatedTool(ids ...string) *gatedTool {
	g := &gatedTool{started: make(chan string), returned: make(chan string, len(ids)), release: map[string]chan struct{}{}}
	for _, id := range ids {
		g.release[id] = make(chan struct{})
	}
	return g
}

func (g *gatedTool) run(call tools.Call) tools.Result {
	g.started <- call.ToolCallID
	<-g.release[call.ToolCallID]
	g.returned <- call.ToolCallID
	return tools.Result{Content: "body of " + call.ToolCallID}
}

// start runs the fixture's engine on a goroutine of its own.
func start(f *agenttest.Fixture, model agent.Model) <-chan agent.Outcome {
	outcome := make(chan agent.Outcome, 1)
	engine, task := f.Engine(), f.Task(model)
	go func() {
		result, err := engine.Run(context.Background(), task)
		if err != nil {
			result = agent.Outcome{Status: agent.StatusFailed, Err: err}
		}
		outcome <- result
	}()
	return outcome
}

func resultOrder(f *agenttest.Fixture) string {
	var ids []string
	for _, message := range f.Journal.Messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil {
				ids = append(ids, part.ToolResult.ToolCallID)
			}
		}
	}
	return strings.Join(ids, ",")
}

func eventOrder(f *agenttest.Fixture, kind agent.EventKind) string {
	var ids []string
	for _, event := range f.Sink.Events {
		if event.Kind == kind {
			ids = append(ids, event.ToolCallID)
		}
	}
	return strings.Join(ids, ",")
}

func TestReadsOfOneReplyRunAtOnceAndAreJournaledInCallOrder(t *testing.T) {
	f := agenttest.NewFixture()
	gate := newGatedTool("r1", "r2", "r3")
	f.Tools.Funcs["read"] = gate.run
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("g1", "ghost"), call("r2", "read"), call("r3", "read")), text("Done."))
	outcome := start(f, model)

	for range 3 {
		signal(t, gate.started)
	}
	for _, id := range []string{"r3", "r2", "r1"} {
		close(gate.release[id])
		if got := signal(t, gate.returned); got != id {
			t.Fatalf("returned %s, want %s", got, id)
		}
	}

	if got := <-outcome; got.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v", got)
	}
	if got := resultOrder(f); got != "r1,g1,r2,r3" {
		t.Fatalf("results journaled as %s, want call order", got)
	}
	if got := eventOrder(f, agent.EventToolFinished); got != "r1,r2,r3" {
		t.Fatalf("tool.finished order = %s", got)
	}
	if got := eventOrder(f, agent.EventToolRequested); got != "r1,r2,r3" {
		t.Fatalf("tool.requested order = %s", got)
	}
	request := model.Requests()[1]
	for _, id := range []string{"r1", "r2", "r3"} {
		if got := toolContent(request, id); got != "body of "+id {
			t.Fatalf("result of %s = %q", id, got)
		}
	}
	if split := agenttest.SplitToolPair(request); split != "" {
		t.Fatal(split)
	}
}

func TestCallsSharingAKeyRunOneAtATimeInCallOrder(t *testing.T) {
	f := agenttest.NewFixture()
	gate := newGatedTool("e1", "r1")
	var e1Done, e2SawE1 atomic.Bool
	f.Tools.Funcs["edit"] = func(call tools.Call) tools.Result {
		if call.ToolCallID == "e2" {
			e2SawE1.Store(e1Done.Load())
			return tools.Result{Content: "edited"}
		}
		result := gate.run(call)
		e1Done.Store(true)
		return result
	}
	f.Tools.Funcs["read"] = gate.run
	f.Tools.Keys = map[string]string{"edit": "dir:/work"}
	model := agenttest.NewScriptedModel(calls(call("e1", "edit"), call("e2", "edit"), call("r1", "read")), text("Done."))
	outcome := start(f, model)

	started := map[string]bool{signal(t, gate.started): true, signal(t, gate.started): true}
	if !started["e1"] || !started["r1"] {
		t.Fatalf("started %v, want the first edit and the read at once", started)
	}
	close(gate.release["r1"])
	close(gate.release["e1"])

	if got := <-outcome; got.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v", got)
	}
	if !e2SawE1.Load() {
		t.Fatal("the second edit ran before the first one finished")
	}
	if got := resultOrder(f); got != "e1,e2,r1" {
		t.Fatalf("results = %s", got)
	}
}

func TestRunsSharingLocksTakeTurnsOnAKey(t *testing.T) {
	locks := toolsched.NewLocks()
	gate := newGatedTool("a1")
	var aDone, bSawA atomic.Bool
	first, second := agenttest.NewFixture(), agenttest.NewFixture()
	for _, f := range []*agenttest.Fixture{first, second} {
		f.Locks = locks
		f.Tools.Keys = map[string]string{"bash": "dir:/work"}
	}
	first.Tools.Funcs["bash"] = func(call tools.Call) tools.Result {
		result := gate.run(call)
		aDone.Store(true)
		return result
	}
	second.Tools.Funcs["bash"] = func(tools.Call) tools.Result {
		bSawA.Store(aDone.Load())
		return tools.Result{Content: "ran"}
	}
	firstOutcome := start(first, agenttest.NewScriptedModel(calls(call("a1", "bash")), text("A done.")))
	signal(t, gate.started)
	secondOutcome := start(second, agenttest.NewScriptedModel(calls(call("b1", "bash")), text("B done.")))

	close(gate.release["a1"])

	for _, outcome := range []<-chan agent.Outcome{firstOutcome, secondOutcome} {
		if got := <-outcome; got.Status != agent.StatusCompleted {
			t.Fatalf("outcome = %+v", got)
		}
	}
	if !bSawA.Load() {
		t.Fatal("the second run's call ran while the first run held the key")
	}
}

func TestBarrierHoldsLaterCallsUntilItSettles(t *testing.T) {
	f := agenttest.NewFixture()
	gate := newGatedTool("m1")
	var m1Done, r2SawM1 atomic.Bool
	f.Tools.Funcs["migrate"] = func(call tools.Call) tools.Result {
		result := gate.run(call)
		m1Done.Store(true)
		return result
	}
	f.Tools.Funcs["read"] = func(tools.Call) tools.Result {
		r2SawM1.Store(m1Done.Load())
		return tools.Result{Content: "file body"}
	}
	f.Tools.Mutating = map[string]bool{"migrate": true}
	model := agenttest.NewScriptedModel(calls(call("m1", "migrate"), call("r2", "read")), text("Done."))
	outcome := start(f, model)

	signal(t, gate.started)
	close(gate.release["m1"])

	if got := <-outcome; got.Status != agent.StatusCompleted {
		t.Fatalf("outcome = %+v", got)
	}
	if !r2SawM1.Load() {
		t.Fatal("a call behind a barrier ran before the barrier settled")
	}
	for _, event := range f.Sink.Events {
		if event.Kind == agent.EventMessageCreated && event.Message.ID == "r2" {
			if !event.Message.Parts[0].ToolCall.Deferred {
				t.Fatal("the held call was not journaled deferred first")
			}
			break
		}
	}
	if got := phases(f.Journal.States); got != "model,tool_batch:m1+r2/r2,tool_batch:m1+r2,model" {
		t.Fatalf("checkpoints = %s", got)
	}
}

func TestParallelResultsExtendTheLoopStreakInCallOrder(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["status"] = statusTool
	model := agenttest.NewScriptedModel(calls(call("s1", "status"), call("s2", "status"), call("s3", "status")), text("Clean."))

	run(t, f, model)

	if note := lastMessage(model.Requests()[1]); !strings.Contains(note.Content, "You are repeating status") {
		t.Fatalf("second request ends with %+v, want the loop warning", note)
	}
}

func TestPanickingToolBecomesAnErrorResult(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["crash"] = func(tools.Call) tools.Result { panic("boom") }
	model := agenttest.NewScriptedModel(calls(call("c1", "crash")), text("Recovered."))

	outcome := run(t, f, model)

	result, ok := f.Journal.Result("c1")
	if outcome.Status != agent.StatusCompleted || !ok || !result.Parts[0].ToolResult.IsError {
		t.Fatalf("outcome = %+v result = %+v", outcome, result)
	}
}

// stoppedBatch runs [r1 read, w2 barrier blocking until the run stops, r3 read]
// and stops the run once r1's result is journaled.
func stoppedBatch(t *testing.T, canceled bool) (*agenttest.Fixture, agent.Outcome) {
	t.Helper()
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	f.Tools.Funcs["wait"] = func(tools.Call) tools.Result { return tools.Result{Content: "waited"} }
	f.Tools.Mutating = map[string]bool{"wait": true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Tools.OnExecute = func(ctx context.Context, name string, _ tools.Call) error {
		if name == "wait" {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	f.Journal.OnAppend = func(message transcript.Message) error {
		if message.Role == transcript.MessageRoleTool && message.Parts[0].ToolResult.ToolCallID == "r1" {
			f.Inbox.Cancel = canceled
			cancel()
		}
		return nil
	}
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("w2", "wait"), call("r3", "read")))

	outcome, err := f.Engine().Run(ctx, f.Task(model))
	if err != nil {
		t.Fatal(err)
	}
	return f, outcome
}

func TestInterruptedBatchLeavesUnfinishedCallsToRecovery(t *testing.T) {
	f, outcome := stoppedBatch(t, false)

	if outcome.Status != agent.StatusInterrupted {
		t.Fatalf("outcome = %+v", outcome)
	}
	if got := resultOrder(f); got != "r1" {
		t.Fatalf("results = %s, want only the finished call", got)
	}
	if message, _ := f.Journal.Message("w2"); message.Parts[0].ToolCall.Deferred {
		t.Fatal("the call in flight is marked deferred")
	}
	if message, _ := f.Journal.Message("r3"); !message.Parts[0].ToolCall.Deferred {
		t.Fatal("the call that never started lost its deferred mark")
	}
}
```

In `internal/agent/engine_test.go` (import `"slices"`) calls of a batch may now execute in any order, so `executedIDs` sorts:

```go
// executedIDs lists the executed calls sorted, as calls of a batch run in any order.
func executedIDs(f *agenttest.Fixture) string {
	ids := make([]string, 0, len(f.Tools.Calls))
	for _, call := range f.Tools.Calls {
		ids = append(ids, call.ToolCallID)
	}
	slices.Sort(ids)
	return strings.Join(ids, ",")
}
```

In `TestMutatingApprovalDefersTheRestOfTheBatch` the second expectation becomes

```go
	if got := executedIDs(f); got != "r1,r2,w1,w1,w2" {
		t.Fatalf("executed = %s, want r1,r2,w1,w1,w2", got)
	}
```

and in `TestBatchCheckpointsNameTheCallsAndTheDeferredOnes` the call behind the barrier is deferred from the first checkpoint on:

```go
	if got := phases(f.Journal.States); got != "model,tool_batch:r1+w1+r2/r2,model" {
		t.Fatalf("checkpoints = %s", got)
	}
```

```go
	if got := phases(f.Journal.States[3:]); got != "tool_batch:w1,tool_batch:r2,model" {
		t.Fatalf("checkpoints after the grant = %s", got)
	}
```

Run: `go test ./internal/agent/`
Expected: FAIL to compile — `f.Tools.Keys undefined`, `f.Locks undefined`, and `OnExecute` has the wrong signature.

- [ ] **Step 2: Make the fakes safe for concurrent calls**

In `internal/agent/agenttest/agenttest.go` (import `"sync"`) replace `Tools`, `Tools.Authorize` and `Tools.Execute` (`Specs` and `Finish` are unchanged). `Calls` stays a field: `Run` returns only after every call goroutine finished, so tests read it afterwards without a lock.

```go
// Tools authorizes registered names only, making the Mutating ones barriers
// with their key from Keys, and records executed calls in start order (read
// Calls after Run returns) and finished ones. OnExecute runs first, on the
// call's goroutine, and fails the call with its error; OnFinish likewise.
type Tools struct {
	Funcs     map[string]ToolFunc
	Mutating  map[string]bool
	Keys      map[string]string
	Calls     []tools.Call
	Finished  []string
	OnExecute func(ctx context.Context, name string, call tools.Call) error
	OnFinish  func(name string, call tools.Call) error
	// SpecReads counts tool listings.
	SpecReads int
	mu        sync.Mutex
}
```

```go
func (t *Tools) Authorize(_ context.Context, name string, _ tools.Call) (agent.Decision, error) {
	if _, ok := t.Funcs[name]; !ok {
		return agent.Decision{Reason: fmt.Sprintf("invalid input: unknown tool %q", name)}, nil
	}
	return agent.Decision{Allowed: true, Barrier: t.Mutating[name], Key: t.Keys[name]}, nil
}
```

```go
func (t *Tools) Execute(ctx context.Context, name string, call tools.Call) (tools.Result, error) {
	if t.OnExecute != nil {
		if err := t.OnExecute(ctx, name, call); err != nil {
			return tools.Result{}, err
		}
	}
	t.mu.Lock()
	t.Calls = append(t.Calls, call)
	t.mu.Unlock()
	return t.Funcs[name](call), nil
}
```

In `Fixture` replace the `ModelSlots` field added in Task 1 with:

```go
	// ModelSlots and Locks are shared by the engines of fixtures that should
	// compete for model requests and concurrency keys.
	ModelSlots *toolsched.Semaphore
	Locks      *toolsched.Locks
	ids        int
}
```

and in `Fixture.Engine` pass `Locks: f.Locks,` after `ModelSlots: f.ModelSlots,`.

- [ ] **Step 3: Give the engine its locks**

`internal/agent/engine.go` — `Config` gains `Locks` and `New` defaults it:

```go
// Config wires the ports of one run.
type Config struct {
	Journal     Journal
	Tools       Tools
	Approvals   Approvals
	Inbox       Inbox
	Sink        Sink
	Prompts     Prompts
	Attachments agentcontext.AttachmentReader
	Now         func() time.Time
	NewID       func(prefix string) string
	// Sleep waits d or until ctx stops; nil uses a real timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// ModelSlots bounds the model requests of every run sharing it; nil is unbounded.
	ModelSlots *toolsched.Semaphore
	// Locks serialises tool calls sharing a concurrency key across every run
	// using it; nil gives the engine locks of its own.
	Locks *toolsched.Locks
}
```

```go
// New returns an engine over the given ports.
func New(cfg Config) *Engine {
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	if cfg.Locks == nil {
		cfg.Locks = toolsched.NewLocks()
	}
	return &Engine{cfg: cfg}
}
```

`internal/agent/ports.go` — the `Tools` doc now states who calls what:

```go
// Tools lists, authorizes, executes and finalizes the tools of one run. Execute
// errors are fatal; tool failures come back as IsError results. Execute runs on
// a goroutine of its own, concurrently with other calls of the batch; the other
// methods run on the engine goroutine. Finish runs after the result is written.
type Tools interface {
	Specs(ctx context.Context) []tools.Spec
	Authorize(ctx context.Context, name string, call tools.Call) (Decision, error)
	Execute(ctx context.Context, name string, call tools.Call) (tools.Result, error)
	Finish(ctx context.Context, name string, call tools.Call, result tools.Result, message transcript.Message) error
}
```

- [ ] **Step 4: Write the batch runner**

`internal/agent/batch.go`:

```go
package agent

import (
	"context"
	"errors"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// maxParallelCalls bounds how many calls of one batch run at once.
const maxParallelCalls = 8

// detachedWriteTimeout bounds the writes that keep a stopped batch's results.
const detachedWriteTimeout = 5 * time.Second

// panicResult answers a call whose tool panicked.
var panicResult = tools.Result{Content: "The tool failed unexpectedly; the daemon log has the details.", Status: tools.ResultStatusError, IsError: true}

// callState is where a call of a batch stands.
type callState int

const (
	callWaiting   callState = iota // not admitted yet
	callRunning                    // started, its tool has not returned
	callFinished                   // its result waits to be journaled
	callRejected                   // refused before it ran; its error waits to be journaled
	callAsked                      // waits for approval; it has no result
	callStopped                    // did not run or return because the run stopped
	callJournaled                  // its result is journaled
)

type batchCall struct {
	req    callRequest
	call   tools.Call
	key    string
	state  callState
	result tools.Result
}

type callOutcome struct {
	result tools.Result
	err    error
}

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
}

// runCalls runs the calls of one batch: admitted in call order, run at once
// (calls sharing a key one at a time), journaled in call order. A call waiting
// for approval parks while the rest runs, unless it is a barrier: the calls
// after it wait, and stay deferred if it asks. It reports whether the run waits.
func (r *run) runCalls(ctx context.Context, requests []callRequest) (bool, error) {
	if len(requests) == 0 {
		return false, nil
	}
	batchCtx, stop := context.WithCancel(ctx)
	defer stop()
	b := &batch{r: r, sched: toolsched.NewBatch[callOutcome](batchCtx, r.Locks, maxParallelCalls), barrier: -1}
	for _, request := range requests {
		b.calls = append(b.calls, &batchCall{req: request, call: r.toolCall(request)})
	}
	err := b.run(ctx)
	if err == nil {
		return b.waiting, nil
	}
	stop()
	b.drain()
	if ctx.Err() != nil {
		return false, errors.Join(ctx.Err(), b.keep(ctx))
	}
	return false, err
}

func (b *batch) run(ctx context.Context) error {
	if err := b.admit(ctx); err != nil {
		return err
	}
	for {
		if err := b.journal(ctx); err != nil {
			return err
		}
		if b.sched.Running() == 0 {
			return nil
		}
		if err := b.settle(ctx, b.sched.Next()); err != nil {
			return err
		}
		if err := b.admit(ctx); err != nil {
			return err
		}
	}
}

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
			if err := b.r.writeCall(ctx, c.req, true); err != nil {
				return err
			}
			c.state, c.result = callRejected, tools.Result{Content: decision.Reason, IsError: true}
			b.next++
			continue
		}
		if err := b.r.startCall(ctx, c.req); err != nil {
			return err
		}
		c.state, c.key = callRunning, decision.Key
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

func (b *batch) checkpoint() *ToolBatch {
	batch := &ToolBatch{CallIDs: make([]string, 0, len(b.calls))}
	for i, c := range b.calls {
		batch.CallIDs = append(batch.CallIDs, c.req.id)
		if i >= b.next {
			batch.DeferredIDs = append(batch.DeferredIDs, c.req.id)
		}
	}
	return batch
}

// launch runs call i on its own goroutine, which only executes the tool.
func (b *batch) launch(i int) {
	c := b.calls[i]
	name, call := c.req.name, c.call
	b.sched.Go(i, c.key, func(ctx context.Context) callOutcome {
		result, err := b.r.Tools.Execute(ctx, name, call)
		return callOutcome{result: result, err: err}
	})
}

// settle takes a finished call: one that asks for approval is requested at
// once, and a barrier that asked holds back the calls not admitted yet. A call
// that returns after the run's context stopped counts as stopped.
func (b *batch) settle(ctx context.Context, done toolsched.Done[callOutcome]) error {
	c := b.calls[done.Index]
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

// record stores a finished call's outcome and reports whether it asks for
// approval; a tool error is returned, as it fails the run.
func (b *batch) record(done toolsched.Done[callOutcome]) (bool, error) {
	c := b.calls[done.Index]
	switch {
	case errors.Is(done.Err, toolsched.ErrPanicked):
		c.state, c.result = callFinished, panicResult
	case done.Err != nil:
		c.state = callStopped
		return false, done.Err
	case done.Value.err != nil:
		c.state = callStopped
		return false, done.Value.err
	case done.Value.result.Approval != nil && !c.req.approved:
		c.state = callAsked
		return true, nil
	default:
		c.state, c.result = callFinished, done.Value.result
	}
	return false, nil
}

// journal writes the results ready so far in call order, up to the first call
// still running; a call waiting for approval has none.
func (b *batch) journal(ctx context.Context) error {
	for ; b.journaled < b.next; b.journaled++ {
		c := b.calls[b.journaled]
		switch c.state {
		case callRunning:
			return nil
		case callFinished:
			if err := b.r.finishCall(ctx, c.req, c.call, c.result); err != nil {
				return err
			}
			c.state = callJournaled
		case callRejected:
			if err := b.r.answerCall(ctx, c.req, c.result); err != nil {
				return err
			}
			c.state = callJournaled
		}
	}
	return nil
}

// drain waits for the calls still running once the batch stopped and drops
// what they return: they were stopped too.
func (b *batch) drain() {
	for b.sched.Running() > 0 {
		b.calls[b.sched.Next().Index].state = callStopped
	}
}

// keep journals, once the run's context stopped, the results of the calls that
// finished before; the others are left to crash recovery.
func (b *batch) keep(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()
	for _, c := range b.calls[b.journaled:] {
		var err error
		switch c.state {
		case callFinished:
			err = b.r.finishCall(ctx, c.req, c.call, c.result)
		case callRejected:
			err = b.r.answerCall(ctx, c.req, c.result)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Drop the sequential runner from `tools.go`**

In `internal/agent/tools.go` delete `callState` with its constants, `runCall`, the sequential `runCalls` and `batchOf`. `executeBatch`'s doc becomes `// executeBatch runs the response's tool calls as one batch.`. Replace `rejectCall` and add `answerCall` after it:

```go
// rejectCall journals a call that did not run with result, an error the model
// can act on.
func (r *run) rejectCall(ctx context.Context, req callRequest, result tools.Result) error {
	if err := r.writeCall(ctx, req, true); err != nil {
		return err
	}
	return r.answerCall(ctx, req, result)
}

// answerCall journals the result of a call already journaled as finished.
func (r *run) answerCall(ctx context.Context, req callRequest, result tools.Result) error {
	if _, err := r.appendResult(ctx, req, result); err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
	return nil
}
```

`denyCall` passes the error result:

```go
// denyCall answers a denied call with the denial and every call its barrier held
// back with an error, so the model re-plans instead of running them.
func (r *run) denyCall(ctx context.Context, req callRequest, reason string) error {
	for _, held := range r.deferredCalls(req.id) {
		if err := r.rejectCall(ctx, held, tools.Result{Content: fmt.Sprintf("Not run: an earlier call in this batch was denied (%s).", req.name), IsError: true}); err != nil {
			return err
		}
	}
	return r.finishCall(ctx, req, r.toolCall(req), DenialResult(reason))
}
```

Run: `go test -race -count=5 ./internal/agent/...`
Expected: PASS (the panic test logs a recovered stack trace).

- [ ] **Step 6: Write the failing core tests**

`internal/core/tool_batch_test.go` (the second test fails without shared locks; the third deadlocks — and times out — if a delegation took its child's `dir:` key):

```go
package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// sessionIn moves a test session into dir under mode.
func sessionIn(t *testing.T, db *store.SQLiteStore, session core.Session, dir string, mode core.PermissionMode) {
	t.Helper()
	session.WorkingDir, session.PermissionMode = dir, mode
	if err := db.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
}

// toolResults reads the tool results of a request by call ID, in request order.
func toolResults(request providers.Request) []string {
	var out []string
	for _, message := range request.Messages {
		if message.ToolCallID != "" {
			out = append(out, message.ToolCallID+"="+message.Content)
		}
	}
	return out
}

func TestReadsOfOneReplyRunAtOnceInANativeRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	started, release := make(chan struct{}, 2), make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		started <- struct{}{}
		<-release
		return tools.Result{Content: "inspected " + call.ToolCallID}, nil
	}}))
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if results = toolResults(request); len(results) > 0 {
			return providers.Response{Text: "Done."}, nil
		}
		return providers.Response{ToolCalls: []providers.ToolCall{
			{ID: "call-a", Name: "inspect_state", Arguments: []byte(`{"path":"a"}`)},
			{ID: "call-b", Name: "inspect_state", Arguments: []byte(`{"path":"b"}`)},
		}}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "parallel_reads", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()

	waitRecoverySignal(t, started, "first read")
	waitRecoverySignal(t, started, "second read while the first runs")
	close(release)

	if err := waitRecoveryError(t, done, "parallel run"); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if strings.Join(results, "|") != "call-a=inspected call-a|call-b=inspected call-b" {
		t.Fatalf("model read %q", results)
	}
}

func TestAllowedMutationsOfTwoSessionsInOneDirectoryTakeTurns(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	dir := t.TempDir()
	first, firstRun := saveCrashRecoveryRun(t, db, "turns_a", core.RunStatusAccepted, false)
	second, secondRun := saveCrashRecoveryRun(t, db, "turns_b", core.RunStatusAccepted, false)
	sessionIn(t, db, first, dir, core.PermissionModeFullAuto)
	sessionIn(t, db, second, dir, core.PermissionModeFullAuto)
	firstStarted, secondReading, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var firstDone, secondSawFirst atomic.Bool
	mutate := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		if call.SessionID == first.ID {
			close(firstStarted)
			<-release
			firstDone.Store(true)
		} else {
			secondSawFirst.Store(firstDone.Load())
		}
		return tools.Result{Content: "mutated"}, nil
	}}
	inspect := funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		close(secondReading)
		return tools.Result{Content: "inspected"}, nil
	}}
	app.WithTools(tools.NewRegistry(mutate, inspect))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		switch {
		case len(toolResults(request)) > 0:
			return providers.Response{Text: "Done."}, nil
		case request.CacheKey == first.ID:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-first", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		default:
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-second", Name: "mutate_state", Arguments: []byte(`{}`)},
				{ID: "call-second-read", Name: "inspect_state", Arguments: []byte(`{}`)},
			}}, nil
		}
	})})
	done := make(chan error, 2)
	go func() { done <- app.ExecuteRun(context.Background(), firstRun.ID) }()
	waitRecoverySignal(t, firstStarted, "first session's mutation")
	go func() { done <- app.ExecuteRun(context.Background(), secondRun.ID) }()
	waitRecoverySignal(t, secondReading, "second session's batch")

	close(release)

	for range 2 {
		if err := waitRecoveryError(t, done, "both runs"); err != nil {
			t.Fatal(err)
		}
	}
	assertRecoveryRunStatus(t, db, firstRun.ID, core.RunStatusCompleted)
	assertRecoveryRunStatus(t, db, secondRun.ID, core.RunStatusCompleted)
	if !secondSawFirst.Load() {
		t.Fatal("the second session mutated the directory while the first one did")
	}
}

func TestDelegatedChildChangesTheParentsDirectoryWithoutDeadlock(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	edit := recoveryToolSpec("mutate_state", tools.EffectMutation)
	edit.Risk, edit.ApprovalMode = tools.RiskSafe, tools.ApprovalNever
	edits := 0
	app.WithTools(tools.NewRegistry(append(core.SubagentToolExecutors(app), funcTool{spec: edit, fn: func(context.Context, tools.Call) (tools.Result, error) {
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
			args, _ := json.Marshal(map[string]string{"goal": "edit the file", "runtime": "matrixclaw"})
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: args}}}, nil
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

Run: `go test ./internal/core/ -run 'TestReadsOfOneReplyRunAtOnceInANativeRun|TestAllowedMutationsOfTwoSessionsInOneDirectoryTakeTurns|TestDelegatedChildChangesTheParentsDirectoryWithoutDeadlock'`
Expected: FAIL — `TestAllowedMutationsOfTwoSessionsInOneDirectoryTakeTurns`: "the second session mutated the directory while the first one did" (each engine still has locks of its own). With shared locks but the old barrier it would time out instead: a mutation stays a barrier in `full_auto`, so the read behind it could not start while it waits for the key.

- [ ] **Step 7: Share the locks and relax the barrier in core**

`internal/core/core.go`: in `Core`, after `modelSlots`, add

```go
	// toolLocks serialises tool calls sharing a concurrency key across runs.
	toolLocks *toolsched.Locks
```

and replace `New`:

```go
func New(store Store) *Core {
	return &Core{
		store:         store,
		activeRuns:    map[string]*activeRun{},
		scheduledRuns: map[string]time.Time{},
		sessionGates:  map[string]*sync.Mutex{},
		events:        newEventBus(),
		now:           time.Now,
		newID:         defaultID,
		historyLimit:  50,
		lifetime:      context.Background(),
		budgets:       DefaultRunBudgets(),
		modelSlots:    toolsched.NewSemaphore(DefaultModelConcurrency),
		toolLocks:     toolsched.NewLocks(),
	}
}
```

`internal/core/run_execute.go`, in `nativeEngine`:

```go
	engine := agent.New(agent.Config{
		Journal:     coreJournal{c: c},
		Tools:       coreTools{c: c, turn: turn},
		Approvals:   coreApprovals{c: c, sessionID: session.ID},
		Inbox:       coreInbox{c: c, session: session},
		Sink:        coreSink{c: c},
		Prompts:     &corePrompts{c: c, turn: turn},
		Attachments: c.attachments,
		Now:         func() time.Time { return c.now().UTC() },
		NewID:       c.newID,
		ModelSlots:  c.modelSlots,
		Locks:       c.toolLocks,
	})
```

`internal/core/agent_tools.go` — replace `coreTools.Authorize`:

```go
// Authorize rejects invalid calls and those a deny rule blocks; Execute acts on
// the verdict it kept for the call. A mutating call is a barrier unless a rule or
// the mode allows it, as it then cannot ask; a delegation still can, for its child.
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
	key := authorizedKey(call)
	t.c.authorized.Delete(key)
	check, err := t.c.checkPermission(ctx, call.SessionID, spec, call)
	if err != nil {
		return agent.Decision{}, err
	}
	if check.verdict.Effect == permission.Deny {
		return agent.Decision{Reason: blockedResult(check.verdict.Rule).Content}, nil
	}
	if call.ToolCallID != "" {
		t.c.authorized.Store(key, check)
	}
	barrier := spec.Mutates() && (check.verdict.Effect != permission.Allow || spec.ID == delegateTaskToolName)
	return agent.Decision{Allowed: true, Barrier: barrier, Key: t.c.tools.ConcurrencyKey(spec.ID, call)}, nil
}
```

- [ ] **Step 8: Run the suite**

Run: `gofmt -w internal/agent internal/core && go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/agent/batch.go internal/agent/parallel_test.go internal/agent/tools.go internal/agent/engine.go internal/agent/ports.go internal/agent/agenttest/agenttest.go internal/agent/engine_test.go internal/core/core.go internal/core/run_execute.go internal/core/agent_tools.go internal/core/tool_batch_test.go
git commit -m "feat(agent): run the tool calls of one reply in parallel by concurrency key

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---
### Task 6: A canceled batch answers its unfinished calls

When the user cancels a run in the middle of a batch, the calls that finished keep their results and every other call of the batch gets `Canceled by user.` — with `tool.finished` for calls that had started, as a plain result for calls that never did (deferred or not admitted). An interrupted run is unchanged: its unfinished calls stay for crash recovery.

**Files:**
- Modify: `internal/agent/batch.go` (`canceledResult`, `batch.keep`), `internal/agent/parallel_test.go`, `internal/core/native_run_characterization_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/agent/parallel_test.go`, before `TestInterruptedBatchLeavesUnfinishedCallsToRecovery`:

```go
func TestCanceledBatchKeepsFinishedResultsAndCancelsTheRest(t *testing.T) {
	f, outcome := stoppedBatch(t, true)

	if outcome.Status != agent.StatusInterrupted {
		t.Fatalf("outcome = %+v", outcome)
	}
	want := map[string]string{"r1": "file body", "w2": "Canceled by user.", "r3": "Canceled by user."}
	for id, content := range want {
		if result, ok := f.Journal.Result(id); !ok || result.Content != content {
			t.Fatalf("result of %s = %+v, want %q", id, result, content)
		}
	}
	if got := resultOrder(f); got != "r1,w2,r3" {
		t.Fatalf("results = %s", got)
	}
	if got := eventOrder(f, agent.EventToolFinished); got != "r1,w2" {
		t.Fatalf("tool.finished = %s, want no event for the call that never started", got)
	}
	if message, _ := f.Journal.Message("r3"); message.Parts[0].ToolCall.Deferred {
		t.Fatal("the canceled deferred call is still marked deferred")
	}
	if got := executedIDs(f); got != "r1" {
		t.Fatalf("executed = %s, want only r1 to have returned", got)
	}
}
```

In `internal/core/native_run_characterization_test.go`, `TestCancelDuringToolStopsRunWithoutAnotherModelCall` checks the answer after the `sealed` check:

```go
	answered := false
	for _, message := range sessionMessages(t, db, session.ID) {
		answered = answered || (message.Role == transcript.MessageRoleTool && message.Content == "Canceled by user.")
	}
	if !answered {
		t.Fatal("the call in flight was not answered as canceled")
	}
```

Run: `go test ./internal/agent/ -run TestCanceledBatch && go test ./internal/core/ -run TestCancelDuringToolStopsRunWithoutAnotherModelCall`
Expected: FAIL — `result of w2 …` or `result of r3 …, want "Canceled by user."` (map order decides which is reported first) and `the call in flight was not answered as canceled`.

- [ ] **Step 2: Answer the rest when the run was canceled**

In `internal/agent/batch.go` add after `panicResult`:

```go
// canceledResult answers a call a canceled run did not finish.
var canceledResult = tools.Result{Content: "Canceled by user.", Status: tools.ResultStatusError, IsError: true}
```

and replace `keep`:

```go
// keep journals, once the run's context stopped, the results of the calls that
// finished before; a canceled run also answers every other call as canceled.
func (b *batch) keep(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()
	canceled, err := b.r.Inbox.Canceled(ctx, b.r.task.RunID)
	if err != nil {
		return err
	}
	for _, c := range b.calls[b.journaled:] {
		switch {
		case c.state == callFinished:
			err = b.r.finishCall(ctx, c.req, c.call, c.result)
		case c.state == callRejected:
			err = b.r.answerCall(ctx, c.req, c.result)
		case !canceled || c.state == callJournaled:
			continue
		case c.state == callWaiting:
			err = b.r.rejectCall(ctx, c.req, canceledResult)
		default:
			err = b.r.finishCall(ctx, c.req, c.call, canceledResult)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Run the suite**

Run: `gofmt -w internal/agent internal/core && go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/`
Expected: PASS (`TestSteerIsRequeuedWhenCancelStopsItsToolResultWrite` still requeues its steer: the kept write fails like the first one).

- [ ] **Step 4: Commit**

```bash
git add internal/agent/batch.go internal/agent/parallel_test.go internal/core/native_run_characterization_test.go
git commit -m "feat(agent): a canceled batch answers the calls it did not finish

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Crash recovery answers every call of a batch in flight

Recovery used to stop at the first in-flight call that needed a retry approval; with parallel batches several calls can be in flight at once. Now every one is answered: read-only calls replay, each mutating call gets its own retry approval, and a still-running blocking subagent keeps the parent waiting first.

**Files:**
- Modify: `internal/core/run_recovery.go` (`prepareRunAfterCrash`), `internal/core/run_crash_recovery_integration_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/core/run_crash_recovery_integration_test.go` (it uses `toolResults` from `tool_batch_test.go`), before `TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate`:

```go
func TestRecoveryAnswersEveryCallOfABatchInFlight(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	inspect := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results = toolResults(request)
		return providers.Response{Text: "Done."}, nil
	})})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(mutation, inspect))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "batch", core.RunStatusRunning, false)
	at := run.StartedAt.Add(time.Second)
	saveRunRecoveryTestMessage(t, sqliteStore, agent.ToolCallMessage("tool_done", run.SessionID, run.ID, inspect.spec.ID, []byte(`{}`), true, at))
	done, err := agent.ToolResultMessage("result_done", run.SessionID, run.ID, "tool_done", inspect.spec.ID, tools.Result{Content: "done before the crash"}, at)
	if err != nil {
		t.Fatal(err)
	}
	saveRunRecoveryTestMessage(t, sqliteStore, done)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_read", inspect.spec.ID)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_write", mutation.spec.ID)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_write_too", mutation.spec.ID)
	deferred := agent.ToolCallMessage("tool_later", run.SessionID, run.ID, inspect.spec.ID, []byte(`{}`), false, at)
	deferred.Parts[0].ToolCall.Deferred = true
	saveRunRecoveryTestMessage(t, sqliteStore, deferred)

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusWaitingApproval)
	if inspect.callCount() != 1 || mutation.callCount() != 0 {
		t.Fatalf("after recovery: inspections = %d (want the read in flight replayed), mutations = %d", inspect.callCount(), mutation.callCount())
	}
	approvals, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 2 {
		t.Fatalf("pending approvals = %+v err = %v, want one retry per mutation in flight", approvals, err)
	}
	for _, approval := range approvals {
		if _, err := app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	if inspect.callCount() != 2 || mutation.callCount() != 2 {
		t.Fatalf("inspections = %d, mutations = %d, want the deferred read and both writes once", inspect.callCount(), mutation.callCount())
	}
	want := "tool_done=done before the crash|tool_read=recovered tool result|tool_write=recovered tool result|tool_write_too=recovered tool result|tool_later=recovered tool result"
	if strings.Join(results, "|") != want {
		t.Fatalf("model read %q", results)
	}
}
```

Run: `go test ./internal/core/ -run TestRecoveryAnswersEveryCallOfABatchInFlight`
Expected: FAIL — `pending approvals = [… tool_write …], want one retry per mutation in flight`.

- [ ] **Step 2: Answer every in-flight call**

In `prepareRunAfterCrash` (`internal/core/run_recovery.go`) replace the loop over `incompleteToolCallsForRun` with:

```go
	// Every call of a batch in flight is answered: read-only ones replay, mutating
	// ones ask again; a child still running keeps the parent waiting for it.
	waitApproval := false
	var waitSubagent *interruptedToolCall
	for _, interrupted := range incompleteToolCallsForRun(messages, run.ID) {
		disposition, err := c.recoverInterruptedTool(ctx, *run, interrupted, approvals)
		if err != nil {
			return false, err
		}
		switch disposition {
		case recoveryToolWaitApproval:
			waitApproval = true
		case recoveryToolWaitSubagent:
			if waitSubagent == nil {
				waitSubagent = &interrupted
			}
		}
	}
	if waitSubagent != nil {
		return false, c.saveRunCheckpoint(ctx, run.ID, RunCheckpointPhaseWaitingSubagent, waitSubagent.Call.ID, waitSubagent.Call.Name)
	}
	if waitApproval {
		return false, c.setRunStatus(ctx, run, RunStatusWaitingApproval, "")
	}
```

The rest of the function (sealing a partial reply, `recovering` checkpoint, `accepted` status) is unchanged.

- [ ] **Step 3: Run the suite**

Run: `gofmt -w internal/core && go build ./... && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/core/run_recovery.go internal/core/run_crash_recovery_integration_test.go
git commit -m "fix(core): recover every call of a tool batch in flight

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Prompt guidance for parallel calls

Spec §6: guidance ships with the feature. The model learns that the calls of one reply run at once and should be independent.

**Files:**
- Modify: `internal/agent/prompt/guidance.go` (`ToolUseDiscipline`), `internal/agent/prompt/guidance_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/prompt/guidance_test.go`:

```go
func TestToolUseDisciplineAsksForIndependentCallsInOneReply(t *testing.T) {
	text := ToolUseDiscipline()
	for _, want := range []string{"run at the same time", "independent reads, searches and inspections into one reply", "goes in a later reply"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool-use discipline lacks %q:\n%s", want, text)
		}
	}
}
```

Run: `go test ./internal/agent/prompt/`
Expected: FAIL — `tool-use discipline lacks "run at the same time"`.

- [ ] **Step 2: Add the guidance**

Replace `ToolUseDiscipline` in `internal/agent/prompt/guidance.go` (the new bullet is the second one):

```go
// ToolUseDiscipline is the tool-use guidance for models that can call tools.
func ToolUseDiscipline() string {
	return strings.TrimSpace(`Tool use discipline:
- Treat requests to do work as instructions to carry the task through to a verified result. A promise, plan, or successful intermediate tool call is not completion.
- Tool calls you make in one reply run at the same time and may finish in any order; their results come back in the order you made them. Put independent reads, searches and inspections into one reply as parallel calls. A call that needs another call's result or effect goes in a later reply.
- Inspect each tool result before deciding the next step. If a tool fails, use its error to correct the request or choose another approach; do not claim success or repeat the same failed call without a reason.
- Continue while useful authorized work remains. Ask a concise question only when missing information or permission actually blocks the next necessary step.
- Use the session plan for multi-step work and keep it consistent with observed results. In the final reply, report what was accomplished, how it was checked, and any remaining blocker.
- Before calling another tool, check whether existing tool results already contain the requested answer; if they do, stop tool use and reply.
- Do not run extra searches, browser snapshots, or verification calls just to improve confidence when the answer is already clear and source-backed.
- If a result is partly useful but has minor uncertainty, answer with that uncertainty instead of repeatedly searching, unless the user asked for exhaustive verification or the sources conflict.
- For simple lookups, prefer one direct path to the answer over parallel or repeated searches.`)
}
```

- [ ] **Step 3: Run the suite and commit**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS.

```bash
git add internal/agent/prompt/guidance.go internal/agent/prompt/guidance_test.go
git commit -m "feat(prompt): ask for independent calls in one reply

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Record the stage in the spec

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`

- [ ] **Step 1: Add the implementation notes**

Insert before `## 5. Providers`, after the stage 4b notes:

```markdown
### Implementation notes (as built, stage 4c)

- **Keys** come from `tools.Spec.ConcurrencyKey(call)` (the call carries the
  working directory): none for read-only tools, `mcp.<server>` for every MCP
  tool, `dir:<working dir>` for mutating filesystem and shell tools and
  `tool:<namespace>` for other mutating tools. An executor may name its own key
  (`tools.ConcurrencyKeyProvider`): `delegate_task` takes `subagents:<dir>`, so
  delegations into one directory run one at a time while the child's own calls
  take `dir:<dir>`; one shared key would deadlock parent and child. The engine
  gets the key in `Decision.Key`.
- **Batch**: calls are authorized and journaled in call order, then up to 8 run
  at once (`internal/agent/toolsched`); calls sharing a key run one at a time in
  call order and hold the daemon-wide lock of the key, so other runs and
  sessions wait too. Results, loop-guard counting, `Tools.Finish` and
  `tool.finished` follow call order (a finished call waits for the calls before
  it); `tool.requested` follows call order as calls start; the two streams
  interleave. Calls of one reply are independent: a read next to an edit of the
  same file may see it before or after the edit.
- **Barrier**: a mutating call is a barrier unless a rule or the mode allows it
  (it then cannot ask); `delegate_task` always is, as its child may ask. The
  calls after a barrier are journaled `deferred` at once and start when it
  returns without asking; if it asks they stay deferred as in stage 4a.
- **Checkpoints** are written by the engine goroutine only: `model` before each
  generation, `tool_batch{call_ids, deferred_ids}` (column
  `run_checkpoints.tool_batch`) whenever calls of a batch start, after they are
  journaled, and `model` when the run parks. Approval requests of engine runs
  and delegations no longer touch the run's checkpoint; run-less `ExecuteTool`
  and crash recovery keep their writes.
- **Recovery**: a call is completed (result), in flight (journaled without a
  result: read-only calls replay, mutating ones ask again, every call of the
  batch and not only the first) or not started (`deferred`: it runs when the
  run resumes). A call still waiting for its key or a slot counts as in flight.
- **Stop**: results of calls finished before the run's context stopped are
  journaled; what a call returns afterwards is dropped. A canceled run answers
  every other call of the batch `Canceled by user.`; an interrupted run leaves
  them to recovery. A panicking tool becomes an error result.
- **Model slots**: `daemon.model_concurrency` (default 4) bounds the model
  requests of all native runs, subagents and summaries included. A run holds a
  slot only inside `Generate`, so a parent blocked in `delegate_task` holds none.
```

- [ ] **Step 2: Final verification and commit**

Run: `go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/ ./internal/store/ ./internal/tools/ ./internal/daemoncmd/`
Expected: PASS.

```bash
git add docs/superpowers/specs/2026-09-23-long-running-agent-design.md
git commit -m "docs: stage 4c as built

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---
## Spec coverage (self-review)

| Spec requirement (§4 Scheduler, Approval barrier, Checkpoints; §2 Loop/Cancel; §6 Prompt; Stages 4c) | Task |
|---|---|
| `tools.Spec` gains `ConcurrencyKey`; read-only free, mutating fs/shell → working dir, MCP → `mcp.<server>` | 3 |
| Daemon-level keyed mutex serialising same-key calls across runs and sessions | 4 (`Locks`), 5 (one per `Core`, core test across two sessions) |
| Mutating shared-isolation children one at a time by working dir, without deadlocking parent and child | 3 (`subagents:<dir>`), 5 (deadlock test) |
| Up to 8 calls concurrently; results journaled in the model's original order | 4 (`Batch`), 5 (`runCalls`, ordering tests) |
| Unknown or invalid calls keep becoming error results | 5 (refused calls journaled in order; `TestUnknownToolIsReturnedAsErrorResult` unchanged) |
| Approval barrier: mutating call needing approval defers later calls; read-only asking call does not hold the batch | 2, 5 (`TestBarrierHoldsLaterCallsUntilItSettles`, 4a tests kept) |
| Engine-only checkpoints, phase `tool_batch{call_ids, deferred_ids}`; no per-call writes on the engine path | 2 |
| Crash recovery: completed / replay-or-ask / never started (deferred) | 2 (deferred journaled before running), 5 (interrupted batch test), 7 |
| Loop guard and budget counting deterministic with parallel results | 5 (`TestParallelResultsExtendTheLoopStreakInCallOrder`; results observed in call order) |
| Cancel: completed results kept, the rest "canceled" | 5 (keep), 6 |
| Daemon-wide provider semaphore shared by parent runs and subagents | 1 |
| Streaming/event order to clients defined | Decision 5; asserted in `TestReadsOfOneReplyRunAtOnceAndAreJournaledInCallOrder` |
| Prompt: issue independent reads/searches as parallel calls | 8 |
| Spec "as built" notes | 9 |

Deliberately not covered here: 6c's `readonly`/`worktree` children and resuming the parent on the whole batch, cost budgets.
