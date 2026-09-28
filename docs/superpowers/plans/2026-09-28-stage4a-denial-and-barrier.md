# Stage 4a — Denial and Approval Barrier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A denied approval no longer fails a run: the model reads `User denied: <reason>` as the call's result and goes on — for direct approvals, bridged subagent approvals (the child reads the denial, the parent keeps waiting) and approvals decided before a daemon restart. A mutating call that waits for approval becomes a barrier: the calls after it in the batch are journaled `deferred` and run only once every approval of the run is decided; the run resumes only then. Users can deny with a reason from the TUI and Telegram. The pre-existing `not found` error when granting a bridged subagent approval is fixed.

**Architecture:** Decisions reach the engine through the Inbox: `InputDecided` replaces `InputApproved` and carries denials with their reason; the engine writes the denial as the call's result (single writer). `Tools.Authorize` marks mutating tools as barriers (`Decision.Barrier`); `executeBatch` defers every later call (`ToolCallPart.Deferred`), and `resumeDecided` runs them in call order once `Approvals.Pending` is false. Core records decisions (`approvals.reason`), starts a run only when none of its approvals is pending, hands bridged decisions to the child's own approval, and crash recovery leaves denied and deferred calls to the resumed engine. Clients deny with a reason through a shared hidden command `/approval deny <id> [reason]` fed by a prompt.

**Tech Stack:** Go 1.26, SQLite (modernc, `ensureColumn` migrations), bubbletea v2 TUI, Telegram Bot API.

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session may commit in parallel: **locate code by function name, not line number**, run `git status --short` before each commit and stage only the paths listed in the task with explicit `git add <paths>` (never `-A`, `-u` or directories). `git rm` for deleted files.
- Run `gofmt -w` on every Go file you touch (code blocks below are not guaranteed to be column-aligned). Every commit must pass `go build ./... && go vet ./... && go test ./...` — run the full suite before committing.
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger. When a pre-existing test fails only because an expectation this plan changes on purpose, update that expectation and name it in your report.
- Every code block below was built and run against `05e45f2` in a scratch copy: the full suite passed after each task, also with `-race` for `internal/core` and `internal/agent`.

## Decisions taken in this plan (owner should know)

1. **Denial text.** `User denied: <reason>`, or `User denied.` without a reason; an error result (`is_error`, status `error`). Built by `agent.DenialResult`, used by the engine and by core for calls made outside a run.
2. **The engine writes denials.** Decided approvals reach the engine as `InputDecided` inputs (`Denied`, `Reason`); `ResolveApproval` never writes into a run's transcript, so the single-writer rule holds even when the run is still finishing its batch. Calls made outside a run (API, voice, MCP server) have no engine: `ResolveApproval` writes their denial result itself, as it already replays their grants.
3. **A run resumes once every approval is decided.** `ResolveApproval` starts the run only when none of the run's approvals is still pending (before, every grant restarted the run, which parked again). Rejected approvals no longer fail runs anywhere; `failRunByID` stays for real failures.
4. **Barrier = mutating tool.** `coreTools.Authorize` sets `Decision.Barrier = spec.Mutates()`. A mutating call that waits for approval defers **every** later call of its batch, read-only ones included: a read after an unapproved write usually depends on it, and results stay in call order. A read-only call that waits for approval (possible from stage 4b ask rules) parks without holding back the batch. Parallel read-only calls ahead of a barrier come with stage 4c.
5. **`deferred` is a transcript flag** `ToolCallPart.Deferred` (`"deferred": true`, additive JSON the iOS package ignores). A deferred call is journaled as a call message without a result and without a `tool.requested` event; when it starts, its message is updated (deferred cleared) and `tool.requested` is emitted. Deferred calls run in call order after the decisions; one that is a barrier again keeps the rest deferred.
6. **Crash recovery.** A call whose newest approval was denied is left to the resumed run, which writes the denial (no run failure, no replay). Deferred calls are not started: recovery neither replays them nor asks "retry after restart". A bridged approval that was denied is treated like a granted one there (the child owns the denial).
7. **Bridged subagent approvals.** Granting *or* denying the parent's bridge approval passes the decision (with the reason) to the child's own approval; the child runs its call or reads the denial; the parent keeps waiting and resumes when the child finishes. The parent's engine treats a decided bridge approval as "resume the delegate call" whichever way it went. Deleted: `resolveSubagentApprovalBridge`, `finishRejectedSubagentDelegateTool`, `queueSubagentCompletionRecord` (only the old denial path used it), `recoveryToolRunFailed`.
8. **The `not found` race** (scratch notes, stage 2a): after a bridged grant, `resumeParentForSubagentStatus` saw the child still `waiting_approval` (it had only been *scheduled*) and `mirrorPendingSubagentApproval` failed with `ErrNotFound` because the child's approval was already decided. `mirrorPendingSubagentApproval` now reports `(mirrored bool, err)` and treats a missing pending child approval as "not yet": the existing watcher keeps waiting for the child's next event. A deterministic regression test grants the bridge with a run starter that does not execute runs.
9. **An approved call that `Authorize` rejects still fails the run** in this stage (unchanged); stage 4b turns it into an error result, when deny rules can reject a granted call.
10. **Deny with reason in clients** goes through one hidden shared command `/approval deny <approval id> [reason]` (`commandcatalog.CommandApproval`, not in menus). `controlplane.DenyWithReasonPrompt(id)` is the prompt both clients open: the TUI permission dialog gets a "Deny with reason" button (key `r`), Telegram a "✍️ Deny with reason" button; the next text becomes the reason. The API accepts `{"approved": false, "reason": "..."}`; the iOS package keeps sending `{"approved": ...}` (compatible) and is not changed in this stage.
11. **TUI** drops its own `Runtime.resolveApproval` in favour of the shared `ControlplaneRuntime.ResolveApproval`.

Known and left as is: a decision that arrives in the milliseconds between the engine's last `Approvals.Pending` check and the run's status change to `waiting_approval` finds the run still active; `startRun` then does nothing and the run waits until the next daemon start recovers it (pre-existing, rare).

Out of scope: permission rules, "Always allow" and the removal of the session auto-approval shortcuts (stage 4b); concurrency keys and parallel batches (stage 4c).

## File structure

Created:
- `internal/store/sqlite_approvals_test.go` — approval reason round trip.
- `internal/core/subagents_approval_test.go` — bridged grant race and bridged denial.
- `internal/controlplane/approval.go`, `internal/controlplane/approval_test.go` — `/approval deny`.
- `clients/terminal/ui/surface/dialog/permissions_test.go` — "Deny with reason" key.
- `clients/telegram/approvals_test.go` — Telegram deny-with-reason flow.

Modified:
- `internal/transcript/message.go` (`ToolCallPart.Deferred`)
- `internal/agent/{ports,tools,messages}.go`, `internal/agent/agenttest/agenttest.go`, `internal/agent/engine_test.go`
- `internal/store/{schema,sqlite_approvals_files}.go`
- `internal/core/{types_approval,contracts,tool_approvals,agent_inbox,agent_tools,subagents,subagents_lifecycle,subagents_persistence,run_recovery}.go` and the tests `native_run_characterization_test.go`, `run_crash_recovery_integration_test.go`, `native_run_test.go`, `run_budget_test.go`, `context_guard_test.go`
- `internal/api/approvals.go`, `internal/daemonclient/runs.go`
- `internal/commandcatalog/catalog.go`, `internal/controlplane/{catalog,dispatcher}.go`, `internal/clientruntime/controlplane_runtime.go`
- TUI: `clients/terminal/ui/surface/dialog/{permissions,permissions_keys,permissions_events,permissions_render}.go`, `clients/terminal/chat/runtime/{app,app_update,app_input,app_approvals,app_dialog_actions,runtime_runs}.go`
- Telegram: `clients/telegram/{constants,keyboards,callbacks,run_render}.go`
- `docs/superpowers/specs/2026-09-23-long-running-agent-design.md` (implementation notes)

---
### Task 1: Keep the denial reason on approvals

**Files:**
- Modify: `internal/core/types_approval.go` (`Approval`)
- Modify: `internal/store/schema.go` (`applyCanonicalSchema`)
- Modify: `internal/store/sqlite_approvals_files.go` (`CreateApproval`, `GetApproval`, `UpdateApproval`, `ListApprovals`)
- Create: `internal/store/sqlite_approvals_test.go`

- [ ] **Step 1: Write the failing test**

`internal/store/sqlite_approvals_test.go`:

```go
package store_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestApprovalKeepsTheDenialReason(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	approval := core.Approval{ID: "a1", SessionID: "s1", RunID: "r1", ToolCallRef: "c1", ToolName: "bash", State: core.ApprovalStatePending, RequestedAt: testEpoch}
	if err := st.CreateApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	decided := testEpoch.Add(1)
	approval.State, approval.Reason, approval.DecidedAt = core.ApprovalStateRejected, "use the staging database", &decided
	if err := st.UpdateApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetApproval(ctx, "a1")
	if err != nil || stored.State != core.ApprovalStateRejected || stored.Reason != "use the staging database" {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	listed, err := st.ListApprovals(ctx, "s1", core.ApprovalStateRejected)
	if err != nil || len(listed) != 1 || listed[0].Reason != "use the staging database" {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/ -run TestApprovalKeepsTheDenialReason`
Expected: FAIL to compile — `approval.Reason undefined (type core.Approval has no field or method Reason)`.

- [ ] **Step 3: Add the field and the column**

In `internal/core/types_approval.go`, `Approval` gains `Reason` between `State` and `RequestedAt`:

```go
type Approval struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"session_id"`
	RunID       string          `json:"run_id,omitempty"`
	ToolCallRef string          `json:"tool_call_id,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	Description string          `json:"description,omitempty"`
	Action      string          `json:"action,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	Path        string          `json:"path,omitempty"`
	State       ApprovalState   `json:"state"`
	// Reason is why the user denied the call; the model reads it.
	Reason      string     `json:"reason,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
}
```

In `internal/store/schema.go` (`applyCanonicalSchema`), add this block right before the `CREATE INDEX IF NOT EXISTS idx_sessions_parent` statement:

```go
	if err := ensureColumn(db, "approvals", "reason", `ALTER TABLE approvals ADD COLUMN reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
