# Tool contract: core decides approval, tools preview (2026-10-03)

Phase C item 2 of the audit cleanup (`audit/providers-tools.md` items 2, 17,
18, 22 and area verdict point 1). Phase B already slimmed `tools.Spec`.

## Current shape

- **Two deciders.** `core/tool_call_run.go` evaluates the permission rules, then
  for every call no rule decides runs the executor with `call.Approved=false`
  and lets it decide: 17 `if !call.Approved` sites (`tools/shell.go` bash and
  task_kill, `mutation_{write,edit,multiedit}.go`, `skills/tools.go`,
  `automation/tools.go`, `core/memory_tools.go` x3, `mcp/client.go`,
  `modules/{storage,delivery,telephony}`) return `Result.Approval`. Whether a
  tool asks is invisible to core until it has run, so `coreTools.Authorize`
  guesses the barrier (`Mutates && verdict != Allow`): `text_to_speech`,
  `storage_save`, `agent` and other mutating tools that never ask still hold
  back the rest of their batch.
- **Approval travels through results.** `tools.Result.Approval`,
  `tools.Call.Approved`, `core.ExecuteToolInput.Approved` (the API rejects it,
  `replayApprovedTool` sets it) and `agent.callRequest.approved` carry one flag
  through four layers; the engine trusts it.
- **Action strings** are invented per tool ("execute", "kill", "memory:add",
  "mcp:srv:tool", "ask_rule", "retry_after_daemon_restart"), stored in
  `approvals.action`, shown as "Action: ..." by Telegram and the terminal
  dialog, decided on by nothing.
