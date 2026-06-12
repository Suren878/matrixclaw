# Core Refactoring Implementation Plan (Phase 2, part 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stabilize and de-tangle `internal/core`: make `internal/tools` a leaf
contract package (cutting core's transitive dependency on `webresearch`), split
the four god files along verified seams, and close the goroutine-safety audit.

**Architecture:** Pure structural refactoring — no behavior change, no public
API change. Every task is a sequence of mechanical moves verified by the full
test suite (`go build ./... && go vet ./... && go test ./...`, 18 packages ok
at baseline, plus `go test -race ./internal/core/...`).

**Tech stack:** Go 1.26, no new dependencies.

**Branch:** `refactor/core-phase2` (worktree `.worktrees/refactor-core-phase2`).
Baseline verified green at `cc37b5c`.

**Out of scope (follow-up plans):** domain-service extraction inside core,
`work` store port, module capability system (master plan Phase 3).

---

### Task 1: Extract `internal/webtools` — make `internal/tools` a leaf

The web tool cluster inside `internal/tools` imports `webresearch` (and via it
`work`), dragging engine/SQLite code into every consumer of the tools
*contract* — including `internal/core`. Move the cluster out.

**Files:**
- Move: `internal/tools/{web.go, web_safety.go, web_service.go, tool_web_fetch.go, tool_web_search.go, tool_web_research.go, web_test.go}` → `internal/webtools/` (package `webtools`)
- Modify: `internal/tools/definitions.go` (drop the `web_fetch` definition entry), `internal/tools/core_registry.go` (drop `CoreRegistryOptions.Web` special case), `internal/tools/schema.go` (move `webFetchInputSchema` to webtools)
- Modify: `internal/daemoncmd/run.go`, `internal/daemoncmd/web_research.go` (use `webtools.*`)

**Steps:**

- [ ] **1.1** Create `internal/webtools/`, `git mv` the seven files, change package clause to `webtools`, qualify tools contract symbols (`tools.Call`, `tools.Result`, `tools.Spec`, `tools.InvalidArgs`, `tools.Executor`, category/profile constants). Exported entry points after move: `webtools.NewService` (was `tools.NewWebService`), `webtools.SearchProviderConfig` (was `tools.WebSearchProviderConfig`), `webtools.NewWebFetchExecutorWithService`, `webtools.NewWebSearchExecutorWithService`, `webtools.NewWebResearchExecutorsWithService`. Unexported helpers used from `tools` (e.g. `normalizeToolID`, `rawSchema`) get local copies or exported equivalents — prefer exporting in `tools` only if already semantically public.
- [ ] **1.2** In `internal/tools`: remove the `web_fetch` entry from `coreDefinitions`, remove `CoreRegistryOptions.Web` and its branch in `CoreCodingExecutorsWithOptions` (keep `CoreRegistryOptions` if other fields remain; delete if empty — adjust the one production caller). `CategoryWeb`/`ProfileWeb`/`OutputWebContent` constants stay in `tools` (they are contract).
- [ ] **1.3** In `internal/daemoncmd/run.go` + `web_research.go`: construct `webtools.NewService(...)`, register `web_fetch` via the webtools executor in `extraTools` (it must remain present in the registry — same tool ID, same spec).
- [ ] **1.4** Verify dependency goal:
  `go list -deps ./internal/tools | grep matrixclaw` → expect only `internal/tools` (± `internal/safego`).
  `go list -deps ./internal/core | grep matrixclaw` → `webresearch` gone.
- [ ] **1.5** Run `go build ./... && go vet ./... && go test ./...` → 18+ ok, 0 FAIL. Confirm `web_fetch`, `web_search`, `web_research*` tool IDs still registered: `go test ./internal/daemoncmd/...`.
- [ ] **1.6** Commit: `refactor: extract web tools into internal/webtools, make internal/tools a leaf`

### Task 2: Split `internal/core/types.go` (689 lines) into domain type files

Pure file reorganization, same package, zero semantic diff.

**Files:** delete `internal/core/types.go`, create:

| New file | Contents (types/funcs from types.go) |
|---|---|
| `types_server.go` | `ServerStatus` |
| `types_session.go` | `SessionStatus`, `SessionKind`, `SessionRuntime`, `Session`, `SessionCapabilities`, `CapabilitiesForSession`, `SessionListFilter`, `CreateSessionInput`, `RenameSessionInput`, `PermissionMode` + consts, `UpdateSessionPermissionModeInput`, `SessionProviderOption` |
| `types_message.go` | `MessageRole`, `Message`, `MessagePartKind`, `MessagePart`, `TextPart`, `ImagePart`, `ReasoningPart`, `ToolCallPart`, `ToolResultPart`, `FinishPart`, `NormalizeMessageParts` |
| `types_run.go` | `RunStatus`, `RunTiming`, `Run`, `BusyInputMode`, `SessionInputStatus`, `SessionInput`, `HandleMessageInput`, `HandleTriggeredRunInput`, `AcceptRunResult`, `AcceptRunStatus` |
| `types_delivery.go` | `ClientBinding`, `ClientDeliveryStatus`, `ClientDeliveryTarget`, `ClientDelivery`, `ClientDeliveryFilter`, `UseBindingInput` |
| `types_subagent.go` | `SubagentTaskStatus`, `SubagentTaskMode`, `SubagentIsolation`, `SubagentRuntime`, `SubagentTask`, `SubagentTaskFilter` |
| `types_usage.go` | `UsageRecord`, `UsageFilter`, `UsageSummary`, `UsageReport` |
| `types_plan.go` | `PlanItemStatus`, `SessionPlan`, `PlanItem`, `PlanRunStatus`, `PlanRun` |
| `types_search.go` | `SearchFilter`, `SearchResult`, `SearchReport`, `SessionSearchResult`, `SessionSearchReport` |
| `types_memory.go` | `MemoryScope`, `MemoryEntry`, `MemoryFilter` |
| `types_approval.go` | `ApprovalState`, `Approval`, `PermissionRequest`, `PermissionNotification`, `FileSnapshot`, `ToolLifecycleState`, `ToolUpdate`, `ExecuteToolInput`, `ExecuteToolResult` |

**Steps:**

- [ ] **2.1** Move declarations as mapped (include each type's adjacent `const` block).
- [ ] **2.2** `gofmt -l internal/core` → empty; `go build ./... && go test ./internal/core/...` → ok.
- [ ] **2.3** Commit: `refactor: split core/types.go into domain type files`

### Task 3: Split `internal/core/context.go` (859 lines)

**Files:** shrink `context.go`, create in `internal/core/`:

| New file | Functions (verified seams) |
|---|---|
| `context.go` (keeps) | `ContextClearedMessageContent`, `SessionContext`, `CompactSession`, `autoCompactSessionIfNeeded`, `forceCompactSessionForRetry`, `providerRequestNeedsCompact`, `sessionContextSnapshot`, `compactSessionWithLoadedMessages` |
| `context_report.go` | `contextReportForSession`, `contextReport`, `sessionContextWindowTokens` + remaining report/token helpers below line 600 that they call |
| `context_compact.go` | `generateCompactSummary`, `compactSessionPlanSnapshot`, `compactSummarySystemPrompt`, `compactHistoryPrompt`, `compactMessageGroups`, `compactTailGroupStart`, `compactGroupHasUserMessage`, `compactGroupTextForSummary`, `compactMessageTextForSummary`, `messageToolCallIDs`, `messageIsToolResultFor`, `compactMessagePartsTextForSummary`, `compactTextPartLimit`, `compactToolPartLimit`, `trimRunesEnd`, `trimRunesFromStart` |
| `context_markers.go` | `latestContextMarker`, `latestCompactSummary`, `latestCompactSummaryForRun` + marker helpers |

**Steps:**

- [ ] **3.1** Move per table (when in doubt, group a helper with its only caller's file).
- [ ] **3.2** `go test ./internal/core/... -run 'Context|Compact' -v` → pass; full core suite → pass.
- [ ] **3.3** Commit: `refactor: split core context.go by responsibility`

### Task 4: Split `internal/core/execution_provider.go` (957 lines)

**Files:** create in `internal/core/`:

| New file | Functions |
|---|---|
| `execution_request.go` | `buildProviderRequest`, `providerSystemPrompt`, `skillsPromptContext`, `runtimeStatusPromptContext`, `runtimeStatusToolIDs`, `providerToolDefinitions`, `runtimeToolUseMode`, `runtimeToolUseAllowed`, `clientSupportsVoiceDelivery`, `clientSupportsDocumentDelivery` |
| `execution_prompts.go` | `joinPromptSections`, `AssistantSystemPrompt`, `responseLanguageGuidancePrompt`, `toolUseDisciplinePrompt`, `webResearchPromptAvailable`, `webResearchGuidancePrompt`, `voiceOutputGuidancePrompt`, `fileDeliveryPromptAvailable`, `fileDeliveryGuidancePrompt`, `delegateTaskPromptAvailable`, `delegateTaskGuidancePrompt`, `delegateTaskToolDescription`, `subagentRuntimeInfo` type+methods, `subagentRuntimeAlias`, `subagentRuntimeDetail`, `normalizeModelNames`, `availableSubagentRuntimeIDs`, `currentProjectRootPrompt`, `sessionPlanPrompt` |
| `execution_conversation.go` | `buildProviderConversation` and everything from line 474 down: `buildProviderConversationWithAttachmentsForRun`, `convertProviderConversationHistory`, `collectProviderToolResults`, `isPairedToolResultMessage`, `isToolCallOnlyProviderMessage`, `batchAdjacentToolCallMessages`, `isAdditionalBatchableToolCallMessage`, `appendProviderToolResults`, `syntheticFailedToolResult`, `buildTextOnlyProviderConversationForRun`, `skipInternalPlanPromptForProvider`, `IsPlanRunPromptMessage`, `textOnlyProviderContent`, `formatToolCallAsText`, `formatToolResultAsText`, `providerVisibleToolResultContent`, `isBrowserSnapshotToolName`, `trimProviderToolResult`, `toProviderMessages`, `messageReasoningContent`, `imagePartLabel`, `providerImageContent`, `messageContentWithAttachmentRefs`, `quoteAttachmentValue` |

`execution_provider.go` is deleted when empty.

**Steps:**

- [ ] **4.1** Move per table.
- [ ] **4.2** Full core suite + `go vet ./...` → pass.
- [ ] **4.3** Commit: `refactor: split core execution_provider.go into request/prompts/conversation`

### Task 5: Split `internal/core/subagents_async.go` (1042 lines)

**Files:** create in `internal/core/`:

| New file | Functions |
|---|---|
| `subagents_async_tools.go` | `SubagentToolExecutors`, `SpawnSubagentToolExecutor`, `ListSubagentsToolExecutor`, `ReadSubagentResultToolExecutor`, the three tool structs with `Spec`/`Execute` |
| `subagents_async.go` (keeps) | `SpawnSubagent`, `ListSubagents`, `ReadSubagentResult`, `recordSubagentResultMessage`, `normalizeSubagentIsolation`, `normalizeSubagentDisplayName`, `generatedSubagentDisplayName`, `subagentParentToolName`, `asyncSubagentUserPrompt` |
| `subagents_lifecycle.go` | `touchAsyncSubagentTaskActivity`, `afterRunExecution`, `syncAsyncSubagentTaskAfterRun`, `syncBlockingSubagentTaskAfterRun`, `deliverPendingSubagentCompletionsForParent`, `parentReadyForSubagentAutoResume`, `updateSubagentResultMessage`, `RecoverSubagentTasks`, `activeSubagentTaskStatuses` |
| `subagents_worktree.go` | `prepareSubagentWorktree`, `gitCommandOutput` |

**Steps:**

- [ ] **5.1** Move per table.
- [ ] **5.2** `go test ./internal/core/... -run Subagent -v` → pass; full suite → pass.
- [ ] **5.3** Commit: `refactor: split core subagents_async.go by lifecycle stage`

### Task 6: Goroutine-safety audit in core paths (stabilization)

**Files:** inspect (modify only where a finding requires it):
`internal/core/subagents.go:342` (`waitForSubagentTerminalAndResumeParent`),
`internal/core/events.go`, any `go func(` in `internal/core/`.

**Steps:**

- [ ] **6.1** `grep -n "go func\|safego" internal/core/*.go` — every background goroutine must run under `safego.Go` (panic-recovery). Wrap any that do not, matching the existing call style: `safego.Go("core.<name>", func() { ... })`.
- [ ] **6.2** Verify no goroutine captures a request-scoped `ctx` for work that must outlive the request (the docs/REFACTORING.md Phase 2 audit item, scoped to core only).
- [ ] **6.3** `go test -race -count=2 ./internal/core/...` → ok.
- [ ] **6.4** Commit (only if changes): `fix: recover panics in core background goroutines`

### Task 7: Final verification and merge prep

- [ ] **7.1** `gofmt -l . | grep -v .worktrees` → empty; `go build ./... && go vet ./... && go test ./...` → 18+ ok, 0 FAIL; `go test -race ./internal/core/... ./internal/store/...` → ok.
- [ ] **7.2** Confirm no file in `internal/core/` exceeds ~700 lines (`find internal/core -name '*.go' -not -name '*_test.go' | xargs wc -l | sort -rn | head -5`); confirm dependency goals from Task 1.4 still hold.
- [ ] **7.3** Update `docs/REFACTORING.md`: mark the safego/core items done; link this plan.
- [ ] **7.4** Commit docs; hand off via superpowers:finishing-a-development-branch (merge to `main` after review).