```

In `internal/store/sqlite_approvals_files.go`, replace the four approval functions with:

```go
func (s *SQLiteStore) CreateApproval(ctx context.Context, approval core.Approval) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO approvals(id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, requested_at, decided_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		approval.ID,
		approval.SessionID,
		approval.RunID,
		approval.ToolCallRef,
		approval.ToolName,
		approval.Description,
		approval.Action,
		string(approval.Params),
		approval.Path,
		string(approval.State),
		approval.Reason,
		formatTime(approval.RequestedAt),
		nullableTime(approval.DecidedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create approval: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetApproval(ctx context.Context, approvalID string) (core.Approval, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, requested_at, decided_at
FROM approvals
WHERE id = ?`, approvalID)

	var approval core.Approval
	var state string
	var paramsJSON string
	var requestedAt string
	var decidedAt sql.NullString
	if err := row.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &state, &approval.Reason, &requestedAt, &decidedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Approval{}, core.ErrNotFound
		}
		return core.Approval{}, fmt.Errorf("store: get approval: %w", err)
	}
	approval.State = core.ApprovalState(state)
	approval.Params = json.RawMessage(paramsJSON)
	approval.RequestedAt = mustParseTime(requestedAt)
	if decidedAt.Valid {
		parsed := mustParseTime(decidedAt.String)
		approval.DecidedAt = &parsed
	}
	return approval, nil
}

func (s *SQLiteStore) UpdateApproval(ctx context.Context, approval core.Approval) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE approvals
SET state = ?, reason = ?, decided_at = ?
WHERE id = ?`,
		string(approval.State),
		approval.Reason,
		nullableTime(approval.DecidedAt),
		approval.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update approval: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update approval rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListApprovals(ctx context.Context, sessionID string, state core.ApprovalState) ([]core.Approval, error) {
	query := `
SELECT id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, requested_at, decided_at
FROM approvals
WHERE session_id = ?`
	args := []any{sessionID}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, string(state))
	}
	query += ` ORDER BY requested_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var approvals []core.Approval
	for rows.Next() {
		var approval core.Approval
		var rawState string
		var paramsJSON string
		var requestedAt string
		var decidedAt sql.NullString
		if err := rows.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &rawState, &approval.Reason, &requestedAt, &decidedAt); err != nil {
			return nil, fmt.Errorf("store: scan approval: %w", err)
		}
		approval.State = core.ApprovalState(rawState)
		approval.Params = json.RawMessage(paramsJSON)
		approval.RequestedAt = mustParseTime(requestedAt)
		if decidedAt.Valid {
			parsed := mustParseTime(decidedAt.String)
			approval.DecidedAt = &parsed
		}
		approvals = append(approvals, approval)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate approvals: %w", err)
	}
	return approvals, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `gofmt -w internal/core/types_approval.go internal/store/schema.go internal/store/sqlite_approvals_files.go internal/store/sqlite_approvals_test.go && go test ./internal/store/ -run TestApprovalKeepsTheDenialReason`
Expected: `ok`.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/types_approval.go internal/store/schema.go internal/store/sqlite_approvals_files.go internal/store/sqlite_approvals_test.go
git commit -m "feat(store): keep the reason of a denied approval

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The engine answers a denied call with the user's reason

Decided approvals (granted or denied) reach the engine as `InputDecided`; the engine writes `User denied: <reason>` as the call's result. `ResolveApproval` still fails the run on a denial until Task 3, so nothing changes for users yet.

**Files:**
- Modify: `internal/agent/ports.go` (`InputKind` constants, `Input`, `Inbox` doc)
- Modify: `internal/agent/messages.go` (add `DenialResult`)
- Modify: `internal/agent/tools.go` (`resumeApproved` → `resumeDecided`, `runCall`, add `toolCall`)
- Modify: `internal/agent/engine.go` (`step`)
- Modify: `internal/agent/agenttest/agenttest.go` (`Inbox`)
- Modify: `internal/core/agent_inbox.go` (`Peek`, `approved` → `decided`)
- Test: `internal/agent/engine_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/engine_test.go`:

```go
func TestDeniedCallGetsTheDenialAsItsResult(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{
		{"use the staging database", "User denied: use the staging database"},
		{"", "User denied."},
	} {
		f := agenttest.NewFixture()
		f.Tools.Funcs["write"] = writeTool
		f.Journal.Seed(agent.ToolCallMessage("w1", agenttest.SessionID, agenttest.RunID, "write", []byte(`{}`), false, f.Clock))
		f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`), Denied: true, Reason: tc.reason}}
		model := agenttest.NewScriptedModel(text("Understood."))

		outcome := run(t, f, model)

		if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 0 {
			t.Fatalf("outcome = %+v calls = %+v", outcome, f.Tools.Calls)
		}
		if result, ok := f.Journal.Result("w1"); !ok || !result.Parts[0].ToolResult.IsError || result.Content != tc.want {
			t.Fatalf("result = %+v", result)
		}
		if message, _ := f.Journal.Message("w1"); !message.Parts[0].ToolCall.Finished {
			t.Fatal("denied call not marked finished")
		}
		if got := toolContent(model.Requests()[0], "w1"); got != tc.want {
			t.Fatalf("request tool content = %q, want %q", got, tc.want)
		}
	}
}
```

In the same file, the two existing approval tests switch to the new inbox field. In `TestResolvedApprovalContinuesTheSameRun` the grant becomes:

```go
	f.Approvals.Grant = func(p agent.Pending) {
		f.Inbox.Decided = append(f.Inbox.Decided, agent.Input{Kind: agent.InputDecided, ToolCallID: p.ToolCallID, ToolName: p.ToolName, WorkingDir: "/work", Args: []byte(`{}`)})
	}
```

and in `TestGrantedApprovalIsExecutedBeforeTheNextModelCall` the seeded input becomes:

```go
	f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)}}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/agent/ -run 'TestDeniedCallGetsTheDenialAsItsResult'`
Expected: FAIL to compile — `f.Inbox.Decided undefined` / `undefined: agent.InputDecided`.

- [ ] **Step 3: Implement**

In `internal/agent/ports.go`, replace the input kinds, `Input` and the `Inbox` doc comment:

```go
const (
	InputSteer   InputKind = "steer"
	InputDecided InputKind = "decided"
)

// Input arrives from outside the engine: steer Text, or a decided approval;
// Denied approvals carry the user's Reason.
type Input struct {
	Kind       InputKind
	ID         string
	Text       string
	ToolCallID string
	ToolName   string
	WorkingDir string
	Args       json.RawMessage
	Denied     bool
	Reason     string
}

// Inbox delivers outside input. Peek never consumes: steers stay pending until the
// engine consumes their IDs, and InputDecided returns decided approvals whose call
// has no result yet.
type Inbox interface {
	Peek(ctx context.Context, runID string, kind InputKind) ([]Input, error)
	Consume(ctx context.Context, runID string, ids []string) error
	Canceled(ctx context.Context, runID string) (bool, error)
}
```

In `internal/agent/messages.go`, add before `ToolResultStatus`:

```go
// DenialResult is the result the model reads for a call the user denied.
func DenialResult(reason string) tools.Result {
	content := "User denied."
	if reason = strings.TrimSpace(reason); reason != "" {
		content = "User denied: " + reason
	}
	return tools.Result{Content: content, Status: tools.ResultStatusError, IsError: true}
}
```

In `internal/agent/tools.go`, replace `resumeApproved` with `resumeDecided`, replace `runCall`, and add `toolCall` right after it:

```go
// resumeDecided answers the run's decided approvals that have no result yet: a
// granted call runs, a denied one gets the denial as its result. It reports
// whether approvals of the run are still open.
func (r *run) resumeDecided(ctx context.Context) (bool, error) {
	decided, err := r.Inbox.Peek(ctx, r.task.RunID, InputDecided)
	if err != nil {
		return false, err
	}
	for _, input := range decided {
		if r.history.hasResult(input.ToolCallID) {
			continue
		}
		request := callRequest{id: input.ToolCallID, name: input.ToolName, args: input.Args, workingDir: input.WorkingDir, approved: true}
		if input.Denied {
			err = r.finishCall(ctx, request, r.toolCall(request), DenialResult(input.Reason))
		} else {
			_, err = r.runCall(ctx, request)
		}
		if err != nil {
			return false, err
		}
	}
	return r.Approvals.Pending(ctx, r.task.RunID)
}

// runCall authorizes, journals and executes one call; it reports whether the call
// now waits for approval.
func (r *run) runCall(ctx context.Context, req callRequest) (bool, error) {
	if req.id == "" {
		req.id = r.NewID("tool")
	}
	call := r.toolCall(req)
	decision, err := r.Tools.Authorize(ctx, req.name, call)
	if err != nil {
		return false, err
	}
	if !decision.Allowed {
		if req.approved {
			return false, errors.New(decision.Reason)
		}
		return false, r.rejectCall(ctx, req, decision.Reason)
	}
	if _, exists := r.history.message(req.id); !exists {
		if err := r.history.append(ctx, r.callMessage(req, false)); err != nil {
			return false, err
		}
		r.Sink.Emit(Event{Kind: EventToolRequested, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name})
	}
	if err := r.checkpoint(ctx, PhaseTool, req.id, req.name); err != nil {
		return false, err
	}
	result, err := r.Tools.Execute(ctx, req.name, call)
	if err != nil {
		return false, err
	}
	if result.Approval != nil && !req.approved {
		err := r.Approvals.Request(ctx, Pending{RunID: r.task.RunID, SessionID: r.task.SessionID, ToolCallID: req.id, ToolName: req.name, Request: *result.Approval})
		return err == nil, err
	}
	return false, r.finishCall(ctx, req, call, result)
}

func (r *run) toolCall(req callRequest) tools.Call {
	return tools.Call{
		SessionID:   r.task.SessionID,
		RunID:       r.task.RunID,
		ToolCallID:  req.id,
		Client:      r.task.Client,
		ExternalKey: r.task.ExternalKey,
		WorkingDir:  req.workingDir,
		Approved:    req.approved,
		Args:        req.args,
	}
}
```

In `internal/agent/engine.go` (`step`), the first call becomes:

```go
	waiting, err := r.resumeDecided(ctx)
```

In `internal/agent/agenttest/agenttest.go`, the inbox fake:

```go
// Inbox hands out pending steers, whose IDs are their text, until they are consumed,
// and decided approvals on every peek. Like the store, it fails on a stopped context.
type Inbox struct {
	Steers  []string
	Decided []agent.Input
	Cancel  bool
}
```

and in its `Peek` the second case becomes:

```go
	case agent.InputDecided:
		return in.Decided, nil
```

In `internal/core/agent_inbox.go`, `Peek` routes `agent.InputDecided` to `in.decided(ctx, runID)`:

```go
	case agent.InputDecided:
		return in.decided(ctx, runID)
```

and `approved` is replaced by:

```go
// decided returns the run's decided approvals whose call has no result yet, oldest
// first; a call's newest approval decides it. A bridged child approval resumes the
// parent's call whichever way the child's approval was decided.
func (in coreInbox) decided(ctx context.Context, runID string) ([]agent.Input, error) {
	approvals, err := in.c.store.ListApprovals(ctx, in.session.ID, "")
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var out []agent.Input
	for _, approval := range approvalsForRun(approvals, runID) {
		callID := strings.TrimSpace(approval.ToolCallRef)
		if callID == "" {
			continue
		}
		if _, ok := seen[callID]; ok {
			continue
		}
		seen[callID] = struct{}{}
		if approval.State == ApprovalStatePending {
			continue
		}
		done, err := in.c.store.HasToolResult(ctx, in.session.ID, callID)
		if err != nil {
			return nil, err
		}
		if done {
			continue
		}
		toolCall, err := in.c.sessionToolCallMessage(ctx, in.session.ID, callID)
		if err != nil {
			return nil, err
		}
		args, found := toolCallArgs(toolCall)
		if !found {
			return nil, fmt.Errorf("%w: tool call %s", ErrNotFound, toolCall.ID)
		}
		var spec tools.Spec
		if in.c.tools != nil {
			spec, _ = in.c.tools.Spec(approval.ToolName)
		}
		_, bridged := decodeSubagentApprovalBridge(approval)
		out = append(out, agent.Input{
			Kind:       agent.InputDecided,
			ToolCallID: callID,
			ToolName:   approval.ToolName,
			WorkingDir: workingDirForApprovalResume(in.session.WorkingDir, spec, approval.Path),
			Args:       args,
			Denied:     approval.State == ApprovalStateRejected && !bridged,
			Reason:     approval.Reason,
		})
	}
	slices.Reverse(out)
	return out, nil
}
```

(`ListApprovals` returns the newest first; the first approval seen for a call is its newest, and a pending newest approval keeps the call waiting.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/agent internal/core/agent_inbox.go && go test ./internal/agent/... ./internal/core/`
Expected: `ok` for every package.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/ports.go internal/agent/messages.go internal/agent/tools.go internal/agent/engine.go internal/agent/agenttest/agenttest.go internal/agent/engine_test.go internal/core/agent_inbox.go
git commit -m "feat(agent): answer a denied call with the user's reason

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `ResolveApproval` returns a denial to the model and resumes once every approval is decided

`ResolveApproval` takes the whole decision (`ApprovalResolveRequest{Approved, Reason}`), stores the reason, never fails a run, and starts the run only when none of its approvals is still pending. A call made outside a run gets its denial as its result. Bridged approvals keep their old flow here (Task 4 replaces it).

**Files:**
- Modify: `internal/core/contracts.go` (`ApprovalResolveRequest`)
- Modify: `internal/core/tool_approvals.go` (`ResolveApproval`, `resolveSubagentApprovalBridge`; add `approvalState`, `recordApprovalDecision`, `resumeDecidedRun`, `finishRunlessApproval`, `finishApprovalCall`; delete `finishRejectedSubagentDelegateTool`)
- Modify: `internal/api/approvals.go` (`handleApprovalByID`)
- Modify: `internal/daemonclient/runs.go` (`ResolveApproval`)
- Modify: `clients/terminal/chat/runtime/runtime_runs.go` (`resolveApproval`)
- Modify: `clients/telegram/callbacks.go` (`resolveApprovalCallback`), `clients/telegram/run_render.go` (`renderApprovalUpdates`)
- Test: `internal/core/native_run_characterization_test.go`; callers in `internal/core/{native_run_test,run_budget_test,context_guard_test,run_crash_recovery_integration_test,native_run_characterization_test}.go`

- [ ] **Step 1: Write the failing tests**

In `internal/core/native_run_characterization_test.go`, delete `TestNativeRunFailsWhenApprovalIsDenied` and add in its place:

```go
func TestDeniedApprovalReturnsTheReasonToTheModel(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, inspect := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, inspect))
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	calls := 0
	var denial string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		}
		denial = toolResultContent(request, "call-mutate")
		return providers.Response{Text: "Leaving the state alone."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "denied", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("pending approvals = %#v, err = %v", approvals, err)
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, core.ApprovalResolveRequest{Reason: "the state is shared"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if denial != "User denied: the state is shared" || mutations != 0 {
		t.Fatalf("model read %q, mutations = %d", denial, mutations)
	}
	stored, err := db.GetApproval(context.Background(), approvals[0].ID)
	if err != nil || stored.State != core.ApprovalStateRejected || stored.Reason != "the state is shared" {
		t.Fatalf("stored approval = %+v err = %v", stored, err)
	}
}

func askingReadTool(id string) funcTool {
	return funcTool{spec: recoveryToolSpec(id, tools.EffectReadOnly), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		if !call.Approved {
			return tools.Result{Approval: &tools.ApprovalRequest{ToolID: id, ToolCallID: call.ToolCallID, Action: "read_secret"}}, nil
		}
		return tools.Result{Content: "secret " + id}, nil
	}}
}

func TestRunResumesOnceEveryApprovalIsDecided(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(askingReadTool("read_a"), askingReadTool("read_b")))
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if len(results) == 0 && toolResultContent(request, "call-a") == "" {
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-a", Name: "read_a", Arguments: []byte(`{}`)},
				{ID: "call-b", Name: "read_b", Arguments: []byte(`{}`)},
			}}, nil
		}
		results = []string{toolResultContent(request, "call-a"), toolResultContent(request, "call-b")}
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "two-approvals", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 2 {
		t.Fatalf("pending approvals = %#v, err = %v", approvals, err)
	}

	byCall := map[string]string{}
	for _, approval := range approvals {
		byCall[approval.ToolCallRef] = approval.ID
	}
	if _, err := app.ResolveApproval(context.Background(), byCall["call-a"], core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 0 {
		t.Fatalf("run scheduled %d times while an approval was still open", got)
	}
	if _, err := app.ResolveApproval(context.Background(), byCall["call-b"], core.ApprovalResolveRequest{Reason: "not that one"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if strings.Join(results, "|") != "secret read_a|User denied: not that one" {
		t.Fatalf("model read %q", results)
	}
}

func TestDeniedCallOutsideARunGetsTheDenialAsItsResult(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate))
	session, _ := saveCrashRecoveryRun(t, db, "runless", core.RunStatusCompleted, false)
	pending, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "mutate_state", ToolCallID: "call-api", Args: []byte(`{}`)})
	if err != nil || pending.Approval == nil {
		t.Fatalf("ExecuteTool = %+v err = %v", pending, err)
	}

	if _, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Reason: "not now"}); err != nil {
		t.Fatal(err)
	}

	assertToolResultCount(t, db, session.ID, "call-api", 1)
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleTool && message.Content != "User denied: not now" {
			t.Fatalf("result = %q", message.Content)
		}
	}
	if mutations != 0 {
		t.Fatalf("mutations = %d", mutations)
	}
}