- **File tools.** Three executors (~300 lines) read and validate the file twice
  (dry run on a 64 KB preview with its own `applyEditPreview`, then for real),
  three identical `*PermissionsParams` and three `*ResponseMetadata` types,
  `FailedEdit`/`EditsFailed` never written (multiedit is all-or-nothing) yet
  rendered by the TUI, and `Result.FileVersion` repeats the metadata's full old
  and new content (read only by the MCP server's structured output).
- **Two status fields.** `tools.Result.{Status,IsError}` (191 `IsError` uses in
  77 files; some set one, some both) and the persisted
  `transcript.ToolResultPart.{Status,IsError}`; `agent.ToolResultStatus` and
  `core.normalizeToolResultStatus` reconcile them. The TUI re-runs
  `tools.IsProcessProbeCommand` to recompute the neutral status the server
  already stored.
- **Bash guard.** `tools/shell_browser_guard.go` (142 lines) tokenizes the
  command with its own quote/escape scanner.

## Target shape

### tools

```go
type Spec struct {
	ID, Description string
	Effect          Effect   // readonly | mutation: concurrency, read-only subagents, recovery
	// Asks makes a call no rule decides wait for the user's approval.
	Asks            bool     `json:"asks,omitempty"`
	Namespace       string
	Category        Category
	InputJSONSchema json.RawMessage
}

// ApprovalRequest is what the user sees before a call runs; core sets
// Suggestion from the permission rules.
type ApprovalRequest struct {
	Description string                 `json:"description"`
	Path        string                 `json:"path,omitempty"`
	Params      any                    `json:"params,omitempty"`
	Suggestion  *permission.Suggestion `json:"suggestion,omitempty"`
}

// Previewer builds a call's approval without side effects: a diff, a command
// and its directory, a validated schedule. An error answers the call instead.
type Previewer interface {
	Preview(ctx context.Context, call Call) (ApprovalRequest, error)
}

func (r *Registry) Preview(ctx, toolID string, call Call) (ApprovalRequest, error)
// default for tools without Preview: "Run <tool>" with the call's arguments.

type Result struct { Content, Metadata, MIMEType, Status, OutputPath, Await, Waiting }
func (r Result) IsError() bool   // Status == error; "" is stored as success
```

Gone: `Call.Approved`, `Result.{Approval,IsError,FileVersion}`,
`ApprovalRequest.{ID,ToolID,ToolCallID,Action}`, `approvalResult`. The
existing optional hooks `SubjectProvider` and `ConcurrencyKeyProvider` stay.

Spec defaults: `Asks` on write, edit, multiedit, bash, task_kill, send_file,
storage_delete, skill_manage, memory, create_scheduled_ai_task,
telephony_call, and every MCP tool that is not read-only (read-only server or
the four page-reading browser tools), browser included. `memory`'s subject
becomes its action (`KindName`), and the presets of every mode carry one
built-in rule `memory: list` = allow, so listing still never asks while a user
`ask`/`deny` rule on `memory` wins as for any tool. Mutating tools that never
asked (agent, skill_use, text_to_speech, storage_save/_temp/_update_metadata,
create_reminder, end_call) keep `Asks=false`.

### core (single gate)

`checkPermission` returns the verdict; `callPermission.asks(spec)` is
`Ask`, or no verdict and `spec.Asks`. A call that asks runs only with a
**grant**: its newest approval is approved and it has no result yet, read from
the store (`Core.callGranted`), so no flag is trusted from the engine or the
API, and a grant is used once (an API replay of a finished call ID asks again).

- `coreTools.Authorize`: deny -> rejected; `Barrier = Mutates && asks && !granted`
  (an asking read-only call still parks without holding the batch).
- `coreTools.Execute(ctx, name, call) (tools.Result, *tools.ApprovalRequest, error)`
  on the call's goroutine, under its concurrency key: asks and not granted ->
  `Registry.Preview` (so it sees the writes of earlier calls of the batch); a
  preview error becomes the call's error result; otherwise execute.
- `Core.ExecuteTool` (run-less) shares the same function; `ExecuteToolInput.Approved`
  goes, `replayApprovedTool` relies on the grant.
- `askedByRule` goes: an ask rule shows the tool's preview (diff, command)
  instead of a generic "Rule X asks" text; the rule is named in the description.

### agent

`Tools.Execute` returns the approval request instead of `Result.Approval`;
`callRequest.approved` and the `!approved` checks in `batch.go` go (core already
answered with the grant). `Pending`/`InterruptedCall` keep `tools.ApprovalRequest`.

### Restart recovery (unchanged semantics)

`interruptedCalls` still asks again for every mutating call that may have run
(new pending approval, which supersedes the old grant); read-only and
never-started calls rerun through Authorize, where an unused grant counts.
Pinned by the existing crash-recovery tests plus a new one: a granted mutating
call interrupted mid-execution does not run until the new approval is granted.

### File mutations

One executor type for write/edit/multiedit (`tools/mutation.go`):

```go
type fileMutation struct {
	path   FilesystemPathPolicy
	create bool                          // write may create the file
	apply  func(old string) (string, error)
}
func mutateFile(m fileMutation, workingDir string, commit bool) (FileChange, error)

// FileChange is a file-changing call's result metadata and approval params.
type FileChange struct {
	Path       string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
}
```

Preview runs the same transform on the whole file (exact validation: "old_string
not found" answers the call before it asks) and cuts old/new to 64 KB with a
notice; Execute re-reads, applies and writes. Edit is one `EditOperation`.
Persisted metadata drops `diff`, `edits_applied`, the path-policy fields and
`FileVersion`; `toolview.FileChangeOf` keeps reading `old_content`,
`new_content`, `additions`, `removals`; the terminal dialog decodes
`tools.FileChange` (and `tools.BashParams` for bash); the TUI's
"n/m edits applied" branch goes. Old rows still decode (same JSON names).

### Status

`tools.Result.Status` only (`IsError()` method). `transcript.ToolResultPart`
drops `is_error` too (`IsError()` method on the part); a store migration sets
`status='error'` on persisted tool results that have `is_error` and no status
and strips `is_error` (one `UPDATE` over `json_each`). Readers switch to the
status: agent context (provider `IsError` stays, it is wire format),
`core/snapshot.go`, voice, Telegram, terminal read model, iOS
`ToolResultPart.isError` removed. The TUI trusts the stored status:
`isExpectedNeutralBashResult` goes and `IsProcessProbeCommand` becomes private.

### Bash guard

`blockedManagedBrowserInstallCommand` reuses `permission.ParseLine`: a raw
mention of the managed runtime paths, or a simple command that is
`playwright-mcp ... install-browser`, or `npx`/`npm exec` with `@playwright/mcp`
and `install-browser`. Checked by bash's Preview and Execute alike, so a blocked
command never asks first. ~110 lines removed. (A deny rule cannot express it:
command patterns match exact words, not `@playwright/mcp@<version>`.)

### Contract and data changes

- `approvals.action` column dropped (migration); `Approval.Action`,
  `PermissionRequest.Action`, voice `ApprovalRequestedPayload.Action`, Telegram
  "Action:" line, terminal `PermissionRequest.Action`, iOS `action` removed.
- `ExecuteToolInput.approved` removed from the API (it was rejected anyway).
- `tools.Spec` gains `asks`; tool results lose `is_error`; file-change
  metadata loses `diff`/`edits_applied`/path-policy fields.

## Commits (each green)

1. `refactor(tools): Result.Status is the only status field` (+ transcript
   migration, clients, TUI trusts status).
2. `refactor(tools): one file mutation path and FileChange metadata` (dry runs
   still in place, built from the same helper).
3. `refactor(tools): the managed-browser guard parses with permission.ParseLine`.
4. `refactor(core): core decides approval, tools only preview` (Spec.Asks,
   Previewer, grants from the store, every executor converted, memory rule).
5. `refactor(core): drop approval action strings` (+ column migration, clients).
6. `docs: tool contract in ARCHITECTURE; as-built notes`.

Rough size: ~-900/+500 lines.

## Risks and tests

- Approval regressions (a tool that asked no longer asks): a table test over
  the registered specs pins `Asks` per tool; core tests for default ask, allow
  rule, ask rule with preview, deny, preview error, read-only subagent refusal.
- Grant from the store: replaying a finished call ID asks again; a pending
  retry approval blocks a re-run (recovery test above).
- Preview ordering: a batch writing then editing one file shows the edit's
  diff against the written content.
- Migration tests for `is_error` -> status and the dropped column
  (`schema_upgrade_test.go` pattern).