func toolResultContent(request providers.Request, callID string) string {
	for _, message := range request.Messages {
		if message.Role == "tool" && message.ToolCallID == callID {
			return message.Content
		}
	}
	return ""
}
```

Every other test that grants an approval passes the new request type. Run this from the repo root; it rewrites `ResolveApproval(<ctx>, <id>, true)` in the five files:

```bash
sed -i -E 's/ResolveApproval\(([^,]+), ([^,]+), true\)/ResolveApproval(\1, \2, core.ApprovalResolveRequest{Approved: true})/' \
  internal/core/native_run_test.go internal/core/run_budget_test.go internal/core/context_guard_test.go \
  internal/core/run_crash_recovery_integration_test.go internal/core/native_run_characterization_test.go
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go vet ./internal/core/`
Expected: FAIL — `cannot use core.ApprovalResolveRequest{…} (value of struct type core.ApprovalResolveRequest) as bool value in argument to app.ResolveApproval` (and `unknown field Reason`).

- [ ] **Step 3: Implement**

`internal/core/contracts.go`:

```go
// ApprovalResolveRequest is a user's decision on an approval; Reason explains a
// denial to the model.
type ApprovalResolveRequest struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}
```

`internal/core/tool_approvals.go`: add `"github.com/Suren878/matrixclaw/internal/agent"` to the imports; replace `ResolveApproval`, `resolveSubagentApprovalBridge` and `finishRejectedSubagentDelegateTool` (deleted) with:

```go
// ResolveApproval records the user's decision on an approval. A run resumes once
// none of its approvals is pending and reads a denial as the call's result; a
// call made outside a run is replayed or answered here.
func (c *Core) ResolveApproval(ctx context.Context, approvalID string, decision ApprovalResolveRequest) (Approval, error) {
	approval, err := c.store.GetApproval(ctx, normalizeText(approvalID))
	if err != nil {
		return Approval{}, err
	}
	if approval.State != ApprovalStatePending {
		if approval.State == approvalState(decision.Approved) {
			return approval, nil
		}
		return Approval{}, fmt.Errorf("%w: approval already resolved", ErrInvalidInput)
	}
	bridge, bridged := decodeSubagentApprovalBridge(approval)
	approval, err = c.recordApprovalDecision(ctx, approval, decision, bridged)
	if err != nil {
		return Approval{}, err
	}
	switch {
	case bridged:
		return approval, c.resolveSubagentApprovalBridge(ctx, approval, bridge, decision)
	case strings.TrimSpace(approval.RunID) == "":
		return approval, c.finishRunlessApproval(ctx, approval)
	default:
		return approval, c.resumeDecidedRun(ctx, approval.SessionID, approval.RunID)
	}
}

func approvalState(approved bool) ApprovalState {
	if approved {
		return ApprovalStateApproved
	}
	return ApprovalStateRejected
}

// recordApprovalDecision stores the decision and tells clients where the call
// stands; a bridged call keeps going whichever way its child's call was decided.
func (c *Core) recordApprovalDecision(ctx context.Context, approval Approval, decision ApprovalResolveRequest, bridged bool) (Approval, error) {
	decidedAt := c.now().UTC()
	approval.State = approvalState(decision.Approved)
	approval.DecidedAt = &decidedAt
	if !decision.Approved {
		approval.Reason = normalizeText(decision.Reason)
	}
	if err := c.store.UpdateApproval(ctx, approval); err != nil {
		return Approval{}, err
	}
	c.publishEvent(Event{
		Type:      EventApprovalResult,
		SessionID: approval.SessionID,
		RunID:     approval.RunID,
		Payload: PermissionNotification{
			ApprovalID: approval.ID,
			ToolCallID: approval.ToolCallRef,
			Granted:    decision.Approved,
			Denied:     !decision.Approved,
		},
	})
	update := ToolUpdate{
		ToolCallID: approval.ToolCallRef,
		ToolName:   approval.ToolName,
		State:      ToolLifecycleRequested,
		RunID:      approval.RunID,
		SessionID:  approval.SessionID,
		ApprovalID: approval.ID,
	}
	if !decision.Approved && !bridged {
		update.State = ToolLifecycleFailed
		update.Error = agent.DenialResult(approval.Reason).Content
	}
	c.publishToolUpdate(approval.SessionID, approval.RunID, update)
	return approval, nil
}

// resumeDecidedRun starts a run once none of its approvals is pending.
func (c *Core) resumeDecidedRun(ctx context.Context, sessionID string, runID string) error {
	pending, err := c.runHasPendingApprovals(ctx, sessionID, runID)
	if err != nil || pending {
		return err
	}
	return c.startRun(ctx, runID)
}

// finishRunlessApproval completes a call made outside a run (API, voice, MCP
// server): a grant replays it, a denial becomes its result.
func (c *Core) finishRunlessApproval(ctx context.Context, approval Approval) error {
	if approval.State == ApprovalStateApproved {
		_, err := c.replayApprovedTool(ctx, approval)
		return err
	}
	return c.finishApprovalCall(ctx, approval, agent.DenialResult(approval.Reason))
}

func (c *Core) resolveSubagentApprovalBridge(ctx context.Context, approval Approval, bridge subagentApprovalBridgeParams, decision ApprovalResolveRequest) error {
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

	if decision.Approved {
		if latest, err := c.store.GetSubagentTask(ctx, task.ID); err == nil {
			task = latest
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if !subagentTaskTerminalStatus(task.Status) {
			task, err = c.markSubagentTaskRunning(ctx, task)
			if err != nil {
				return err
			}
		}
		if task.Mode == SubagentTaskModeAsync {
			return nil
		}
		return c.resumeParentAfterSubagentTerminal(ctx, task)
	}

	summary := "Subagent approval denied"
	if bridge.ChildToolName != "" {
		summary += " for " + bridge.ChildToolName
	}
	task, err = c.finishSubagentTaskRecord(ctx, task, SubagentTaskStatusFailed, summary, summary, false)
	if err != nil {
		return err
	}
	if task.Mode == SubagentTaskModeAsync {
		c.publishSubagentToolUpdate(task)
		task, err = c.queueSubagentCompletionRecord(ctx, task)
		if err != nil {
			return err
		}
		return c.deliverPendingSubagentCompletionsForParent(ctx, task.ParentSessionID)
	}
	if err := c.finishApprovalCall(ctx, approval, tools.Result{Content: summary, Metadata: task, Status: tools.ResultStatusError, IsError: true}); err != nil {
		return err
	}
	if strings.TrimSpace(approval.RunID) != "" {
		return c.startRun(ctx, approval.RunID)
	}
	return nil
}

// finishApprovalCall writes result for the approval's call unless it has one.
func (c *Core) finishApprovalCall(ctx context.Context, approval Approval, result tools.Result) error {
	toolCallID := strings.TrimSpace(approval.ToolCallRef)
	done, err := c.store.HasToolResult(ctx, approval.SessionID, toolCallID)
	if err != nil || done {
		return err
	}
	toolCall, err := c.sessionToolCallMessage(ctx, approval.SessionID, toolCallID)
	if err != nil {
		return err
	}
	args, _ := toolCallArgs(toolCall)
	prepared := preparedToolCall{
		SessionID:  approval.SessionID,
		RunID:      approval.RunID,
		ToolName:   approval.ToolName,
		ToolCallID: toolCallID,
		Message:    toolCall,
	}
	_, _, err = c.finishToolCall(ctx, prepared, ExecuteToolInput{Args: args}, result)
	return err
}
```

`internal/api/approvals.go` (`handleApprovalByID`) passes the whole request:

```go
	approval, err := s.core.ResolveApproval(r.Context(), approvalID, req)
```

`internal/daemonclient/runs.go`:

```go
func (c *Client) ResolveApproval(ctx context.Context, approvalID string, request core.ApprovalResolveRequest) (core.Approval, error) {
	var response core.ApprovalResponse
	path := "/v1/approvals/" + escapedPath(approvalID) + "/resolve"
	if err := c.doJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return core.Approval{}, err
	}
	return response.Approval, nil
}
```

`clients/terminal/chat/runtime/runtime_runs.go` (`resolveApproval`), last line:

```go
	return client.ResolveApproval(ctx, approvalID, core.ApprovalResolveRequest{Approved: approved})
```

`clients/telegram/callbacks.go`: add `"github.com/Suren878/matrixclaw/internal/core"` to the imports; in `resolveApprovalCallback` the first line becomes:

```go
	approval, err := w.daemon(target.externalKey).ResolveApproval(ctx, approvalID, core.ApprovalResolveRequest{Approved: approved})
```

`clients/telegram/run_render.go` (`renderApprovalUpdates`), the auto-approval call becomes:

```go
			if _, err := w.daemon(target.externalKey).ResolveApproval(ctx, approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/core internal/api/approvals.go internal/daemonclient/runs.go clients/terminal/chat/runtime/runtime_runs.go clients/telegram && go test ./internal/core/ -run 'TestDeniedApprovalReturnsTheReasonToTheModel|TestRunResumesOnceEveryApprovalIsDecided|TestDeniedCallOutsideARunGetsTheDenialAsItsResult|TestNativeRunParksForApprovalAndResumesAfterGrant' -v`
Expected: the four tests PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/contracts.go internal/core/tool_approvals.go internal/api/approvals.go internal/daemonclient/runs.go \
  clients/terminal/chat/runtime/runtime_runs.go clients/telegram/callbacks.go clients/telegram/run_render.go \
  internal/core/native_run_characterization_test.go internal/core/native_run_test.go internal/core/run_budget_test.go \
  internal/core/context_guard_test.go internal/core/run_crash_recovery_integration_test.go
git commit -m "feat(core): return denied approvals to the model and resume once all are decided

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Bridged subagent approvals — the child reads the denial, the parent keeps waiting; fix the `not found` race

**Files:**
- Modify: `internal/core/tool_approvals.go` (`ResolveApproval`; `resolveSubagentApprovalBridge` → `passDecisionToSubagent`)
- Modify: `internal/core/subagents.go` (`resumeParentForSubagentStatus`, `mirrorPendingSubagentApproval`)
- Modify: `internal/core/subagents_lifecycle.go` (`syncAsyncSubagentTaskAfterRun`, `syncBlockingSubagentTaskAfterRun`)
- Modify: `internal/core/run_recovery.go` (`recoverInterruptedTool`)
- Modify: `internal/core/subagents_persistence.go` (delete `queueSubagentCompletionRecord`)
- Create: `internal/core/subagents_approval_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/core/subagents_approval_test.go`:

```go
package core_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// bridgedChild delegates from a parent to a child whose one mutating call waits
// for approval; Saw is the child's view of that call's result.
type bridgedChild struct {
	app       *core.Core
	db        *store.SQLiteStore
	mutations int
	mu        sync.Mutex
	saw       string
}

func newBridgedChild(t *testing.T, starter func(*core.Core) core.RunStarter) *bridgedChild {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	b := &bridgedChild{app: app, db: db}
	app.WithRunStarter(starter(app))
	mutate, _ := approvalTools(&b.mutations)
	app.WithTools(tools.NewRegistry(append(core.SubagentToolExecutors(app), mutate)...))
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
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: []byte(`{"goal":"change the state","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	return b
}

func (b *bridgedChild) childSaw() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saw
}

// park runs the parent until it waits for the child's approval and returns the
// parent's run, its bridged approval and the child's run.
func (b *bridgedChild) park(t *testing.T) (core.Run, core.Approval, string) {
	t.Helper()
	session, run := saveCrashRecoveryRun(t, b.db, "bridge-parent", core.RunStatusAccepted, false)
	if err := b.app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, b.db, run.ID, core.RunStatusWaitingApproval)
	approvals, err := b.db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 || approvals[0].ToolCallRef != "call-delegate" {
		t.Fatalf("parent approvals = %+v err = %v", approvals, err)
	}
	task, err := b.db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, b.db, task.ChildRunID, core.RunStatusWaitingApproval)
	return run, approvals[0], task.ChildRunID
}

func TestGrantingABridgedApprovalBeforeTheChildResumes(t *testing.T) {
	starter := &recordingRunStarter{}
	b := newBridgedChild(t, func(*core.Core) core.RunStarter { return starter })
	parent, bridge, childRunID := b.park(t)

	if _, err := b.app.ResolveApproval(context.Background(), bridge.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if got := starter.count(childRunID); got != 1 {
		t.Fatalf("child schedules = %d, want 1", got)
	}
	if err := b.app.ExecuteRun(context.Background(), childRunID); err != nil {
		t.Fatal(err)
	}
	if err := b.app.ExecuteRun(context.Background(), parent.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	if b.mutations != 1 || b.childSaw() != "mutated" {
		t.Fatalf("mutations = %d, child saw %q", b.mutations, b.childSaw())
	}
}

func TestDeniedBridgedApprovalLetsTheChildGoOn(t *testing.T) {
	var starter *executingRunStarter
	b := newBridgedChild(t, func(app *core.Core) core.RunStarter {
		starter = &executingRunStarter{app: app}
		return starter
	})
	parent, bridge, childRunID := b.park(t)

	if _, err := b.app.ResolveApproval(context.Background(), bridge.ID, core.ApprovalResolveRequest{Reason: "not in the shared tree"}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	waitForRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	starter.wait(t)

	assertRecoveryRunStatus(t, b.db, childRunID, core.RunStatusCompleted)
	if b.mutations != 0 || b.childSaw() != "User denied: not in the shared tree" {
		t.Fatalf("mutations = %d, child saw %q", b.mutations, b.childSaw())
	}
	task, err := b.db.GetSubagentTaskByParentToolCall(context.Background(), parent.SessionID, parent.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertSubagentTaskStatus(t, task, core.SubagentTaskStatusCompleted)
	for _, message := range sessionMessages(t, b.db, parent.SessionID) {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == "call-delegate" && part.ToolResult.Content != "Child read: User denied: not in the shared tree" {
				t.Fatalf("delegate result = %q", part.ToolResult.Content)
			}
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/core/ -run 'TestGrantingABridgedApprovalBeforeTheChildResumes|TestDeniedBridgedApprovalLetsTheChildGoOn' -v`
Expected: FAIL —
```
subagents_approval_test.go:87: ResolveApproval: not found
subagents_approval_test.go:127: subagent task status = "failed" (Subagent approval denied for mutate_state), want "completed"
```
The first failure is the race from the stage 2a notes, reproduced deterministically: the recording run starter only schedules the child, so the child is still `waiting_approval` with its approval already decided.

- [ ] **Step 3: Implement**

`internal/core/tool_approvals.go`: in `ResolveApproval` the bridged case becomes

```go
	case bridged:
		return approval, c.passDecisionToSubagent(ctx, bridge, decision)
```

and `resolveSubagentApprovalBridge` is replaced by:

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
	if !subagentTaskTerminalStatus(task.Status) {
		if task, err = c.markSubagentTaskRunning(ctx, task); err != nil {
			return err
		}
	}
	if task.Mode == SubagentTaskModeAsync {
		return nil
	}
	return c.resumeParentAfterSubagentTerminal(ctx, task)
}
```

`internal/core/subagents.go`: in `resumeParentForSubagentStatus`, the waiting branch returns the mirror's own answer:

```go
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

and `mirrorPendingSubagentApproval` becomes:

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
		ToolName:   subagentParentToolName(task),
		ToolCallID: task.ParentToolCallID,
	}
	if _, _, created, createErr := c.createPendingApproval(ctx, prepared, ExecuteToolInput{}, tools.Result{Approval: request}, nil); createErr != nil || !created {
		return true, createErr
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

`internal/core/subagents_lifecycle.go`: in `syncAsyncSubagentTaskAfterRun` the waiting case becomes

```go
	case RunStatusWaitingApproval:
		_, err := c.mirrorPendingSubagentApproval(ctx, task)
		return err
```

and in `syncBlockingSubagentTaskAfterRun` the last line of the waiting case becomes

```go
		_, err = c.mirrorPendingSubagentApproval(ctx, task)
		return err
```

`internal/core/run_recovery.go` (`recoverInterruptedTool`), the blocking child that waits for approval:

```go
			if childErr == nil && childRun.Status == RunStatusWaitingApproval {
				mirrored, err := c.mirrorPendingSubagentApproval(ctx, task)
				switch {
				case err != nil:
					return recoveryToolContinue, err
				case mirrored:
					return recoveryToolWaitApproval, nil
				}
				return recoveryToolWaitSubagent, nil
			}
```

`internal/core/subagents_persistence.go`: delete `queueSubagentCompletionRecord` (its only caller was the old bridge denial).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/core && go test -race -count=3 ./internal/core/ -run 'TestGrantingABridgedApprovalBeforeTheChildResumes|TestDeniedBridgedApprovalLetsTheChildGoOn'`
Expected: `ok`.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/tool_approvals.go internal/core/subagents.go internal/core/subagents_lifecycle.go internal/core/run_recovery.go internal/core/subagents_persistence.go internal/core/subagents_approval_test.go
git commit -m "fix(core): let a subagent read a denied bridged approval; wait for its resume

Granting a bridged approval failed with not found when the child was
scheduled but still waiting_approval; the parent now keeps waiting.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Crash recovery leaves a denied call to the resumed run

**Files:**
- Modify: `internal/core/run_recovery.go` (`prepareRunAfterCrash`, `recoveryToolDisposition` constants, `recoverInterruptedTool`)
- Test: `internal/core/run_crash_recovery_integration_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/core/run_crash_recovery_integration_test.go`, before `TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate`:

```go
func TestRecoverDeniedToolReturnsTheDenialToTheModel(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	var saw string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		saw = toolResultContent(request, "tool_mutation")
		return providers.Response{Text: "Kept the old config."}, nil
	})})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(mutation))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "denied_before_restart", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	decided := runRecoveryTestTime()
	if err := sqliteStore.CreateApproval(context.Background(), core.Approval{
		ID: "approval_denied", SessionID: run.SessionID, RunID: run.ID, ToolCallRef: "tool_mutation", ToolName: mutation.spec.ID,
		State: core.ApprovalStateRejected, Reason: "keep the old config", RequestedAt: decided, DecidedAt: &decided,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("recovered run schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, run.SessionID, "tool_mutation", 1)
	if saw != "User denied: keep the old config" || mutation.callCount() != 0 {
		t.Fatalf("model read %q, mutations = %d", saw, mutation.callCount())
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/ -run TestRecoverDeniedToolReturnsTheDenialToTheModel -v`
Expected: FAIL — `recovered run schedules = 0, want 1` (recovery failed the run).

- [ ] **Step 3: Implement**

In `internal/core/run_recovery.go`, the approval check at the top of `recoverInterruptedTool` becomes:

```go
	if latest, ok := latestApprovalForToolCall(approvals, run.ID, call.ID); ok {
		_, bridged := decodeSubagentApprovalBridge(latest)
		switch {
		case latest.State == ApprovalStatePending:
			return recoveryToolWaitApproval, nil
		case latest.State == ApprovalStateRejected && !bridged:
			// The resumed run reads the denial as the call's result.
			return recoveryToolContinue, nil
		}
	}
```

Nothing returns `recoveryToolRunFailed` any more: delete the constant

```go
const (
	recoveryToolContinue recoveryToolDisposition = iota
	recoveryToolWaitApproval
	recoveryToolWaitSubagent
)
```

and its case in `prepareRunAfterCrash`, whose switch keeps only:

```go
		switch disposition {
		case recoveryToolWaitApproval:
			if err := c.setRunStatus(ctx, run, RunStatusWaitingApproval, ""); err != nil {
				return false, err
			}
			return false, nil
		case recoveryToolWaitSubagent:
			if err := c.saveRunCheckpoint(ctx, run.ID, RunCheckpointPhaseWaitingSubagent, interrupted.Call.ID, interrupted.Call.Name); err != nil {
				return false, err
			}
			return false, nil
		}
```

(`failRecoveredRun` stays: it still ends runs past the recovery attempt limit.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `gofmt -w internal/core/run_recovery.go internal/core/run_crash_recovery_integration_test.go && go test ./internal/core/ -run 'TestRecover' -v`
Expected: every `TestRecover…` test PASSES.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/run_recovery.go internal/core/run_crash_recovery_integration_test.go
git commit -m "fix(core): resume a run whose approval was denied before a restart

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Approval barrier and deferred calls

A mutating call that waits for approval is a barrier: the later calls of its batch are journaled `deferred` and run, in call order, only once none of the run's approvals is pending. Crash recovery treats deferred calls as not started.

**Files:**
- Modify: `internal/transcript/message.go` (`ToolCallPart`)
- Modify: `internal/agent/ports.go` (`Decision`)
- Modify: `internal/agent/tools.go` (`executeBatch`, `resumeDecided`, `runCall`, `rejectCall`, `finishCall`; add `callState`, `runDeferred`, `deferredCalls`, `startCall`, `deferCall`, `writeCall`, `deferredCall`)
- Modify: `internal/agent/agenttest/agenttest.go` (`Tools`)
- Modify: `internal/core/agent_tools.go` (`coreTools.Authorize`)
- Modify: `internal/core/run_recovery.go` (`incompleteToolCallsForRun`)
- Test: `internal/agent/engine_test.go`, `internal/core/native_run_characterization_test.go`, `internal/core/run_crash_recovery_integration_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/agent/engine_test.go`, replace `TestApprovalRequestParksAfterTheRestOfTheBatch` with:

```go
func lookupTool(call tools.Call) tools.Result {
	if !call.Approved {
		return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "lookup", ToolCallID: call.ToolCallID, Action: "lookup"}}
	}
	return tools.Result{Content: "looked up"}
}

func executedIDs(f *agenttest.Fixture) string {
	ids := make([]string, 0, len(f.Tools.Calls))
	for _, call := range f.Tools.Calls {
		ids = append(ids, call.ToolCallID)
	}
	return strings.Join(ids, ",")
}

func TestApprovalThatIsNotABarrierLetsTheBatchRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["lookup"] = lookupTool
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(calls(call("l1", "lookup"), call("r1", "read")))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusWaitingApproval {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(f.Approvals.Requests) != 1 || f.Approvals.Requests[0].ToolCallID != "l1" || f.Approvals.Requests[0].Request.Action != "lookup" {
		t.Fatalf("approval requests = %+v", f.Approvals.Requests)
	}
	if _, ok := f.Journal.Result("r1"); !ok {
		t.Fatal("the read after a non-barrier approval did not run")
	}
	if _, ok := f.Journal.Result("l1"); ok {
		t.Fatal("unapproved lookup has a result")
	}
	if message, _ := f.Journal.Message("l1"); message.Parts[0].ToolCall.Finished {
		t.Fatal("pending call marked finished")
	}
}

func TestMutatingApprovalDefersTheRestOfTheBatch(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Tools.Funcs["read"] = readTool
	f.Tools.Mutating = map[string]bool{"write": true}
	model := agenttest.NewScriptedModel(calls(call("r1", "read"), call("w1", "write"), call("r2", "read"), call("w2", "write")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusWaitingApproval || len(f.Approvals.Requests) != 1 || f.Approvals.Requests[0].ToolCallID != "w1" {
		t.Fatalf("outcome = %+v requests = %+v", outcome, f.Approvals.Requests)
	}
	if got := executedIDs(f); got != "r1,w1" {
		t.Fatalf("executed = %s, want r1,w1", got)
	}
	for _, id := range []string{"r2", "w2"} {
		if message, ok := f.Journal.Message(id); !ok || !message.Parts[0].ToolCall.Deferred {
			t.Fatalf("%s not journaled deferred: %+v", id, message)
		}
		if _, ok := f.Journal.Result(id); ok {
			t.Fatalf("%s has a result before the decision", id)
		}
	}
	for _, event := range f.Sink.Events {
		if event.Kind == agent.EventToolRequested && (event.ToolCallID == "r2" || event.ToolCallID == "w2") {
			t.Fatalf("deferred call %s announced as requested", event.ToolCallID)
		}
	}

	f.Approvals.Open = false
	f.Inbox.Decided = []agent.Input{{Kind: agent.InputDecided, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`), Denied: true, Reason: "not yet"}}
	outcome = run(t, f, model)

	if outcome.Status != agent.StatusWaitingApproval || len(f.Approvals.Requests) != 2 || f.Approvals.Requests[1].ToolCallID != "w2" {
		t.Fatalf("after the denial: outcome = %+v requests = %+v", outcome, f.Approvals.Requests)
	}
	if got := executedIDs(f); got != "r1,w1,r2,w2" {
		t.Fatalf("executed = %s, want r1,w1,r2,w2", got)
	}
	if message, _ := f.Journal.Message("w2"); message.Parts[0].ToolCall.Deferred {
		t.Fatal("the new barrier is still marked deferred")
	}
	if len(model.Requests()) != 1 {
		t.Fatalf("model called %d times while a barrier was open", len(model.Requests()))
	}

	f.Approvals.Open = false
	f.Inbox.Decided = append(f.Inbox.Decided, agent.Input{Kind: agent.InputDecided, ToolCallID: "w2", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)})
	outcome = run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 2 {
		t.Fatalf("after the grant: outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	request := model.Requests()[1]
	for id, want := range map[string]string{"r1": "file body", "w1": "User denied: not yet", "r2": "file body", "w2": "written"} {
		if got := toolContent(request, id); got != want {
			t.Fatalf("result of %s = %q, want %q", id, got, want)
		}
	}
	if split := agenttest.SplitToolPair(request); split != "" {
		t.Fatal(split)
	}
}

```

In `internal/core/native_run_characterization_test.go`, replace `TestNativeRunParksForApprovalAndResumesAfterGrant` (its read-only call used to run before the grant) with:

```go
func TestNativeRunHoldsLaterCallsBehindAnApprovalBarrier(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, inspect := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, inspect))
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	calls := 0
	var resumedWithBothResults bool
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)},
				{ID: "call-inspect", Name: "inspect_state", Arguments: []byte(`{}`)},
			}}, nil
		}
		resumedWithBothResults = toolResultContent(request, "call-mutate") == "mutated" && toolResultContent(request, "call-inspect") == "recovered tool result"
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "approval", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	assertToolResultCount(t, db, session.ID, "call-mutate", 0)
	assertToolResultCount(t, db, session.ID, "call-inspect", 0)
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || approvals[0].ToolCallRef != "call-mutate" || approvals[0].Action != "write_state" {
		t.Fatalf("pending approvals = %#v", approvals)
	}
	if calls != 1 || mutations != 0 || inspect.callCount() != 0 {
		t.Fatalf("before grant: model=%d mutations=%d inspect=%d", calls, mutations, inspect.callCount())
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, db, session.ID, "call-inspect", 1)
	if calls != 2 || mutations != 1 || inspect.callCount() != 1 || !resumedWithBothResults {
		t.Fatalf("after grant: model=%d mutations=%d inspect=%d both results=%v", calls, mutations, inspect.callCount(), resumedWithBothResults)
	}
}
```

In `internal/core/run_crash_recovery_integration_test.go`, add `"github.com/Suren878/matrixclaw/internal/agent"` to the imports and, before `TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate`:

```go
func TestRecoveryLeavesDeferredCallsToTheResumedRun(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	inspect := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results = []string{toolResultContent(request, "tool_mutation"), toolResultContent(request, "tool_inspect")}
		return providers.Response{Text: "Done."}, nil
	})})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(mutation, inspect))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "deferred", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	deferred := agent.ToolCallMessage("tool_inspect", run.SessionID, run.ID, inspect.spec.ID, []byte(`{}`), false, run.StartedAt.Add(2*time.Second))
	deferred.Parts[0].ToolCall.Deferred = true
	saveRunRecoveryTestMessage(t, sqliteStore, deferred)
	decided := runRecoveryTestTime()
	if err := sqliteStore.CreateApproval(context.Background(), core.Approval{
		ID: "approval_mutation", SessionID: run.SessionID, RunID: run.ID, ToolCallRef: "tool_mutation", ToolName: mutation.spec.ID,
		State: core.ApprovalStateRejected, RequestedAt: decided, DecidedAt: &decided,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	if inspect.callCount() != 0 {
		t.Fatal("recovery replayed a deferred call")
	}
	if approvals, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending); err != nil || len(approvals) != 0 {
		t.Fatalf("recovery approvals = %+v err = %v", approvals, err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	if strings.Join(results, "|") != "User denied.|recovered tool result" || inspect.callCount() != 1 || mutation.callCount() != 0 {
		t.Fatalf("model read %q, inspect = %d, mutations = %d", results, inspect.callCount(), mutation.callCount())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go vet ./internal/agent/ ./internal/core/`
Expected: FAIL to compile — `unknown field Mutating in struct literal` / `ToolCall.Deferred undefined`.

- [ ] **Step 3: Implement**

`internal/transcript/message.go`:

```go
type ToolCallPart struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Finished bool   `json:"finished,omitempty"`
	// Deferred marks a call held back behind an approval barrier; it has not started.
	Deferred bool `json:"deferred,omitempty"`
}
```

`internal/agent/ports.go`:

```go
// Decision says whether a requested tool call may run; Reason goes back to the model.
// A Barrier call that waits for approval holds back the calls after it in its batch.
type Decision struct {
	Allowed bool
	Reason  string
	Barrier bool
}
```

`internal/agent/tools.go`: replace everything from the doc comment of `executeBatch` down to the end of `finishCall` (that is `executeBatch`, `resumeDecided`, `runCall`, `toolCall`, `rejectCall`, `finishCall`) with the block below; `callRequest` above it and `appendResult` and everything after it stay as they are. `runCall` no longer assigns missing call IDs: `executeBatch` does, and decided or deferred calls always carry one.

```go
// callState is where a call stands after runCall.
type callState int

const (
	callDone callState = iota
	// callPending waits for approval; later calls of its batch still run.
	callPending
	// callBarrier waits for approval and holds back the later calls of its batch.
	callBarrier
)

// executeBatch runs the response's tool calls in order. A call waiting for approval
// parks while the rest of the batch runs, unless it is a barrier: every later call
// is then journaled deferred and runs once the run's approvals are decided.
func (r *run) executeBatch(ctx context.Context, response providers.Response) (bool, error) {
	seen := make(map[string]providers.ToolCall)
	waiting, barrier := false, false
	for _, toolCall := range response.ToolCalls {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		id := strings.TrimSpace(toolCall.ID)
		if id != "" {
			if prior, ok := seen[id]; ok {
				if !sameRequestedTool(prior.Name, prior.Arguments, toolCall) {
					return false, fmt.Errorf("tool call ID %q reused with different arguments", id)
				}
				continue
			}
			if prior, ok := r.history.callMessage(id); ok {
				if prior.RunID != r.task.RunID {
					return false, fmt.Errorf("tool call ID %q belongs to another run", id)
				}
				for _, part := range prior.Parts {
					if part.ToolCall != nil && part.ToolCall.ID == id && !sameRequestedTool(part.ToolCall.Name, []byte(part.ToolCall.Input), toolCall) {
						return false, fmt.Errorf("tool call ID %q reused with different arguments", id)
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
			return false, errors.New("provider returned tool call without a name")
		}
		if id == "" {
			id = r.NewID("tool")
		}
		request := callRequest{id: id, name: name, args: toolCall.Arguments, workingDir: r.task.WorkingDir}
		if barrier {
			if err := r.deferCall(ctx, request); err != nil {
				return false, err
			}
			continue
		}
		state, err := r.runCall(ctx, request)
		if err != nil {
			return false, err
		}
		waiting = waiting || state != callDone
		barrier = state == callBarrier
	}
	return waiting, nil
}

// resumeDecided answers the run's decided approvals that have no result yet: a
// granted call runs, a denied one gets the denial as its result. Once none is
// pending, the calls deferred behind a barrier run. It reports whether the run
// still waits for approval.
func (r *run) resumeDecided(ctx context.Context) (bool, error) {
	decided, err := r.Inbox.Peek(ctx, r.task.RunID, InputDecided)
	if err != nil {
		return false, err
	}
	for _, input := range decided {
		if r.history.hasResult(input.ToolCallID) {
			continue
		}
		request := callRequest{id: input.ToolCallID, name: input.ToolName, args: input.Args, workingDir: input.WorkingDir, approved: true}
		if input.Denied {
			err = r.finishCall(ctx, request, r.toolCall(request), DenialResult(input.Reason))
		} else {
			_, err = r.runCall(ctx, request)
		}
		if err != nil {
			return false, err
		}
	}
	pending, err := r.Approvals.Pending(ctx, r.task.RunID)
	if err != nil || pending {
		return pending, err
	}
	return r.runDeferred(ctx)
}

// runDeferred runs the calls held back by a barrier in call order, until one of
// them is a barrier again.
func (r *run) runDeferred(ctx context.Context) (bool, error) {
	waiting := false
	for _, request := range r.deferredCalls() {
		state, err := r.runCall(ctx, request)
		if err != nil {
			return false, err
		}
		if state == callBarrier {
			return true, nil
		}
		waiting = waiting || state == callPending
	}
	return waiting, nil
}

// deferredCalls lists the run's calls still held back by a barrier, in call order.
func (r *run) deferredCalls() []callRequest {
	var out []callRequest
	for _, message := range r.history.all() {
		if message.RunID != r.task.RunID {
			continue
		}
		for _, part := range message.Parts {
			if call := part.ToolCall; call != nil && call.Deferred && !r.history.hasResult(call.ID) {
				out = append(out, callRequest{id: call.ID, name: call.Name, args: json.RawMessage(call.Input), workingDir: r.task.WorkingDir})
			}
		}
	}
	return out
}

// runCall authorizes, journals and executes one call and reports where it stands.
func (r *run) runCall(ctx context.Context, req callRequest) (callState, error) {
	call := r.toolCall(req)
	decision, err := r.Tools.Authorize(ctx, req.name, call)
	if err != nil {
		return callDone, err
	}
	if !decision.Allowed {
		if req.approved {
			return callDone, errors.New(decision.Reason)
		}
		return callDone, r.rejectCall(ctx, req, decision.Reason)
	}
	if err := r.startCall(ctx, req); err != nil {
		return callDone, err
	}
	if err := r.checkpoint(ctx, PhaseTool, req.id, req.name); err != nil {
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

// startCall journals a call about to run and announces it; a call held back by
// a barrier loses its deferred mark.
func (r *run) startCall(ctx context.Context, req callRequest) error {
	if existing, ok := r.history.message(req.id); ok && !deferredCall(existing) {
		return nil
	}
	if err := r.writeCall(ctx, req, false); err != nil {
		return err
	}
	r.Sink.Emit(Event{Kind: EventToolRequested, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name})
	return nil
}

// deferCall journals a call held back by an approval barrier.
func (r *run) deferCall(ctx context.Context, req callRequest) error {
	if _, ok := r.history.message(req.id); ok {
		return nil
	}
	message := r.callMessage(req, false)
	message.Parts[0].ToolCall.Deferred = true
	return r.history.append(ctx, message)
}

// writeCall journals the call's message, or updates the one already journaled.
func (r *run) writeCall(ctx context.Context, req callRequest, finished bool) error {
	message := r.callMessage(req, finished)
	existing, ok := r.history.message(req.id)
	if !ok {
		return r.history.append(ctx, message)
	}
	message.CreatedAt = existing.CreatedAt
	return r.history.finish(ctx, message)
}

func deferredCall(message transcript.Message) bool {
	for _, part := range message.Parts {
		if part.ToolCall != nil && part.ToolCall.Deferred {
			return true
		}
	}
	return false
}

func (r *run) toolCall(req callRequest) tools.Call {
	return tools.Call{
		SessionID:   r.task.SessionID,
		RunID:       r.task.RunID,
		ToolCallID:  req.id,
		Client:      r.task.Client,
		ExternalKey: r.task.ExternalKey,
		WorkingDir:  req.workingDir,
		Approved:    req.approved,
		Args:        req.args,
	}
}

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
	return r.checkpoint(ctx, PhaseModel, "", "")
}

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
	if err := r.Tools.Finish(ctx, req.name, call, result, message); err != nil {
		return err
	}
	return r.checkpoint(ctx, PhaseModel, "", "")
}
```

`internal/agent/agenttest/agenttest.go`, the tools fake:

```go
// ToolFunc executes one fake tool call.
type ToolFunc func(call tools.Call) tools.Result

// Tools authorizes registered names only, making the Mutating ones approval
// barriers, and records executed and finished calls. OnExecute and OnFinish run
// first and fail the call with their error.
type Tools struct {
	Funcs     map[string]ToolFunc
	Mutating  map[string]bool
	Calls     []tools.Call
	Finished  []string
	OnExecute func(name string, call tools.Call) error
	OnFinish  func(name string, call tools.Call) error
	// SpecReads counts tool listings.
	SpecReads int
}
```

and the last line of its `Authorize`:

```go
	return agent.Decision{Allowed: true, Barrier: t.Mutating[name]}, nil
```

`internal/core/agent_tools.go`, `coreTools.Authorize` — mutating tools are barriers:

```go
func (t coreTools) Authorize(ctx context.Context, name string, call tools.Call) (agent.Decision, error) {
	// A call ID owned by another session fails the run with a clear error instead of
	// a primary-key conflict on the first journal write.
	if _, err := t.c.isNewToolCallMessage(ctx, call.SessionID, call.ToolCallID); err != nil {
		return agent.Decision{}, err
	}
	_, spec, err := t.c.checkToolCall(ctx, call.SessionID, name)
	if errors.Is(err, ErrInvalidInput) {
		return agent.Decision{Reason: err.Error()}, nil
	}
	if err != nil {
		return agent.Decision{}, err
	}
	return agent.Decision{Allowed: true, Barrier: spec.Mutates()}, nil
}
```

`internal/core/run_recovery.go`, in `incompleteToolCallsForRun` a deferred call is not interrupted — it never started:

```go
			if part.ToolCall == nil || part.ToolCall.Deferred {
				continue
			}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/transcript/message.go internal/agent internal/core && go test ./internal/agent/... ./internal/core/ -run 'Barrier|Deferred|TestMutatingApprovalDefersTheRestOfTheBatch|TestApprovalThatIsNotABarrierLetsTheBatchRun|TestNativeRunHoldsLaterCallsBehindAnApprovalBarrier|TestRecoveryLeavesDeferredCallsToTheResumedRun' -v`
Expected: all listed tests PASS.

- [ ] **Step 5: Full suite (with the race detector for the loop) and commit**

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/ ./internal/core/
git status --short
git add internal/transcript/message.go internal/agent/ports.go internal/agent/tools.go internal/agent/agenttest/agenttest.go internal/agent/engine_test.go \
  internal/core/agent_tools.go internal/core/run_recovery.go internal/core/native_run_characterization_test.go internal/core/run_crash_recovery_integration_test.go
git commit -m "feat(agent): hold later calls of a batch behind a mutating approval

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `/approval deny <id> [reason]` in the shared controlplane

A hidden command both clients use to deny with a reason: the TUI and Telegram open `DenyWithReasonPrompt(id)`, whose submit prefix is `/approval deny <id> `, and the typed text becomes the reason.

**Files:**
- Modify: `internal/commandcatalog/catalog.go` (`CommandApproval`, catalog entry)
- Modify: `internal/controlplane/catalog.go` (`CommandApproval`)
- Modify: `internal/controlplane/dispatcher.go` (`ApprovalRuntime`, `Dispatcher.approvals`, `New`, `Handle`)
- Modify: `internal/clientruntime/controlplane_runtime.go` (add `ResolveApproval`)
- Create: `internal/controlplane/approval.go`, `internal/controlplane/approval_test.go`

- [ ] **Step 1: Write the failing test**

`internal/controlplane/approval_test.go`:

```go
package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type approvalRuntime struct {
	ids      []string
	requests []core.ApprovalResolveRequest
}

func (r *approvalRuntime) ResolveApproval(_ context.Context, approvalID string, request core.ApprovalResolveRequest) (core.Approval, error) {
	r.ids = append(r.ids, approvalID)
	r.requests = append(r.requests, request)
	return core.Approval{ID: approvalID, ToolName: "bash", State: core.ApprovalStateRejected, Reason: request.Reason}, nil
}

func TestApprovalDenyCarriesTheReason(t *testing.T) {
	runtime := &approvalRuntime{}

	result, err := New(runtime, "").Handle(context.Background(), "key", DenyWithReasonPrompt("approval_1").SubmitCommandPrefix+"use  the staging db")

	if err != nil || len(runtime.ids) != 1 || runtime.ids[0] != "approval_1" {
		t.Fatalf("result = %+v ids = %v err = %v", result, runtime.ids, err)
	}
	if runtime.requests[0] != (core.ApprovalResolveRequest{Reason: "use  the staging db"}) {
		t.Fatalf("request = %+v", runtime.requests[0])
	}
	if result.Text != "❌ Denied bash: use  the staging db" || !result.ReloadSnapshot {
		t.Fatalf("result = %+v", result)
	}
}

func TestApprovalRejectsOtherAnswers(t *testing.T) {
	for _, command := range []string{"/approval", "/approval deny", "/approval allow approval_1"} {
		runtime := &approvalRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || result.Text != approvalUsage || len(runtime.ids) != 0 {
			t.Errorf("%s: result = %+v ids = %v err = %v", command, result, runtime.ids, err)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/controlplane/ -run TestApproval`
Expected: FAIL to compile — `undefined: DenyWithReasonPrompt`, `undefined: approvalUsage`.

- [ ] **Step 3: Implement**

`internal/commandcatalog/catalog.go`: add the constant after `CommandHelp`

```go
	CommandApproval    CommandID = "approval"
```

and, as the last catalog entry (hidden: no menu, no Telegram command list),

```go
		{ID: CommandApproval, Command: "/approval", Description: "Answer an approval", Menu: false, Public: false},
```

`internal/controlplane/catalog.go`: after `CommandHelp`

```go
	CommandApproval    = commandcatalog.CommandApproval
```

`internal/controlplane/dispatcher.go`: after `PermissionRuntime` add

```go
type ApprovalRuntime interface {
	ResolveApproval(ctx context.Context, approvalID string, request core.ApprovalResolveRequest) (core.Approval, error)
}
```

a field `approvals      ApprovalRuntime` after `permissions` in `Dispatcher`, the assignment `d.approvals, _ = runtime.(ApprovalRuntime)` after the `PermissionRuntime` one in `New`, and in `Handle` after the `CommandPermissions` case:

```go
	case CommandApproval:
		return d.handleApproval(ctx, args)
```

`internal/controlplane/approval.go`:

```go
package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

const approvalUsage = "Usage: /approval deny <approval id> [reason]"

// DenyWithReasonPrompt asks for the reason a client sends with a denial.
func DenyWithReasonPrompt(approvalID string) PromptData {
	return PromptData{
		Title:               "Why deny it?",
		Placeholder:         "Reason the agent reads (optional)",
		SubmitCommandPrefix: controlplaneCommand("approval", "deny", approvalID) + " ",
	}
}

// handleApproval answers "/approval deny <id> [reason]": the call is denied and
// the model reads the reason as its result.
func (d *Dispatcher) handleApproval(ctx context.Context, args string) (Result, error) {
	if d.approvals == nil {
		return unsupportedRuntime("approval"), nil
	}
	action, rest := cutWord(args)
	approvalID, reason := cutWord(rest)
	if !strings.EqualFold(action, "deny") || approvalID == "" {
		return Result{Handled: true, Text: approvalUsage}, nil
	}
	approval, err := d.approvals.ResolveApproval(ctx, approvalID, core.ApprovalResolveRequest{Reason: reason})
	if err != nil {
		return Result{}, err
	}
	text := "❌ Denied " + firstNonEmptyTrimmed(approval.ToolName, "the call")
	if approval.Reason != "" {
		text += ": " + approval.Reason
	}
	return Result{Handled: true, Text: text, ReloadSnapshot: true}, nil
}

// cutWord splits text into its first word and the trimmed rest.
func cutWord(text string) (string, string) {
	word, rest, _ := strings.Cut(strings.TrimSpace(text), " ")
	return word, strings.TrimSpace(rest)
}
```

`internal/clientruntime/controlplane_runtime.go`, after `UpdateSessionPermissionMode`:

```go
func (r ControlplaneRuntime) ResolveApproval(ctx context.Context, approvalID string, request core.ApprovalResolveRequest) (core.Approval, error) {
	client, err := r.client("")
	if err != nil {
		return core.Approval{}, err
	}
	return client.ResolveApproval(ctx, approvalID, request)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/commandcatalog internal/controlplane internal/clientruntime && go test ./internal/controlplane/ ./internal/commandcatalog/ ./internal/clientruntime/`
Expected: `ok`.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/commandcatalog/catalog.go internal/controlplane/catalog.go internal/controlplane/dispatcher.go internal/controlplane/approval.go internal/controlplane/approval_test.go internal/clientruntime/controlplane_runtime.go
git commit -m "feat(controlplane): deny an approval with a reason

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: TUI — "Deny with reason"

The permission dialog gets a "Deny with reason" button (key `r`). Choosing it opens the shared reason prompt; submitting runs `/approval deny <id> <reason>`; closing the prompt with Esc brings the permission dialog back. While the reason is typed no other permission dialog opens over it.

**Files:**
- Modify: `clients/terminal/ui/surface/dialog/permissions.go` (`PermissionAction` constants)
- Modify: `clients/terminal/ui/surface/dialog/permissions_keys.go` (`permissionsKeyMap`, `defaultPermissionsKeyMap`)
- Modify: `clients/terminal/ui/surface/dialog/permissions_events.go` (`HandleMsg`, `permissionOptions`)
- Modify: `clients/terminal/ui/surface/dialog/permissions_render.go` (`renderButtons`)
- Modify: `clients/terminal/chat/runtime/app.go` (`appModel`)
- Modify: `clients/terminal/chat/runtime/app_dialog_actions.go` (`handlePermissionResponse`)
- Modify: `clients/terminal/chat/runtime/app_update.go` (`Update`, `ActionClose` case)
- Modify: `clients/terminal/chat/runtime/app_input.go` (`handleDialogInput`)
- Modify: `clients/terminal/chat/runtime/app_approvals.go` (`syncPermissionDialogCmd`, `resolveApprovalCmd`)
- Modify: `clients/terminal/chat/runtime/runtime_runs.go` (delete `resolveApproval`)
- Create: `clients/terminal/ui/surface/dialog/permissions_test.go`

- [ ] **Step 1: Write the failing test**

`clients/terminal/ui/surface/dialog/permissions_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./clients/terminal/ui/surface/dialog/`
Expected: FAIL to compile — `undefined: PermissionDenyWithReason`.

- [ ] **Step 3: Implement the dialog**

`permissions.go`:

```go
const (
	PermissionAllow          PermissionAction = "allow"
	PermissionAllowSession   PermissionAction = "allow_session"
	PermissionDeny           PermissionAction = "deny"
	PermissionDenyWithReason PermissionAction = "deny_with_reason"
)
```

`permissions_keys.go`: add `DenyWithReason   key.Binding` after `Deny` in `permissionsKeyMap`, and after the `Deny` binding in `defaultPermissionsKeyMap`:

```go
		DenyWithReason: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("r", "deny with reason"),
		),
```

`permissions_events.go`: in `HandleMsg`, after the `Deny` case:

```go
		case key.Matches(msg, p.keyMap.DenyWithReason):
			return p.respond(PermissionDenyWithReason)
```

and `permissionOptions` ends with both deny buttons:

```go
func (p *Permissions) permissionOptions() []permissionOption {
	options := []permissionOption{
		{label: "Allow", action: PermissionAllow},
	}
	if p.canAllowSession() {
		options = append(options, permissionOption{label: "Allow Session", action: PermissionAllowSession})
	}
	options = append(options,
		permissionOption{label: "Deny", action: PermissionDeny},
		permissionOption{label: "Deny with reason", action: PermissionDenyWithReason},
	)
	return options
}
```

`permissions_render.go` (`renderButtons`): both deny buttons are drawn as danger:

```go
			Danger:  option.action == PermissionDeny || option.action == PermissionDenyWithReason,
```

- [ ] **Step 4: Wire the prompt in the app**

`app.go`, in `appModel` right after `suppressedApprovals`:

```go
	// denyingApproval is the approval whose denial reason is being typed.
	denyingApproval string
```

`app_dialog_actions.go`: add `"github.com/Suren878/matrixclaw/internal/controlplane"` to the imports; `handlePermissionResponse` opens the prompt instead of resolving:

```go
func (m *appModel) handlePermissionResponse(msg surfacedialog.ActionPermissionResponse) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.PermissionsID)
	m.suppressedApprovals[msg.Permission.ID] = struct{}{}
	if msg.Action == surfacedialog.PermissionDenyWithReason {
		m.denyingApproval = msg.Permission.ID
		m.dialog.OpenDialog(surfacedialog.NewPromptCommand(m.com, controlplane.DenyWithReasonPrompt(msg.Permission.ID)))
		return nil
	}
	if msg.Action == surfacedialog.PermissionAllowSession && surfacepermission.CanAllowSessionApproval(msg.Permission) {
		sessionID := strings.TrimSpace(msg.Permission.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(m.session)
		}
		if sessionID != "" {
			m.autoEditSessions[sessionID] = struct{}{}
		}
	}
	return tea.Batch(
		m.resolveApprovalCmd(
			msg.Permission,
			msg.Action != surfacedialog.PermissionDeny,
		),
		m.syncPermissionDialogCmd(),
	)
}
```

`app_update.go`, the `surfacedialog.ActionClose` case:

```go
	case surfacedialog.ActionClose:
		m.invalidateControlplaneResults()
		top := m.dialog.DialogLast()
		if top != nil && top.ID() == surfacedialog.CommandsID {
			m.commandsDialogRoot = false
		}
		m.dialog.CloseFrontDialog()
		if top != nil && top.ID() == surfacedialog.PromptCommandID && m.denyingApproval != "" {
			// The reason prompt was closed without denying: ask again.
			delete(m.suppressedApprovals, m.denyingApproval)
			m.denyingApproval = ""
			return m, m.syncPermissionDialogCmd()
		}
		return m, nil
```

`app_input.go` (`handleDialogInput`): a submitted prompt ends the reason entry:

```go
	if command, ok := action.(surfacedialog.ActionRunControlplaneCommand); ok {
		if sourceID != "" && sourceID != surfacedialog.CommandsID {
			m.dialog.CloseDialog(sourceID)
		}
		if sourceID == surfacedialog.PromptCommandID {
			m.denyingApproval = ""
		}
		return m, m.handleRunControlplaneCommand(command, fromCommands)
	}
```

`app_approvals.go`: `syncPermissionDialogCmd` opens nothing while a reason is typed —

```go
	if m.read == nil || m.dialog == nil || m.denyingApproval != "" {
		return nil
	}
```

— and `resolveApprovalCmd` uses the shared runtime method from Task 7:

```go
		approval, err := m.rt.ResolveApproval(m.ctx, permission.ID, core.ApprovalResolveRequest{Approved: approved})
```

`runtime_runs.go`: delete `(*Runtime).resolveApproval`; `Runtime` embeds `clientruntime.ControlplaneRuntime`, whose `ResolveApproval` talks to the same daemon client.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w clients/terminal && go vet ./clients/... && go test ./clients/terminal/...`
Expected: `ok` for every package.

- [ ] **Step 6: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add clients/terminal/ui/surface/dialog/permissions.go clients/terminal/ui/surface/dialog/permissions_keys.go clients/terminal/ui/surface/dialog/permissions_events.go \
  clients/terminal/ui/surface/dialog/permissions_render.go clients/terminal/ui/surface/dialog/permissions_test.go \
  clients/terminal/chat/runtime/app.go clients/terminal/chat/runtime/app_dialog_actions.go clients/terminal/chat/runtime/app_update.go \
  clients/terminal/chat/runtime/app_input.go clients/terminal/chat/runtime/app_approvals.go clients/terminal/chat/runtime/runtime_runs.go
git commit -m "feat(tui): deny an approval with a reason

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Telegram — "Deny with reason"

A "✍️ Deny with reason" button next to "❌ Deny" turns the chat's next message into the reason (through the shared prompt and `/approval deny`). Pressing another button of the same approval first drops the pending reason prompt.

**Files:**
- Modify: `clients/telegram/constants.go` (`cbApprovalReason`)
- Modify: `clients/telegram/keyboards.go` (`approvalKeyboard`)
- Modify: `clients/telegram/callbacks.go` (`handleCallbackQuery`, `resolveApprovalCallback`; add `askDenialReason`)
- Create: `clients/telegram/approvals_test.go`

- [ ] **Step 1: Write the failing test**

`clients/telegram/approvals_test.go`:

```go
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

type approvalBotAPI struct {
	recordingBotAPI
	sent []SendMessageRequest
}

func (a *approvalBotAPI) SendMessage(_ context.Context, request SendMessageRequest) (SentMessage, error) {
	a.sent = append(a.sent, request)
	return SentMessage{MessageID: int64(len(a.sent))}, nil
}

func TestDenyWithReasonSendsTheNextMessageAsTheReason(t *testing.T) {
	var path string
	var resolved core.ApprovalResolveRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/v1/approvals/") {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&resolved); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(core.ApprovalResponse{Approval: core.Approval{ID: "approval_1", ToolName: "bash", State: core.ApprovalStateRejected, Reason: resolved.Reason}})
	}))
	defer server.Close()
	api := &approvalBotAPI{}
	worker := &Worker{
		api:     api,
		config:  Config{BaseURL: server.URL, ClientName: "telegram-test", DaemonHTTPClient: server.Client()},
		prompts: map[string]controlplane.PromptData{},
	}
	chat := Chat{ID: 42, Type: "private"}

	if err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: &Message{MessageID: 7, Chat: chat}, Data: cbApprovalReason + "approval_1"}); err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("resolved before the reason arrived: %s", path)
	}
	if err := worker.handleTextMessage(context.Background(), &Message{MessageID: 8, Chat: chat, From: &User{ID: 42}, Text: "not on production"}); err != nil {
		t.Fatal(err)
	}

	if path != "/v1/approvals/approval_1/resolve" || resolved != (core.ApprovalResolveRequest{Reason: "not on production"}) {
		t.Fatalf("resolved %s with %+v", path, resolved)
	}
	if last := api.sent[len(api.sent)-1].Text; !strings.Contains(last, "Denied bash: not on production") {
		t.Fatalf("reply = %q", last)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./clients/telegram/ -run TestDenyWithReasonSendsTheNextMessageAsTheReason`
Expected: FAIL to compile — `undefined: cbApprovalReason`.

- [ ] **Step 3: Implement**

`constants.go`, after `cbApprovalDeny`:

```go
	cbApprovalReason  = "ar:"
```

`keyboards.go` (`approvalKeyboard`), the second row:

```go
			{
				{Text: "❌ Deny", CallbackData: cbApprovalDeny + approvalID},
				{Text: "✍️ Deny with reason", CallbackData: cbApprovalReason + approvalID},
			},
```

`callbacks.go`: in `handleCallbackQuery`, after the `cbApprovalDeny` case:

```go
	case strings.HasPrefix(cq.Data, cbApprovalReason):
		return w.askDenialReason(telegramCtx, target, strings.TrimPrefix(cq.Data, cbApprovalReason))
```

and replace `resolveApprovalCallback` with `askDenialReason` followed by the new `resolveApprovalCallback`:

```go
// askDenialReason makes the chat's next message the reason for denying the approval.
func (w *Worker) askDenialReason(ctx context.Context, target chatTarget, approvalID string) error {
	w.setPrompt(target.externalKey, controlplane.DenyWithReasonPrompt(approvalID))
	return w.sendText(ctx, target, "Send the reason for denying, or /cancel.")
}

func (w *Worker) resolveApprovalCallback(ctx context.Context, target chatTarget, cq *CallbackQuery, approvalID string, approved bool, allowSession bool) error {
	if prompt, ok := w.prompt(target.externalKey); ok && prompt.SubmitCommandPrefix == controlplane.DenyWithReasonPrompt(approvalID).SubmitCommandPrefix {
		w.clearPrompt(target.externalKey)
	}
	approval, err := w.daemon(target.externalKey).ResolveApproval(ctx, approvalID, core.ApprovalResolveRequest{Approved: approved})
	if err != nil {
		return w.editOrSend(ctx, target, cq.Message.MessageID, fmt.Sprintf("Resolve approval failed: %v", err), nil)
	}
	status := "Denied"
	if approved {
		status = "Approved"
	}
	if approved && allowSession && canAllowSessionApproval(approval) {
		w.rememberAutoEditSession(target, approval.SessionID)
		status = "Approved for session"
	}
	if err := w.editOrSend(ctx, target, cq.Message.MessageID, status+"\n\n"+renderApprovalText(approval), nil); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `gofmt -w clients/telegram && go test ./clients/telegram/`
Expected: `ok`.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add clients/telegram/constants.go clients/telegram/keyboards.go clients/telegram/callbacks.go clients/telegram/approvals_test.go
git commit -m "feat(telegram): deny an approval with a reason

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Record how stage 4a was built

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`

- [ ] **Step 1: Add the notes**

In the spec, at the end of section 4 (right before `## 5. Providers`), insert:

```markdown
### Implementation notes (as built, stage 4a)

- **Denial** reaches the engine as a decided approval (`InputDecided` with
  `Denied` and `Reason`); the engine writes `User denied: <reason>` (or
  `User denied.`) as an error result. Calls made outside a run get the same
  result from `ResolveApproval`. The reason is kept in `approvals.reason`.
- **Resume**: `ResolveApproval` starts a run only when none of its approvals
  is pending.
- **Barrier**: `Tools.Authorize` marks mutating tools (`Decision.Barrier`); a
  barrier waiting for approval defers every later call of the batch, read-only
  ones included (`ToolCallPart.Deferred`, no `tool.requested` until it starts).
  A read-only call waiting for approval does not hold the batch back.
- **Bridged approvals**: either decision goes to the child's approval with its
  reason; the parent's delegate call resumes when the child finishes. A missing
  pending child approval while the child is still `waiting_approval` means its
  decision is on the way (fixes the `not found` race of granting a bridge).
- **Clients** deny with a reason through the hidden `/approval deny <id>
  [reason]` command behind a prompt (TUI key `r`, Telegram button).
```

- [ ] **Step 2: Commit**

```bash
git status --short
git add docs/superpowers/specs/2026-09-23-long-running-agent-design.md
git commit -m "docs: record how stage 4a denial and the approval barrier were built

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Spec coverage (stage 4a)

| Spec requirement | Task |
|---|---|
| Denial becomes `User denied: <reason>` and the loop continues (`ResolveApproval`) | 2, 3 |
| Subagent bridge: the child continues with the denial, the parent keeps waiting | 4 |
| Crash recovery: a denied approval no longer fails the run | 5 |
| "Deny with reason" carries a reason (API, TUI, Telegram) | 1, 3, 7, 8, 9 |
| Mutating call needing approval is a barrier; later calls journaled `deferred` and executed after the decision | 6 |
| Crash recovery treats `deferred` calls as not started | 6 |
| Run parks once the runnable part is done and resumes when every approval is resolved | 3, 6 |
| Bridged approval grant `not found` race fixed, with a test | 4 |
| Manual check on the test stand: a batch `[edit, read]` in the TUI waits with the read deferred; "Deny with reason" shows the reason in the model's next reply | after Task 9 |
