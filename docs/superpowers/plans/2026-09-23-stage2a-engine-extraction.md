# Stage 2a — Engine Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the native agent loop out of `internal/core` into a new `internal/agent` engine behind narrow ports, switch `core.ExecuteRun` to it with identical observable behaviour, delete the old loop, and add `interrupted` rescheduling.

**Architecture:** `internal/agent` owns the loop (`Engine.Run(ctx, Task) (Outcome, error)`), an in-memory single-writer transcript over the `Journal` port, generation/retry/streaming, tool batches, approvals resume and auto-compaction. `internal/agent/context` (package `agentcontext`) owns the model's view of a session (conversation building, markers, compaction, token estimates); `internal/agent/prompt` owns fixed prompt texts. `core` implements the ports, builds the per-step system prompt, and applies the returned `Outcome` (completion, park, cancel, failure, crash-recovery preservation + rescheduling). Characterization tests pin today's behaviour through `core` + SQLite before anything moves and stay unchanged across the switch.

**Tech Stack:** Go 1.26, SQLite store (`internal/store`), `internal/providers` runtimes, go-workflows orchestration (unchanged).

---

## 0. Conventions used by this plan

- Repo: `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`, work directly on `main`. Another session may touch the repo: stage only the paths listed in each commit step, never `git add -A`.
- Stages 0, 1 and 1b are merged (see the cross-stage contracts, including "Contract amendments", and `docs/superpowers/plans/2026-09-23-stage0-foundations.md` for exact Stage 0 names). All code in this plan uses their names: `transcript.Message` and part types, `Message.Seq`, `store.GetMessage/HasToolResult/ListMessagesAfter`, normalised `providers.Usage{PromptTokens, OutputTokens, CacheReadTokens, CacheWriteTokens, ReasoningTokens, ProviderRaw}` with `Usage.IsZero()` (Stage 0 deleted `core.ProviderUsage`; finish-part usage and `ContextReport.LastProviderUsage` use `providers.Usage`), `RunStep` + `SaveRunStep/ListRunSteps/ListUsageRecords` (the store numbers steps itself), `c.recordRunStep(ctx, runID, response, stopReason, latency)` and `generationStopReason` in `internal/core/usage.go`, Stage 0's `c.compactSession(ctx, sessionID, runID)` / `compactSessionWithLoadedMessages(ctx, session, messages, runID)` / `generateCompactSummary(ctx, session, messages, runID)` (in-run summaries are recorded as `compact` steps), `c.sessionToolCallMessage(ctx, sessionID, toolCallID)` and `toolCallArgs(message) (json.RawMessage, bool)`, `providers.StopReason` / `Response.StopReason`, `Request.CacheKey`, `providers.ReasoningBlock` / `Response.Reasoning`, `transcript.ReasoningPart.RedactedData`, `providers.Message.IsError`, and core's "one assistant message per response's parallel calls" conversation builder.
- Line numbers quoted as `file:NN-MM` refer to commit `6176876` (before Stages 0–1b). Stages 0–1b shift them; always locate code by the function name given next to the range.
- "Move verbatim" means: cut the function body exactly as it is on `main` now (post Stage 0/1/1b), paste it into the destination, apply only the listed identifier changes. Any logic change is shown in full in this plan.
- Owner rules: delete replaced code (no shims, aliases or dead code at the end of the stage), doc comments ≤ 4 lines without history, tests only on observable behaviour.
- Every commit must pass `go build ./... && go vet ./... && go test ./...`.
- Commit messages end with a blank line and `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## 1. Deviations from spec §1 (recorded, intentional)

1. **Package name.** `internal/agent/context` declares `package agentcontext` (a package named `context` would shadow the standard library in every file that needs both). It is always imported as `agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"`.
2. **`Journal`** gains `BeginStreaming` and `Stream` next to `FinishStreaming`: streamed progress goes through the store's fast progress path (`MessageProgressStore`) exactly as today, and only `FinishStreaming` does the durable, search-indexed update. `Load` returns the **whole session** in 2a (marker parsing stays text-based until the Stage 3 `seq` boundary). `Append`/`BeginStreaming` return `seq` from a new `INSERT … RETURNING seq` path (`store.AppendMessage`, and `SaveMessageProgress` now returns the seq; Task 4). `Step.StopReason` is a plain string because `run_steps` also records in-run summary generations as `compact`; step numbers stay assigned by the store (a step is recorded once per successful `Generate` and never replayed, so no upsert is needed).
3. **`Tools`**: `Specs(ctx)` (the core adapter is built per run, so it needs no `Task`); `Authorize(ctx, name, call)` and `Execute(ctx, name, call)` take the tool name because `tools.Call` has none. In 2a `Authorize` only performs today's pre-execution validation (unknown tool, tool not allowed for child subagents, `delegate_task` in external sessions); whether a call needs approval is still reported by `Execute` through `tools.Result.Approval` (the dry-run moves into `Authorize` with `internal/permission` in 4b). A fourth method `Finish(ctx, name, call, result, resultMessage)` keeps today's post-result side effects (file-version snapshot, subagent result-message link) in their current order with their current error semantics. `Execute` errors are fatal; tool failures come back as `IsError` results.
4. **`Approvals`** gains `Pending(ctx, runID) (bool, error)` (today's `runHasPendingApprovals` check that decides between parking and continuing).
5. **`Inbox`**: `Drain(ctx, runID, kind InputKind) ([]Input, error)` — `InputSteer` consumes pending steer text, `InputApproved` idempotently returns granted approvals whose tool call has no result yet (the adapter checks `HasToolResult`). It returns `[]Input`, not the Sink's `Event` type. It also has `Canceled(ctx, runID)` because today's loop polls the run's durable status while streaming and after every generation.
6. **`Sink`** emits `message.created/updated` and tool lifecycle events (`tool.requested`, `tool.finished`); message events carry the message.
7. **New port `Prompts`** (`System`, `PlanSnapshot`, `Budget`): today the system prompt is rebuilt every step from core state (plan, memory, skills, runtime status, recovery notice) and the context budget is re-read every step; the stable prompt is Stage 3. The assembly stays in core (`internal/core/agent_prompts.go`), the fixed texts move to `internal/agent/prompt`.
8. **Outcome writes stay in core.** The engine is the only transcript writer *while `Run` executes*. The final reply is written by core together with the run status (`store.CompleteRun`, one transaction) when it applies `Outcome{completed}`, and the errored / canceled / daemon-restart seals are written when it applies the other outcomes. The engine finalizes the message (content, parts, usage finish part) but does not persist it; this keeps today's atomic completion that crash recovery relies on.
9. **Context API types stay in core.** `ContextReport`, `ContextBlock`, `ContextBlockKind`, `ContextCompact`, `CompactSessionResult`, `ErrSessionRequired`, `SessionContext`, `CompactSession` are API types/methods used by `api`, `controlplane`, `clientruntime`, `daemonclient` and the TUI; they move to the new file `internal/core/session_context.go` and use `agentcontext` for all logic. The finish-part check `messageHasFinishReason` becomes `transcript.HasFinishReason` ("finish metadata" in spec §1).
10. **Relocated public helpers** (importers updated, no aliases): `core.IsProviderSupportedImageMIMEType` → `providers.IsSupportedImageMIMEType`; `core.IsPlanRunPromptMessage` → `prompt.IsPlanRunPrompt` (deleted with Planning Mode in Stage 5); `core.AssistantSystemPrompt(profile)` → `prompt.AssistantSystemPrompt(name, systemPrompt)`; `core.ContextClearedMessageContent` → `agentcontext.ClearedMarkerContent`; `core.EstimateTextTokens`, `core.EstimatedImageTokens`, `core.FormatShortNumber` → `agentcontext`; `core.AttachmentReader/AttachmentData/ErrAttachmentUnavailable` → `agentcontext`.
11. **`recordRunStep` takes an `agent.Step`** instead of `(runID, response, stopReason, latency)`; the engine records every successful `Generate` (including empty replies that are retried, as Stage 0 does) with `stepStopReason` (moved `generationStopReason`) and every in-run summary with `compact`.
12. **Daemon lifetime.** Spec §1 defines `interrupted` as "context cancelled while the daemon keeps running", but the daemon has no such signal; 2a adds `Core.WithLifetime(ctx)` (default `context.Background()`), cancelled in `daemoncmd.Run` before the workflow adapter closes.

## 2. Behaviour changes (exact list; everything else is identical)

- **B1 Steer at step start.** Today steer text is merged into the *next* tool result before it is written (kept) **and**, at the start of every step, rewritten into the run's latest *already written* tool result. The second path mutates a persisted message and is removed. Steers that arrive after a step's last tool result was written stay pending and are merged into the next tool result; if the run ends with a text reply they are re-queued as a new run by `afterRunExecution` (unchanged). The window is: after the last tool result of a batch is written and before the next step's first tool result (in practice the model's final text generation).
- **B2 Steer-merged tool result written once.** A tool result with merged steer guidance is created once instead of created and then updated. Final stored content is identical; clients see one `message.created` instead of `created`+`updated` for that write. (Reasoning needs no change here: since Stage 1 Task 4 it is written with the step's reply message by `saveAssistantToolTurn`, and the engine's `run.finishToolTurn` keeps exactly that.)
- **B3 Async subagent results are not rewritten.** `updateSubagentResultMessage` (rewrote the parent's `spawn_subagent` tool result when the child finished) is removed. The `tool.updated` event for the parent call is still published; the result reaches the parent through the existing completion run (`subagentCompletionPrompt`) and `read_subagent_result`.
- **B4 `/compact` and `/clear` while a run executes** return `ErrRunActive` (HTTP 409) when a run of that session is executing in this daemon. Other system notices are unaffected.
- **B5 Foreign messages during a run.** Messages written into the session by others while a native run executes (run-less `ExecuteTool` from API/voice/MCP, system notices) are not visible to that run's later steps; the next run sees them.
- **B6 Interrupted runs are rescheduled.** When a native run's context stops (not a user cancel) and the daemon lifetime is alive, core keeps the run `running` with a `recovering` checkpoint exactly as today and then calls `startRun`; the new execution goes through the existing crash-recovery path (`RecoveryCount`+1, max 8, recovery notice in the prompt).
- **B7 Race-window details.** One run context is used for all engine work: a cancel during compaction or between steps aborts the in-flight store/model call instead of letting it finish (final run state unchanged); a run cancelled in the instant its reply completed is sealed with the final reply text rather than the streamed preview; summary generation uses the model resolved at run start (today re-resolved per compaction).

## 3. Function mapping (every function of the deleted files)

| Old location (core) | New home | Task |
|---|---|---|
| `execution.go` `maxRunToolSteps` | `agent/engine.go` `maxSteps` | 8, 10 |
| `execution.go` `orphanedRunError`, `failOrphanedRun`, `persistAssistantError` | `core/run_status.go` (verbatim) | 10 |
| `execution.go` `runExecution`, `newRunExecution` | deleted (replaced by `agent.Task`) | 10 |
| `execution.go` `ExecuteRun`, `prepareRunExecution` | `core/run_execute.go` `ExecuteRun`, `prepareNativeRun` (rewritten) | 9 |
| `execution.go` `executeRunLoop`, `executeRunStep` | `agent/engine.go` `Engine.Run`, `run.step` | 8 |
| `execution.go` `applyRunTurnResult`, `checkAndHandleCanceled` | `agent/engine.go` `run.settle` + `core/run_outcome.go` `applyOutcome` | 8, 9 |
| `execution.go` `runHasPendingApprovals` | `core/tool_approvals.go` (verbatim) | 10 |
| `execution.go` `resolveSessionRuntime` | `core/run_execute.go` (verbatim) | 10 |
| `execution_turn.go` `errRunCanceled`, `generateAssistantTurn` | `agent/generate.go` `errRunCanceled`, `run.generate` | 8 |
| `execution_turn.go` `turnExecution`, `newTurnExecution`, `hasRunScope`, `toolInput` | `agent.Task`, core `nativeTurn`, `agent/tools.go` `run.runCall`; `hasRunScope` deleted (Task validated in `Run`) | 8, 9 |
| `execution_turn.go` `turnStepOutcome`, `turnStepResult` | `agent/engine.go` `stepKind`, `stepResult` | 8 |
| `execution_turn.go` `handleAssistantTurnResponse` | `agent/engine.go` `run.handleResponse` | 8 |
| `execution_turn.go` `resumeApprovedTools` | `agent/tools.go` `run.resumeApproved` + `core/agent_inbox.go` `coreInbox.approved` | 8, 9 |
| `execution_request.go` `buildProviderRequest` | `agent/request.go` `run.buildRequest` | 8 |
| `execution_request.go` `providerSystemPrompt`, `skillsPromptContext`, `runtimeStatusPromptContext`, `runtimeStatusToolIDs` | `core/agent_prompts.go` `nativeSystemPrompt`, `nativeSkillsPrompt`, `nativeStatusPrompt`, `nativeStatusToolIDs` | 9 |
| `execution_request.go` `providerToolDefinitions` | `core/agent_tools.go` `nativeToolSpecs` + `coreTools.Specs`; `agent/request.go` `toolDefinitions` | 8, 9 |
| `execution_request.go` `clientSupportsVoiceDelivery`, `clientSupportsDocumentDelivery` | `core/agent_tools.go` (verbatim) | 10 |
| `execution_request.go` `runtimeToolUseMode`, `runtimeToolUseAllowed`, `runtimeImageInputAllowed` | `agent/request.go` `ToolUseAllowed`, `ImageInputAllowed` | 8 |
| `execution_prompts.go` `joinPromptSections`, `AssistantSystemPrompt`, `responseLanguageGuidancePrompt`, `currentProjectRootPrompt` | `agent/prompt/prompt.go` `JoinSections`, `AssistantSystemPrompt(name, systemPrompt)`, `languageGuidance`, `ProjectRoot` | 5 |
| `execution_prompts.go` `toolUseDisciplinePrompt`, `webResearchGuidancePrompt`, `voiceOutputGuidancePrompt`, `fileDeliveryGuidancePrompt`, `telephonyCallGuidancePrompt` | `agent/prompt/guidance.go` `ToolUseDiscipline`, `WebResearchGuidance`, `VoiceOutputGuidance`, `FileDeliveryGuidance`, `TelephonyCallGuidance` | 5 |
| `execution_prompts.go` `webResearchPromptAvailable`, `fileDeliveryPromptAvailable`, `telephonyCallPromptAvailable`, `delegateTaskPromptAvailable` | `core/agent_prompts.go` (verbatim) | 5 |
| `execution_prompts.go` `delegateTaskGuidancePrompt`, `delegateTaskToolDescription`, type `subagentRuntimeInfo` + `PromptLine`, method `subagentRuntimeInfo`, `subagentRuntimeAlias`, `subagentRuntimeDetail`, `normalizeModelNames`, `availableSubagentRuntimeIDs` | `core/subagents_prompt.go` (verbatim) | 5 |
| `execution_prompts.go` `sessionPlanPrompt` | `core/plan.go` (verbatim) | 5 |
| `execution_generation.go` `generateAssistantTurnWithRetry` | `agent/generate.go` `run.generateWithRetry` | 8 |
| `execution_generation.go` `saveAssistantToolTurn` | `agent/engine.go` `run.finishToolTurn` | 8 |
| `execution_generation.go` `stopReasonError` (Stage 1 Task 3) | `agent/context/errors.go` `StopReasonError` (old loop and summaries call it from Task 6; engine from Task 8) | 6 |
| `execution_generation.go` `responseReasoningParts` (Stage 1 Task 4) | `agent/messages.go` `reasoningParts` (copy; core copy deleted with the file) | 7, 10 |
| `usage.go` `generationStopReason` (Stage 0); the `recordRunStep` call in `generateAssistantTurn` | `agent/generate.go` `stepStopReason`, `run.recordStep` → `Journal.RecordStep` → `core/usage.go` `recordRunStep(ctx, agent.Step)` | 8, 9, 10 |
| `execution_status.go` `completeAssistantTurn` | `core/run_outcome.go` (message building removed → `agent.finalReply`) | 9, 10 |
| `execution_status.go` `planRunLooksBlocked`, `completeDonePlanParents`, `allPlanChildrenTerminal`, `completeActivePlanItemsIfRunFinished` | `core/plan.go` (verbatim) | 10 |
| `execution_status.go` `providerResponseMessageParts`, `providerUsageFinishPart` | `agent/messages.go` `finalReply`, `usageFinishPart` | 7, 10 |
| `execution_status.go` `setRunStatus`, `markAssistantErrored`, `saveAssistantErrored`, `appendErrorFinishPart`, `appendCanceledFinishPart`, `isRunCanceled`, `finishCanceledAssistant`, `failAcceptedRun`, `failRunByID` | `core/run_status.go` (verbatim) | 10 |
| `execution_tools.go` `executeRequestedTools`, `sameRequestedTool`, `recordRejectedToolRequest` (`attachReasoningToToolCallMessage` was already deleted by Stage 1 Task 4) | `agent/tools.go` `run.executeBatch`, `sameRequestedTool`, `run.rejectCall` | 8 |
| `execution_conversation.go` (all) | `agent/context/conversation.go`; entry points `Conversation`, `TextOnlyConversation`; `IsProviderSupportedImageMIMEType` → providers (Task 4); `IsPlanRunPromptMessage` → prompt (Task 5) | 4, 5, 6 |
| `context.go` consts `compactMessagePrefix`, `contextClearedMessagePrefix`, `contextClearedSummary`, `compactBackoffMinimumSavingsPercent` | `agent/context/markers.go` | 6 |
| `context.go` `maxCompactPromptRunes` | `agent/context/compact.go` | 6 |
| `context.go` `EstimatedImageTokens` | `agent/context/estimate.go` | 6 |
| `context.go` `CompactSessionResult`, `ContextBlockKind`+consts, `ContextBlock`, `ContextReport`, `ContextCompact`, `ErrSessionRequired`, `SessionContext`, `CompactSession` | `core/session_context.go` | 6 |
| `context.go` `compactSession(ctx, sessionID, runID)` (Stage 0) | `core/session_context.go`, run-bound recording via `runStepGenerator` until the old loop is gone; folded back into `CompactSession` | 6, 10 |
| `context.go` `ContextClearedMessageContent` | `agentcontext.ClearedMarkerContent` | 6 |
| `context.go` `autoCompactSessionIfNeeded`, `forceCompactSessionForRetry`, `providerRequestNeedsCompact`, `sessionContextSnapshot`, `compactSessionWithLoadedMessages` | temporarily `core/execution_compaction.go` (old loop), deleted; engine: `run.autoCompact`, `run.compact`, `run.requestNeedsCompact`, `run.compactHistory` | 6, 8, 10 |
| `context_compact.go` `generateCompactSummary` | `agentcontext.summarize` (called by `agentcontext.Compact`); its Stage 0 `compact` step recording → `runStepGenerator` (old loop) / `summaryModel` (engine) | 6, 8 |
| `context_compact.go` `compactSessionPlanSnapshot` | `core/plan_tools.go` (verbatim) | 6 |
| `context_compact.go` `compactSummarySystemPrompt`, `compactHistoryPrompt`, `compactMessageGroup(s)`, `compactTailGroupStart`, `compactGroupHasUserMessage`, `compactGroupTextForSummary`, `compactMessageTextForSummary`, `messageToolCallIDs`, `messageIsToolResultFor`, `compactMessagePartsTextForSummary`, `compactTextPartLimit`, `compactToolPartLimit`, `trimRunesEnd`, `trimRunesFromStart` | `agent/context/compact.go` (verbatim) | 6 |
| `context_errors.go` `isContextLengthExceededError` | `agentcontext.IsContextLengthExceeded` | 6 |
| `context_markers.go` `contextMarker`, `latestContextMarker`, `latestCompactSummary`, `latestCompactSummaryForRun`, `compactBackoffActive` | `agentcontext.Marker`, `LatestMarker`, `LatestSummary`, `LatestSummaryForRun`, `CompactBackoffActive` | 6 |
| `context_markers.go` `compactMarkerSummary`, `clearMarkerSummary`, `compactStatsPattern`, `parseCompactStats`, `parseShortTokenNumber` | `agent/context/markers.go` (verbatim) | 6 |
| `context_report.go` `contextReportForSession`, `contextReport`, `sessionContextWindowTokens`, `estimateToolSchemaTokens`, `latestProviderUsage` | `core/session_context.go` | 6 |
| `context_report.go` `EstimateMessageTokens`, `EstimateTextTokens`, `EstimateProviderRequestTokens`, `FormatShortNumber` | `agent/context/estimate.go` (`EstimateProviderRequestTokens` → `EstimateRequestTokens`) | 6 |
| `context_report.go` `compactRecommendation`, `compactRecommendationForWindow`, `compactThresholdForWindow` | `agentcontext.Recommendation`, `agentcontext.Threshold` | 6 |
| Other files touched: `assistant_sanitize.go` (only used by the native loop) | `agent/sanitize.go` | 8, 10 |
| `session_inputs.go` `drainPendingSteersIntoLatestToolResult`, `injectPendingSteersIntoToolResultMessage`, `messageHasToolResult` | deleted (B1); consumption → `coreInbox.steers` | 9, 10 |
| `session_inputs.go` `appendUserGuidanceToToolResult`, `appendGuidanceBlock` | `agent/tools.go` | 8, 10 |
| `run_recovery.go` `applyRunTurnResultAfterContextStopped` | `core/run_outcome.go` `applyInterruptedOutcome` | 9, 10 |
| `run_recovery.go` `messageHasFinishReason`, `messageInterruptedByDaemonRestart` | `transcript.HasFinishReason`; conversation uses it directly | 4, 6 |
| `store` `insertMessage` (no seq returned) | `insertMessage` returns seq via `RETURNING`; new `AppendMessage`; `SaveMessageProgress` returns seq | 4 |
| `tool_call_prepare.go` `newToolCallMessage` | `agent.ToolCallMessage` | 10 |
| `tool_call_finish.go` message building in `saveToolResultMessage`, `toolResultStatus` | `agent.ToolResultMessage`, `agent.ToolResultStatus` | 10 |
| `subagents_lifecycle.go` `updateSubagentResultMessage`; `subagents_presentation.go` `subagentFinishedResultContent`; `tool_helpers.go` `normalizeToolContent` | deleted (B3); event via `publishSubagentToolUpdate` | 12 |

## 4. File structure after Stage 2a

```
internal/transcript/finish.go           HasFinishReason
internal/providers/images.go            IsSupportedImageMIMEType
internal/store/sqlite_messages.go       insertMessage … RETURNING seq, AppendMessage
internal/agent/ports.go                 Model, Window, Journal, Phase, State, Step, Decision, Tools,
                                        Pending, Approvals, InputKind, Input, Inbox, EventKind, Event, Sink, Prompts
internal/agent/task.go                  Task, Status, Outcome
internal/agent/engine.go                Config, Engine, New, Run, step loop, settle, interrupted, tool-turn write
internal/agent/journal.go               history: in-memory transcript over the Journal port (single writer)
internal/agent/generate.go              streaming generation, bounded retry, step recording
internal/agent/request.go               request building, ToolUseAllowed, ImageInputAllowed, auto-compaction
internal/agent/tools.go                 tool batch, rejected calls, approval park/resume, steer merge
internal/agent/messages.go              ToolCallMessage, ToolResultMessage, ToolResultStatus, final reply parts
internal/agent/sanitize.go              assistant output sanitizer (moved)
internal/agent/agenttest/agenttest.go   ScriptedModel, ModelFunc, fakes for every port, Fixture
internal/agent/engine_test.go           engine behaviour tests
internal/agent/tools_internal_test.go   sameRequestedTool identity test (moved)
internal/agent/prompt/prompt.go         JoinSections, AssistantSystemPrompt, ProjectRoot
internal/agent/prompt/guidance.go       fixed guidance texts
internal/agent/prompt/plan.go           IsPlanRunPrompt
internal/agent/context/attachments.go   AttachmentReader, AttachmentData, ErrAttachmentUnavailable
internal/agent/context/conversation.go  provider conversation building
internal/agent/context/markers.go       compaction/clear markers, backoff
internal/agent/context/compact.go       summary request + Compact
internal/agent/context/estimate.go      token estimates, thresholds, recommendation
internal/agent/context/errors.go        IsContextLengthExceeded
internal/agent/context/conversation_test.go (moved)
internal/core/run_execute.go            ExecuteRun, prepareNativeRun, nativeEngine, resolveSessionRuntime, reschedule
internal/core/run_outcome.go            applyOutcome, applyInterruptedOutcome, completeAssistantTurn
internal/core/run_status.go             run status helpers shared with external agents
internal/core/agent_journal.go          coreJournal
internal/core/agent_tools.go            nativeTurn, coreTools, nativeToolSpecs, client capability checks
internal/core/agent_inbox.go            coreInbox, coreApprovals
internal/core/agent_sink.go             coreSink
internal/core/agent_prompts.go          corePrompts, nativeSystemPrompt and its helpers, prompt availability checks
internal/core/session_context.go        context API types, SessionContext, CompactSession, context report
internal/core/subagents_prompt.go       subagent runtime guidance texts
internal/core/native_run_characterization_test.go
internal/core/native_run_test.go        (renamed execution_generation_test.go)
deleted: internal/core/execution*.go, context*.go, assistant_sanitize.go, message_progress_test.go
```

---

### Task 0: Prerequisite audit (no commit)

**Files:** none modified.

- [ ] **Step 1: Confirm a green baseline with Stages 0–1b**

Run: `cd /root/projects/matrixclaw && git status --short && go build ./... && go vet ./... && go test ./... 2>&1 | tail -20`
Expected: clean tree (other sessions' files aside), all packages `ok`.

- [ ] **Step 2: Record the Stage 0/1 coupling points this plan relies on**

Run each and keep the output next to you while executing Tasks 8–10:

```bash
grep -n "type RunStep struct" -A16 internal/core/types_run.go
grep -n "func (c \*Core) recordRunStep\|func generationStopReason" -A25 internal/core/usage.go
grep -rn "recordRunStep\|compactSession(" internal/core/*.go | grep -v _test
grep -n "func insertMessage\|type sqlExecer\|insertMessage(" internal/store/*.go
grep -n "StopMaxTokens" internal/core/*.go
grep -n "Reasoning\b\|\.Reasoning\|RedactedData\|ReasoningBlock" internal/core/execution_*.go
grep -n "CacheKey\|MaxOutputTokens\|ToolChoice" internal/core/execution_*.go internal/core/context*.go
grep -n "ToolUseDisabled" internal/providers/*.go
git diff 6176876 -- internal/core/execution*.go internal/core/context*.go internal/core/usage.go | head -400
```

Expected (as the Stage 0, 1 and 1b plans leave the tree) and where each is used. If the output differs, stop and reconcile the earlier stage first; this plan does not carry alternatives.

| Coupling | State after Stages 0–1b | Used in this plan |
|---|---|---|
| `RunStep` fields | `RunID string, Step int, Model, Provider string, PromptTokens, CacheReadTokens, CacheWriteTokens, OutputTokens, ReasoningTokens int64, StopReason string, LatencyMillis int64, ToolCalls int, CreatedAt time.Time` (Stage 0 Task 5) | `recordRunStep(ctx, agent.Step)` (Task 9) fills exactly these. |
| step writes | `recordRunStep(ctx, runID, response, stopReason, latency)` (Stage 0 Tasks 5 and 7) called in `generateAssistantTurn` after every successful `Generate`, and in `generateCompactSummary` with `"compact"` (a no-op without a run ID); usage records are a `GROUP BY` over `run_steps` | Same call points in the engine: `run.generate`, `summaryModel` (Task 8). |
| `generationStopReason` | `return string(providers.ResolveStopReason(response.StopReason, len(response.ToolCalls)))` (Stage 1 Task 3) | Becomes `stepStopReason` (Task 8, Step 5). |
| `insertMessage` | `insertMessage(ctx, execer sqlExecer, message transcript.Message) error` with the `MAX(seq)+1` subquery and Stage 0's column list (Stage 0 Task 2) | Task 4 turns it into `RETURNING seq`. |
| max-tokens / content-filter conversion | `stopReasonError(response)` in `execution_generation.go`, called in `generateAssistantTurnWithRetry` right after `generateAssistantTurn` (`if err == nil { err = stopReasonError(response) }`) and in `generateCompactSummary` right after its `recordRunStep` (Stage 1 Task 3) | Moves to `agentcontext.StopReasonError` (Task 6); called at the same two positions in `run.generateWithRetry` (Task 8) and `agentcontext.summarize` (Task 6). |
| reasoning parts | `responseReasoningParts(response)` in `execution_generation.go`: a `ReasoningContent` part (if any) followed by one part per `Response.Reasoning` block, prepended to the tool-step reply's parts in `saveAssistantToolTurn`; `attachReasoningToToolCallMessage` is deleted; final replies keep only `ReasoningContent` via the unchanged `providerResponseMessageParts` (Stage 1 Task 4) | `reasoningParts` and `finalReply` in `agent/messages.go` (Task 7), used by `run.finishToolTurn` (Task 8). |
| request fields | `buildProviderRequest` sets `CacheKey: turn.SessionID` (Stage 1 Task 3); `generateCompactSummary` sets no new field | `run.buildRequest` sets `CacheKey`; `agentcontext.summarize` sets `SessionID`, `SystemPrompt`, `Messages` as today. |
| `providers.ToolUseDisabled` | still exists; Stage 1b only removes it as the Anthropic default | `ToolUseAllowed` and the text-only branch keep it. |
| Stage 1 conversation tests | `internal/core/tool_step_conversation_test.go` (package `core`, calls `buildProviderConversationWithAttachmentsForRun`) | Moved with the conversation builder in Task 6. |

Every other Stage 0/1 hunk in `execution*.go`/`context*.go` travels with the verbatim moves.

---

### Task 1: Characterization tests — tool round, events, checkpoints, step cap

**Files:**
- Create: `internal/core/native_run_characterization_test.go`

These tests must pass on the **current** loop and stay unchanged until the end of the stage.

- [ ] **Step 1: Write the tests**

```go
package core_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type funcTool struct {
	spec tools.Spec
	fn   func(context.Context, tools.Call) (tools.Result, error)
}

func (t funcTool) Spec() tools.Spec { return t.spec }
func (t funcTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	return t.fn(ctx, call)
}

type windowLLMs struct {
	recoveryLLMs
	window int
}

func (w windowLLMs) ContextWindowTokens(string, string) (int, bool) { return w.window, true }

func drainEvents(events <-chan core.Event) []core.Event {
	var out []core.Event
	for {
		select {
		case event := <-events:
			out = append(out, event)
		default:
			return out
		}
	}
}

func describeEvent(event core.Event) string {
	switch payload := event.Payload.(type) {
	case transcript.Message:
		kinds := make([]string, 0, len(payload.Parts))
		for _, part := range payload.Parts {
			kind := string(part.Kind)
			if part.ToolCall != nil && part.ToolCall.Finished {
				kind += "(done)"
			}
			kinds = append(kinds, kind)
		}
		return fmt.Sprintf("%s %s/%s", event.Type, payload.Role, strings.Join(kinds, ","))
	case core.ToolUpdate:
		return fmt.Sprintf("%s %s", event.Type, payload.State)
	case core.Run:
		return fmt.Sprintf("%s %s", event.Type, payload.Status)
	default:
		return string(event.Type)
	}
}

func sessionMessages(t *testing.T, db *store.SQLiteStore, sessionID string) []transcript.Message {
	t.Helper()
	messages, err := db.ListMessages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestNativeToolRoundPublishesEventsInOrder(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	now := runRecoveryTestTime()
	app.WithClock(func() time.Time { return now })
	app.WithTools(tools.NewRegistry(&recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}))
	usage := providers.Usage{PromptTokens: 3, OutputTokens: 1}
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			if err := providers.StreamText(ctx, "Checking"); err != nil {
				return providers.Response{}, err
			}
			return providers.Response{Text: "Checking.", Usage: usage, ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		if err := providers.StreamText(ctx, "Done"); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{Text: "Done.", Usage: usage}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "events", core.RunStatusAccepted, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := app.SubscribeEvents(ctx, session.ID)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	var trace []string
	for _, event := range drainEvents(events) {
		switch event.Type {
		case core.EventRunUpdated, core.EventMessageCreated, core.EventMessageUpdated, core.EventToolUpdated:
			trace = append(trace, describeEvent(event))
		}
	}
	want := []string{
		"run.updated running",
		"message.created assistant/text",
		"message.updated assistant/text,finish",
		"message.created assistant/tool_call",
		"tool.updated requested",
		"message.updated assistant/tool_call(done)",
		"message.created tool/tool_result",
		"tool.updated completed",
		"message.created assistant/text",
		"message.updated assistant/text,finish",
		"run.updated completed",
	}
	if got := strings.Join(trace, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("event trace:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

func TestNativeRunCheckpointsModelAndToolPhases(t *testing.T) {
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
		seen = append(seen, fmt.Sprintf("%s:%s:%s:%s", label, checkpoint.Phase, checkpoint.ToolCallID, checkpoint.ToolName))
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

	want := []string{"model1:model::", "tool:tool:call-1:inspect_state", "model2:model::"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("checkpoints = %v, want %v", seen, want)
	}
	waitForRecoveryCheckpointGone(t, db, run.ID)
}

func TestNativeRunFailsAfterThirtyTwoToolSteps(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("step-%d", calls), Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "step-cap", core.RunStatusAccepted, false)

	err := app.ExecuteRun(context.Background(), run.ID)

	if err == nil || err.Error() != "tool loop exceeded 32 steps" {
		t.Fatalf("ExecuteRun error = %v", err)
	}
	got, getErr := db.GetRun(context.Background(), run.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.Status != core.RunStatusFailed || got.Error != "tool loop exceeded 32 steps" {
		t.Fatalf("run = %s (%s)", got.Status, got.Error)
	}
	if calls != 32 || tool.callCount() != 32 {
		t.Fatalf("model calls=%d tool calls=%d, want 32/32", calls, tool.callCount())
	}
}
```

- [ ] **Step 2: Run them against the current loop**

Run: `go test ./internal/core/ -run 'TestNativeToolRoundPublishesEventsInOrder|TestNativeRunCheckpointsModelAndToolPhases|TestNativeRunFailsAfterThirtyTwoToolSteps' -count=1 -v`
Expected: PASS. If the event trace differs, the test is wrong, not the code: fix the expectation to what `main` does now (it pins today's behaviour).

- [ ] **Step 3: Commit**

```bash
git add internal/core/native_run_characterization_test.go
git commit -m "test(core): pin native tool round, events, checkpoints and step cap

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Characterization tests — approvals, steer, cancel during a tool

**Files:**
- Modify: `internal/core/native_run_characterization_test.go` (append)

- [ ] **Step 1: Append the tests** (add `"errors"` to the imports)

```go
func approvalTools(mutations *int) (funcTool, *recoveryTool) {
	mutate := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		if !call.Approved {
			return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "mutate_state", ToolCallID: call.ToolCallID, Action: "write_state", Description: "write the state"}}, nil
		}
		*mutations++
		return tools.Result{Content: "mutated"}, nil
	}}
	return mutate, &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
}

func TestNativeRunParksForApprovalAndResumesAfterGrant(t *testing.T) {
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
		results := map[string]string{}
		for _, message := range request.Messages {
			if message.Role == "tool" {
				results[message.ToolCallID] = message.Content
			}
		}
		resumedWithBothResults = results["call-mutate"] == "mutated" && results["call-inspect"] == "recovered tool result"
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "approval", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	assertToolResultCount(t, db, session.ID, "call-inspect", 1)
	assertToolResultCount(t, db, session.ID, "call-mutate", 0)
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || approvals[0].ToolCallRef != "call-mutate" || approvals[0].Action != "write_state" {
		t.Fatalf("pending approvals = %#v", approvals)
	}
	if calls != 1 || mutations != 0 || inspect.callCount() != 1 {
		t.Fatalf("before grant: model=%d mutations=%d inspect=%d", calls, mutations, inspect.callCount())
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, db, session.ID, "call-mutate", 1)
	if calls != 2 || mutations != 1 || !resumedWithBothResults {
		t.Fatalf("after grant: model=%d mutations=%d both results=%v", calls, mutations, resumedWithBothResults)
	}
}

func TestNativeRunFailsWhenApprovalIsDenied(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, inspect := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, inspect))
	app.WithRunStarter(&recordingRunStarter{})
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "denied", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("pending approvals = %#v, err = %v", approvals, err)
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, false); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.RunStatusFailed || got.Error != "approval denied" || mutations != 0 {
		t.Fatalf("run = %s (%s), mutations = %d", got.Status, got.Error, mutations)
	}
}

func TestSteerDuringToolIsAppendedToThatToolResult(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	started := make(chan struct{})
	release := make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		close(started)
		select {
		case <-release:
			return tools.Result{Content: "inspected"}, nil
		case <-ctx.Done():
			return tools.Result{}, ctx.Err()
		}
	}}))
	calls := 0
	var sawGuidance bool
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		for _, message := range request.Messages {
			sawGuidance = sawGuidance || (message.ToolCallID == "call-1" && strings.Contains(message.Content, "User guidance: focus on logs"))
		}
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "steer", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, started, "tool start")

	result, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Text: "focus on logs", BusyMode: core.BusyInputModeSteer})
	if err != nil || result.Status != core.AcceptRunStatusSteered {
		t.Fatalf("AcceptRun = %#v, %v", result, err)
	}
	close(release)
	if err := waitRecoveryError(t, done, "steered run"); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if !sawGuidance {
		t.Fatal("next model request lacks the steer guidance in the tool result")
	}
	found := false
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleTool && message.Content == "inspected\n\nUser guidance: focus on logs" {
			found = true
		}
	}
	if !found {
		t.Fatal("stored tool result does not carry the steer guidance")
	}
	pending, err := db.ListPendingSessionInputs(context.Background(), session.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending inputs = %#v, err = %v", pending, err)
	}
}

func TestCancelDuringToolStopsRunWithoutAnotherModelCall(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	started := make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		close(started)
		<-ctx.Done()
		return tools.Result{}, ctx.Err()
	}}))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cancel-tool", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, started, "tool start")

	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled run"); err != nil {
		t.Fatalf("ExecuteRun after cancel: %v", err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
	sealed := false
	for _, message := range sessionMessages(t, db, session.ID) {
		sealed = sealed || (message.RunID == run.ID && message.Role == transcript.MessageRoleAssistant && messageHasFinishPart(message, "canceled"))
	}
	if !sealed {
		t.Fatal("tool-turn assistant message was not sealed as canceled")
	}
	if _, err := db.GetRunCheckpoint(context.Background(), run.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("canceled run checkpoint error = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run against the current loop**

Run: `go test ./internal/core/ -run 'Approval|Steer|CancelDuringTool' -count=1 -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/core/native_run_characterization_test.go
git commit -m "test(core): pin approval park/resume, denial, steer and tool cancel

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Characterization tests — compaction, context overflow, max tokens, subagents

**Files:**
- Modify: `internal/core/native_run_characterization_test.go` (append; add `"regexp"`, `"sync"` and `"github.com/Suren878/matrixclaw/internal/orchestration"` to imports)

- [ ] **Step 1: Append the tests**

```go
var compactMarkerPattern = regexp.MustCompile("^🧠 Context compacted: ~[0-9.]+[kM]? -> ~[0-9.]+[kM]? tokens\n\nSUMMARY$")

func saveNativeRunWithHistory(t *testing.T, db *store.SQLiteStore, suffix string, history ...transcript.Message) (core.Session, core.Run) {
	t.Helper()
	ctx := context.Background()
	now := runRecoveryTestTime()
	session := core.Session{
		ID: "session_" + suffix, Title: suffix, Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw,
		ProviderID: "recovery-test", ModelID: "test-model", PermissionMode: core.PermissionModeDefault,
		Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	for i, message := range history {
		message.SessionID = session.ID
		message.CreatedAt = now.Add(time.Duration(i-len(history)) * time.Minute)
		message.UpdatedAt = message.CreatedAt
		saveRunRecoveryTestMessage(t, db, message)
	}
	run := core.Run{ID: "run_" + suffix, SessionID: session.ID, UserMessageID: "msg_user_" + suffix, Status: core.RunStatusAccepted, StartedAt: now, UpdatedAt: now}
	user := transcript.Message{
		ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser,
		Content: "original task " + suffix, Parts: transcript.NormalizeMessageParts("original task "+suffix, nil),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.AcceptMessage(ctx, user, run); err != nil {
		t.Fatal(err)
	}
	return session, run
}

func countCompactMarkers(t *testing.T, db *store.SQLiteStore, sessionID string) int {
	t.Helper()
	count := 0
	for _, message := range sessionMessages(t, db, sessionID) {
		if message.Role == transcript.MessageRoleSystem && message.RunID == "" && compactMarkerPattern.MatchString(message.Content) {
			count++
		}
	}
	return count
}

func TestNativeRunCompactsLargeHistoryBeforeTheModelCall(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	big := strings.Repeat("x", 330_000)
	var summaryRequests, mainRequests int
	var mainPrompt string
	var leaked bool
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "You compact matrixclaw chat histories") {
			summaryRequests++
			return providers.Response{Text: "SUMMARY"}, nil
		}
		mainRequests++
		mainPrompt = request.SystemPrompt
		for _, message := range request.Messages {
			leaked = leaked || strings.Contains(message.Content, big[:1000])
		}
		return providers.Response{Text: "Done."}, nil
	})}})
	session, run := saveNativeRunWithHistory(t, db, "compact", transcript.Message{
		ID: "msg_old_user", Role: transcript.MessageRoleUser, Content: big, Parts: transcript.NormalizeMessageParts(big, nil),
	})

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if summaryRequests != 1 || mainRequests != 1 {
		t.Fatalf("summary=%d main=%d, want 1/1", summaryRequests, mainRequests)
	}
	if !strings.Contains(mainPrompt, "Session context summary:\nSUMMARY") || leaked {
		t.Fatalf("main request: summary in prompt=%v, old history leaked=%v", strings.Contains(mainPrompt, "SUMMARY"), leaked)
	}
	if got := countCompactMarkers(t, db, session.ID); got != 1 {
		t.Fatalf("compact markers = %d, want 1", got)
	}
}

func TestContextLengthErrorForcesCompactionAndRetriesOnce(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var summaryRequests, mainRequests int
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "You compact matrixclaw chat histories") {
			summaryRequests++
			return providers.Response{Text: "SUMMARY"}, nil
		}
		mainRequests++
		if mainRequests == 1 {
			return providers.Response{}, errors.New("provider: context_length_exceeded")
		}
		return providers.Response{Text: "Recovered."}, nil
	})}})
	session, run := saveNativeRunWithHistory(t, db, "overflow")

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if summaryRequests != 1 || mainRequests != 2 {
		t.Fatalf("summary=%d main=%d, want 1/2", summaryRequests, mainRequests)
	}
	if got := countCompactMarkers(t, db, session.ID); got != 1 {
		t.Fatalf("compact markers = %d, want 1", got)
	}
}

func TestMaxOutputStopFailsTheRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "cut", StopReason: providers.StopMaxTokens}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "max-tokens", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err == nil {
		t.Fatal("ExecuteRun error = nil, want the max-output error")
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusFailed)
}

func TestBlockingSubagentReturnsChildSummaryToParent(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	parentCalls := 0
	var delegateResult string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child found 3 files"}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: []byte(`{"goal":"count files","runtime":"matrixclaw"}`)}}}, nil
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
	if task.Status != core.SubagentTaskStatusCompleted || task.Summary != "child found 3 files" {
		t.Fatalf("task = %s %q", task.Status, task.Summary)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCompleted)
}

type asyncSubagentScenario struct {
	db      *store.SQLiteStore
	session core.Session
	run     core.Run
	events  []core.Event
}

func runAsyncSubagentScenario(t *testing.T) asyncSubagentScenario {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
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
			if message.Role == "user" && strings.HasPrefix(message.Content, "Subagent ") && strings.Contains(message.Content, "completed.") {
				return providers.Response{Text: "Synthesized."}, nil
			}
		}
		if !spawned {
			spawned = true
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "spawn_subagent", Arguments: []byte(`{"name":"Scanner","goal":"scan the tree","runtime":"matrixclaw"}`)}}}, nil
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
		followUp := ""
		for _, message := range sessionMessages(t, db, session.ID) {
			if message.Role == transcript.MessageRoleUser && strings.HasPrefix(message.Content, "Subagent ") && strings.Contains(message.Content, "completed.") {
				followUp = message.RunID
			}
		}
		if followUp != "" {
			if got, err := db.GetRun(context.Background(), followUp); err == nil && got.Status == core.RunStatusCompleted {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("subagent completion follow-up run did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return asyncSubagentScenario{db: db, session: session, run: run, events: drainEvents(events)}
}

func TestAsyncSubagentCompletionStartsParentFollowUpRun(t *testing.T) {
	scenario := runAsyncSubagentScenario(t)

	task, err := scenario.db.GetSubagentTaskByParentToolCall(context.Background(), scenario.session.ID, scenario.run.ID, "call-spawn")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.SubagentTaskStatusCompleted || task.CompletionDeliveredAt == nil {
		t.Fatalf("task = %s delivered=%v", task.Status, task.CompletionDeliveredAt)
	}
}
```

- [ ] **Step 2: Run against the current loop**

Run: `go test ./internal/core/ -run 'Compact|ContextLength|MaxOutput|Subagent' -count=1 -v`
Expected: PASS. `TestMaxOutputStopFailsTheRun` relies on Stage 1's conversion of `StopMaxTokens` into today's error.

- [ ] **Step 3: Run the whole core suite three times to catch flakiness**

Run: `go test ./internal/core/ -count=3`
Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
git add internal/core/native_run_characterization_test.go
git commit -m "test(core): pin compaction, overflow retry, max tokens and subagents

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Leaf helpers and seq-returning message inserts

**Files:**
- Create: `internal/transcript/finish.go`, `internal/providers/images.go`, `internal/providers/images_test.go`, `internal/store/sqlite_append_test.go`
- Modify: `internal/core/run_recovery.go`, `internal/core/execution_conversation.go`, `internal/core/execution_conversation_test.go`, `clients/telegram/messages.go`, `internal/store/sqlite_messages.go` (and every other `insertMessage` caller in `internal/store`), `internal/core/ports.go`, `internal/core/message_progress.go`, `internal/core/message_progress_test.go`, `internal/core/execution_turn.go`, `internal/core/external_agent_execution.go`

Two commits: leaf helpers, then the store.

- [ ] **Step 1: Move the image MIME test first so it fails**

Create `internal/providers/images_test.go` with the body of `TestIsProviderSupportedImageMIMEType` from `internal/core/execution_conversation_test.go`, renamed:

```go
package providers

import "testing"

func TestIsSupportedImageMIMEType(t *testing.T) {
	tests := map[string]bool{
		"image/jpeg":                true,
		"image/png; charset=binary": true,
		"image/gif":                 true,
		"image/webp":                true,
		"image/svg+xml":             false,
		"image/tiff":                false,
		"":                          false,
	}
	for mimeType, want := range tests {
		if got := IsSupportedImageMIMEType(mimeType); got != want {
			t.Errorf("IsSupportedImageMIMEType(%q) = %t, want %t", mimeType, got, want)
		}
	}
}
```

Delete `TestIsProviderSupportedImageMIMEType` from `internal/core/execution_conversation_test.go`.

Run: `go test ./internal/providers/ -run TestIsSupportedImageMIMEType`
Expected: FAIL, `undefined: IsSupportedImageMIMEType`.

- [ ] **Step 2: Move the function**

Create `internal/providers/images.go` (`package providers`, import `strings`) and move `IsProviderSupportedImageMIMEType` verbatim from `internal/core/execution_conversation.go` (6176876: 400-415), renamed `IsSupportedImageMIMEType`, with the doc comment:

```go
// IsSupportedImageMIMEType reports whether image data can be sent inline to every
// image-capable provider. Other image/* files (for example SVG) stay attachment
// references only.
```

Replace the two calls in `toProviderMessages` (`internal/core/execution_conversation.go`) with `providers.IsSupportedImageMIMEType`, and in `clients/telegram/messages.go` replace `core.IsProviderSupportedImageMIMEType(doc.MIMEType)` with `providers.IsSupportedImageMIMEType(doc.MIMEType)` (add the `internal/providers` import; drop `core` if it becomes unused).

- [ ] **Step 3: Move the finish-reason check to transcript**

Create `internal/transcript/finish.go`:

```go
package transcript

import "strings"

// HasFinishReason reports whether the message has a finish part with the reason;
// an empty reason matches any finish part.
func HasFinishReason(message Message, reason string) bool {
	reason = strings.TrimSpace(reason)
	for _, part := range message.Parts {
		if part.Finish == nil {
			continue
		}
		if reason == "" || strings.TrimSpace(part.Finish.Reason) == reason {
			return true
		}
	}
	return false
}
```

In `internal/core/run_recovery.go` delete `messageHasFinishReason` and replace its three uses with `transcript.HasFinishReason` (same arguments).

- [ ] **Step 4: Build, test, commit the leaf helpers**

Run: `gofmt -l internal clients; go build ./... && go vet ./... && go test ./...`
Expected: all `ok`.

```bash
git add internal/transcript/finish.go internal/providers/images.go internal/providers/images_test.go \
  internal/core/run_recovery.go internal/core/execution_conversation.go internal/core/execution_conversation_test.go \
  clients/telegram/messages.go
git commit -m "refactor: move finish-reason and image MIME checks to leaf packages

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 5: Write the failing store test** — `internal/store/sqlite_append_test.go`

```go
package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestMessageInsertsReturnTheirSeq(t *testing.T) {
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if err := st.CreateSession(ctx, core.Session{
		ID: "s1", Title: "s1", Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw,
		PermissionMode: core.PermissionModeDefault, Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	message := func(id string) transcript.Message {
		return transcript.Message{ID: id, SessionID: "s1", Role: transcript.MessageRoleUser, Content: id, CreatedAt: now, UpdatedAt: now}
	}

	first, err := st.AppendMessage(ctx, message("m1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.SaveMessageProgress(ctx, message("m2"))
	if err != nil {
		t.Fatal(err)
	}

	if first <= 0 || second != first+1 {
		t.Fatalf("seqs = %d, %d; want consecutive positive", first, second)
	}
	stored, err := st.GetMessage(ctx, "m2")
	if err != nil || stored.Seq != second {
		t.Fatalf("stored m2 = %+v, err = %v", stored, err)
	}
}
```

Run: `go test ./internal/store/ -run TestMessageInsertsReturnTheirSeq`
Expected: FAIL, `st.AppendMessage undefined` and `assignment mismatch: 2 variables but st.SaveMessageProgress returns 1 value`.

- [ ] **Step 6: Return seq from inserts**

In `internal/store/sqlite_messages.go`, change `insertMessage` to return the seq (the column and argument lists are Stage 0 Task 2's; only the signature, `RETURNING seq` and `Scan` change):

```go
// sqlRowQueryer is satisfied by *sql.DB and *sql.Tx.
type sqlRowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// insertMessage assigns the next database-wide seq in the same statement and returns it.
func insertMessage(ctx context.Context, queryer sqlRowQueryer, message transcript.Message) (int64, error) {
	var seq int64
	err := queryer.QueryRowContext(ctx, `
INSERT INTO messages(id, session_id, run_id, role, content, parts_json, model, provider, created_at, updated_at, seq)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages))
RETURNING seq`,
		message.ID,
		message.SessionID,
		message.RunID,
		string(message.Role),
		message.Content,
		marshalMessageParts(message),
		message.Model,
		message.Provider,
		formatTime(message.CreatedAt),
		formatTime(messageUpdatedAt(message)),
	).Scan(&seq)
	return seq, err
}
```

Replace `SaveMessage` and `SaveMessageProgress` and add `AppendMessage`:

```go
func (s *SQLiteStore) SaveMessage(ctx context.Context, message transcript.Message) error {
	_, err := s.AppendMessage(ctx, message)
	return err
}

// AppendMessage stores a message, indexes it for search and returns its seq.
func (s *SQLiteStore) AppendMessage(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := insertMessage(ctx, s.db, message)
	if err != nil {
		return 0, fmt.Errorf("store: save message: %w", err)
	}
	_ = upsertMessageSearch(ctx, s.db, message)
	return seq, nil
}

// SaveMessageProgress stores a streaming snapshot without search indexing and returns its seq.
func (s *SQLiteStore) SaveMessageProgress(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := insertMessage(ctx, s.db, message)
	if err != nil {
		return 0, fmt.Errorf("store: save message progress: %w", err)
	}
	return seq, nil
}
```

The other two `insertMessage` callers, `CompleteRun` and `AcceptMessage` (same file, both pass a `*sql.Tx`), become `if _, err := insertMessage(ctx, tx, …); err != nil {` with the same error handling as before; `grep -n "insertMessage(" internal/store/*.go` must show no other caller. `sqlExecer` stays: `insertRun`, `updateRun`, `touchSession`, `insertClientDelivery` and `upsertMessageSearch` still take it.

- [ ] **Step 7: Core interfaces and callers**

`internal/core/ports.go`, `MessageStore`: add `AppendMessage(ctx context.Context, message transcript.Message) (int64, error)` after `SaveMessage`.

`internal/core/message_progress.go`:

```go
// MessageProgressStore is an optional fast path for transient streaming
// snapshots. Final message writes still use MessageStore so search indexes and
// other durable projections are updated exactly once at a turn boundary.
type MessageProgressStore interface {
	SaveMessageProgress(ctx context.Context, message transcript.Message) (int64, error)
	UpdateMessageProgress(ctx context.Context, message transcript.Message) error
}

func (c *Core) saveMessageProgress(ctx context.Context, message transcript.Message) (int64, error) {
	if progressStore, ok := c.store.(MessageProgressStore); ok {
		return progressStore.SaveMessageProgress(ctx, message)
	}
	return c.store.AppendMessage(ctx, message)
}
```

(`updateMessageProgress` unchanged.) Callers of `c.saveMessageProgress` in `internal/core/execution_turn.go` (`generateAssistantTurn`) and `internal/core/external_agent_execution.go` become `if _, err := c.saveMessageProgress(ctx, …); err != nil {`. In `internal/core/message_progress_test.go` change the fake's method to:

```go
func (s *progressCountingStore) SaveMessageProgress(_ context.Context, message transcript.Message) (int64, error) {
	s.progressSaves++
	s.message = message
	return int64(s.progressSaves), nil
}
```

(Stage 0 Task 1 already qualified the file's message types as `transcript.Message` and added the import.)

- [ ] **Step 8: Build, test, commit the store change**

Run: `gofmt -l internal; go build ./... && go vet ./... && go test ./...`
Expected: all `ok`, including `TestMessageInsertsReturnTheirSeq`.

```bash
git add internal/store/sqlite_messages.go internal/store/sqlite_append_test.go internal/core/ports.go \
  internal/core/message_progress.go internal/core/message_progress_test.go internal/core/execution_turn.go \
  internal/core/external_agent_execution.go
git commit -m "feat(store): return seq from message inserts

insertMessage uses INSERT ... RETURNING seq; AppendMessage and
SaveMessageProgress hand the seq back for the engine journal.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `internal/agent/prompt` and core prompt helpers

**Files:**
- Create: `internal/agent/prompt/prompt.go`, `internal/agent/prompt/guidance.go`, `internal/agent/prompt/plan.go`, `internal/core/agent_prompts.go`, `internal/core/subagents_prompt.go`
- Modify: `internal/core/execution_request.go`, `internal/core/context_report.go`, `internal/core/execution_conversation.go`, `internal/core/context_compact.go`, `internal/core/plan.go`, `clients/terminal/chat/runtime/app_usage.go`, `clients/terminal/chat/viewmodel/surface_adapter.go`
- Delete: `internal/core/execution_prompts.go`

Pure move; the characterization tests (which assert "daemon restarted" and "Subagent mode:" in prompts) and the full suite are the tests.

- [ ] **Step 1: Create `internal/agent/prompt/prompt.go`**

```go
// Package prompt holds the fixed texts of the native agent's system prompt.
package prompt

import (
	"fmt"
	"strings"
)

// JoinSections joins non-empty prompt sections with blank lines.
func JoinSections(sections ...string) string {
	values := make([]string, 0, len(sections))
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			values = append(values, section)
		}
	}
	return strings.Join(values, "\n\n")
}

// AssistantSystemPrompt is the configured identity and system prompt plus language guidance.
func AssistantSystemPrompt(name string, systemPrompt string) string {
	name = strings.Join(strings.Fields(name), " ")
	systemPrompt = strings.TrimSpace(systemPrompt)
	languageGuidance := languageGuidance()
	if name == "" {
		return JoinSections(systemPrompt, languageGuidance)
	}
	identity := fmt.Sprintf("Assistant identity:\n- Your configured assistant name is %q. Use this exact name when asked who you are. If older/default instructions mention a different assistant name, this configured name takes precedence.", name)
	if systemPrompt == "" {
		return JoinSections(identity, languageGuidance)
	}
	return JoinSections(identity, systemPrompt, languageGuidance)
}
```

Then move verbatim from `internal/core/execution_prompts.go`: `responseLanguageGuidancePrompt` → unexported `languageGuidance`; `currentProjectRootPrompt` → `ProjectRoot` with doc comment `// ProjectRoot tells the model which directory relative tool paths resolve against.`

- [ ] **Step 2: Create `internal/agent/prompt/guidance.go`**

`package prompt`, import `strings`. Move verbatim (bodies unchanged, only names) with one-line doc comments:
- `toolUseDisciplinePrompt` → `ToolUseDiscipline` (`// ToolUseDiscipline is the tool-use guidance for models that can call tools.`)
- `webResearchGuidancePrompt` → `WebResearchGuidance`
- `voiceOutputGuidancePrompt` → `VoiceOutputGuidance`
- `fileDeliveryGuidancePrompt` → `FileDeliveryGuidance`
- `telephonyCallGuidancePrompt` → `TelephonyCallGuidance`

- [ ] **Step 3: Create `internal/agent/prompt/plan.go`**

```go
package prompt

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// IsPlanRunPrompt reports whether message is an internal plan runner prompt; such
// prompts stay in history for audit but the provider already sees the plan.
func IsPlanRunPrompt(message transcript.Message) bool {
	if message.Role != transcript.MessageRoleUser {
		return false
	}
	content := strings.TrimSpace(message.Content)
	return strings.HasPrefix(content, "Execute the current session plan.") ||
		strings.HasPrefix(content, "Execute the next session plan item.") ||
		strings.HasPrefix(content, "The session plan was updated.")
}
```

Delete `IsPlanRunPromptMessage` from `internal/core/execution_conversation.go`; replace its callers: `skipInternalPlanPromptForProvider` (same file) and `compactMessageGroups` (`internal/core/context_compact.go`) with `prompt.IsPlanRunPrompt`, and `clients/terminal/chat/viewmodel/surface_adapter.go` `core.IsPlanRunPromptMessage(message)` with `prompt.IsPlanRunPrompt(message)`.

- [ ] **Step 4: Move the core-state helpers**

- Create `internal/core/agent_prompts.go` (`package core`) and move verbatim `webResearchPromptAvailable`, `fileDeliveryPromptAvailable`, `telephonyCallPromptAvailable`, `delegateTaskPromptAvailable`.
- Create `internal/core/subagents_prompt.go` (`package core`, imports `context`, `fmt`, `strings`) and move verbatim `delegateTaskGuidancePrompt`, `delegateTaskToolDescription`, type `subagentRuntimeInfo` with `PromptLine`, method `(c *Core) subagentRuntimeInfo`, `subagentRuntimeAlias`, `subagentRuntimeDetail`, `normalizeModelNames`, `availableSubagentRuntimeIDs` (6176876: `execution_prompts.go:118-279`).
- Append `sessionPlanPrompt` verbatim to `internal/core/plan.go`.
- `git rm internal/core/execution_prompts.go`.

- [ ] **Step 5: Update callers**

In `internal/core/execution_request.go` `providerSystemPrompt`: `AssistantSystemPrompt(assistant)` → `prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)`; `subagent…`/availability calls unchanged; `currentProjectRootPrompt(x)` → `prompt.ProjectRoot(x)`; `voiceOutputGuidancePrompt()` → `prompt.VoiceOutputGuidance()`; `fileDeliveryGuidancePrompt()` → `prompt.FileDeliveryGuidance()`; `telephonyCallGuidancePrompt()` → `prompt.TelephonyCallGuidance()`; `toolUseDisciplinePrompt()` → `prompt.ToolUseDiscipline()`; `webResearchGuidancePrompt()` → `prompt.WebResearchGuidance()`; `joinPromptSections(` → `prompt.JoinSections(`.
In `internal/core/context_report.go` `contextReport`: `AssistantSystemPrompt(assistant)` → `prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)`.
In `clients/terminal/chat/runtime/app_usage.go` `assistantPromptTokens`: `core.AssistantSystemPrompt(assistant)` → `prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)`.
Import `"github.com/Suren878/matrixclaw/internal/agent/prompt"` where used.

- [ ] **Step 6: Build and test**

Run: `gofmt -l internal clients; go build ./... && go vet ./... && go test ./...`
Expected: all `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/prompt/prompt.go internal/agent/prompt/guidance.go internal/agent/prompt/plan.go \
  internal/core/agent_prompts.go internal/core/subagents_prompt.go \
  internal/core/execution_request.go internal/core/context_report.go internal/core/execution_conversation.go \
  internal/core/context_compact.go internal/core/plan.go \
  clients/terminal/chat/runtime/app_usage.go clients/terminal/chat/viewmodel/surface_adapter.go
git commit -m "refactor(agent): move fixed prompt texts to internal/agent/prompt

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `internal/agent/context` (package `agentcontext`) and core's context API

**Files:**
- Create: `internal/agent/context/attachments.go`, `conversation.go`, `markers.go`, `compact.go`, `estimate.go`, `errors.go`; `internal/core/session_context.go`, `internal/core/execution_compaction.go`
- Move: `internal/core/execution_conversation_test.go` → `internal/agent/context/conversation_test.go`; `internal/core/tool_step_conversation_test.go` (Stage 1 Task 4) → `internal/agent/context/tool_step_conversation_test.go`
- Modify: `internal/core/core.go`, `internal/core/execution.go`, `internal/core/execution_request.go`, `internal/core/execution_generation.go`, `internal/core/plan_tools.go`, `internal/core/run_recovery.go`, `internal/daemoncmd/run_helpers.go`, `internal/controlplane/context.go`, `clients/terminal/chat/runtime/app_usage.go`
- Delete: `internal/core/context.go`, `context_compact.go`, `context_errors.go`, `context_markers.go`, `context_report.go`, `execution_conversation.go`

- [ ] **Step 1: Move the conversation test first**

```bash
mkdir -p internal/agent/context
git mv internal/core/execution_conversation_test.go internal/agent/context/conversation_test.go
git mv internal/core/tool_step_conversation_test.go internal/agent/context/tool_step_conversation_test.go
```

Edit `conversation_test.go`: `package agentcontext`; import `transcript`; `Message` → `transcript.Message`, `MessageRole*` → `transcript.MessageRole*`, `MessagePart`/`MessagePartKind*`/`ImagePart`/`ToolCallPart`/`ToolResultPart` → `transcript.` equivalents (Stage 0 Task 1 may already have qualified them); `buildProviderConversationWithAttachmentsForRun(` → `Conversation(`.

Edit `tool_step_conversation_test.go` (Stage 1's `TestToolStepIsReplayedAsOneAssistantMessage` and `TestPlainReasoningOnOlderToolCallsStaysReasoningContent`, already written with `transcript.` types): `package core` → `package agentcontext`; both `buildProviderConversationWithAttachmentsForRun(` → `Conversation(`. Its helpers `stepCallMessage`/`stepResultMessage` do not collide with `conversation_test.go`.

Run: `go test ./internal/agent/context/`
Expected: FAIL (`undefined: toProviderMessages`, `Conversation`, …).

- [ ] **Step 2: `attachments.go`**

```go
// Package agentcontext builds the model's view of a session: provider conversation,
// compaction markers and summaries, and token estimates.
package agentcontext
```

then move verbatim from `internal/core/core.go`: `AttachmentData`, `ErrAttachmentUnavailable` (with its comment), `AttachmentReader` (imports `context`, `errors`). Delete them from `core.go`; change `Core.attachments` to `agentcontext.AttachmentReader` and `WithAttachmentReader(reader agentcontext.AttachmentReader)`. In `internal/daemoncmd/run_helpers.go` replace `core.AttachmentData` → `agentcontext.AttachmentData` and `core.ErrAttachmentUnavailable` → `agentcontext.ErrAttachmentUnavailable`.

- [ ] **Step 3: `conversation.go`**

Move the whole of `internal/core/execution_conversation.go` verbatim, then:
- `package agentcontext`; `Message`/part types/roles → `transcript.` equivalents.
- delete the method `(c *Core) buildProviderConversation`;
- `buildProviderConversationWithAttachmentsForRun` → `Conversation`, doc: `// Conversation converts history into provider messages, pairing every tool call with its result.`
- `buildTextOnlyProviderConversationForRun` → `TextOnlyConversation`, doc: `// TextOnlyConversation renders history as plain text for models without tool calling.`
- `messageInterruptedByDaemonRestart(message)` → `transcript.HasFinishReason(message, restartFinishReason)` and add `const restartFinishReason = "daemon_restart"`;
- `prompt.IsPlanRunPrompt` and `providers.IsSupportedImageMIMEType` stay as set in Tasks 4–5.

Delete `messageInterruptedByDaemonRestart` from `internal/core/run_recovery.go`. In `internal/core/execution_request.go` `buildProviderRequest`: `buildTextOnlyProviderConversationForRun(` → `agentcontext.TextOnlyConversation(`, and `c.buildProviderConversation(ctx, effectiveHistory, turn.RunID, runtimeImageInputAllowed(turn.Runtime))` → `agentcontext.Conversation(ctx, effectiveHistory, c.attachments, turn.RunID, runtimeImageInputAllowed(turn.Runtime))`.

- [ ] **Step 4: `markers.go`**

Move verbatim from `internal/core/context.go` the consts `compactMessagePrefix`, `contextClearedMessagePrefix`, `contextClearedSummary`, `compactBackoffMinimumSavingsPercent`, and from `context_markers.go` `compactMarkerSummary`, `clearMarkerSummary`, `compactStatsPattern`, `parseCompactStats`, `parseShortTokenNumber`; `latestCompactSummaryForRun` → `LatestSummaryForRun` (doc: `// LatestSummaryForRun is LatestSummary but keeps the run's own messages written before a compaction marker.`), `compactBackoffActive` → `CompactBackoffActive` (doc: `// CompactBackoffActive reports whether the last two compactions each saved under 10%.`), types → `transcript.`. Replace `contextMarker`/`latestContextMarker`/`latestCompactSummary` with:

```go
// Marker is the newest compaction or clear marker and the history after it.
type Marker struct {
	Summary   string
	Effective []transcript.Message
	Cleared   bool
}

// LatestMarker finds the newest marker; without one Effective is the whole history.
func LatestMarker(messages []transcript.Message) Marker {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != transcript.MessageRoleSystem {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if summary, ok := compactMarkerSummary(content); ok {
			return Marker{Summary: summary, Effective: messages[i+1:]}
		}
		if summary, ok := clearMarkerSummary(content); ok {
			return Marker{Summary: summary, Effective: messages[i+1:], Cleared: true}
		}
	}
	return Marker{Effective: messages}
}

// LatestSummary returns the newest marker's summary and the history after it.
func LatestSummary(messages []transcript.Message) (string, []transcript.Message) {
	marker := LatestMarker(messages)
	return marker.Summary, marker.Effective
}

// ClearedMarkerContent is the system message content written by /clear.
func ClearedMarkerContent() string {
	return contextClearedMessagePrefix + "\n\n" + contextClearedSummary
}
```

In `LatestSummaryForRun` the fallback call `latestCompactSummary(messages)` becomes `LatestSummary(messages)`. Callers: `execution_request.go` `latestCompactSummaryForRun(` → `agentcontext.LatestSummaryForRun(`; `internal/controlplane/context.go` `core.ContextClearedMessageContent()` → `agentcontext.ClearedMarkerContent()`.

- [ ] **Step 5: `estimate.go`**

```go
package agentcontext

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// EstimatedImageTokens is the flat estimate for one image.
const EstimatedImageTokens = 1_500

const minimumCompactThreshold = 80_000

// Threshold is the token count at which a model window needs compaction.
func Threshold(windowTokens int) int {
	if windowTokens <= 0 {
		return 0
	}
	return max(windowTokens/2, minimumCompactThreshold)
}

// Recommendation reports whether a context of tokens should be compacted and why;
// windowTokens 0 means the window is unknown.
func Recommendation(tokens int, windowTokens int) (bool, string) {
	if windowTokens <= 0 {
		if tokens < minimumCompactThreshold {
			return false, ""
		}
		return true, "estimated context is high; compact session history before continuing"
	}
	if tokens < Threshold(windowTokens) {
		return false, ""
	}
	return true, "estimated context is high for the selected model; compact session history before continuing"
}

// SessionTokens estimates a session's context: fixed parts plus the newest marker
// summary and the history after it.
func SessionTokens(baseTokens int, messages []transcript.Message) int {
	marker := LatestMarker(messages)
	return baseTokens + EstimateTextTokens(marker.Summary) + EstimateMessageTokens(marker.Effective)
}
```

then move verbatim `EstimateMessageTokens`, `EstimateTextTokens`, `FormatShortNumber` (from `context_report.go`, with doc comments) and `EstimateProviderRequestTokens` renamed `EstimateRequestTokens`. Callers: `internal/controlplane/context.go` `core.FormatShortNumber` → `agentcontext.FormatShortNumber`; `clients/terminal/chat/runtime/app_usage.go` `core.EstimateTextTokens` → `agentcontext.EstimateTextTokens`, `core.EstimatedImageTokens` → `agentcontext.EstimatedImageTokens`.

- [ ] **Step 6: `compact.go` and `errors.go`**

`errors.go`: move `isContextLengthExceededError` verbatim as `IsContextLengthExceeded` (doc: `// IsContextLengthExceeded recognises provider errors for requests over the model window.`). In `internal/core/execution.go` replace `isContextLengthExceededError(` with `agentcontext.IsContextLengthExceeded(`.

Also move Stage 1's `stopReasonError` from `internal/core/execution_generation.go` into `errors.go` as `StopReasonError`, body unchanged (it is needed by `summarize` below and by the engine in Task 8; `agent` imports `agentcontext`, not the reverse):

```go
// StopReasonError fails a reply cut by the output limit or a content filter:
// the loop has no continuation path for either.
func StopReasonError(response providers.Response) error {
	switch response.StopReason {
	case providers.StopMaxTokens, providers.StopContentFilter:
		return fmt.Errorf("%s: generation stopped before completion (%s)", response.Provider, response.StopReason)
	default:
		return nil
	}
}
```

In `generateAssistantTurnWithRetry` (`internal/core/execution_generation.go`) change `err = stopReasonError(response)` to `err = agentcontext.StopReasonError(response)` and delete `stopReasonError` there; run `goimports -w` on that file (it may drop `fmt`).

`compact.go`: move `maxCompactPromptRunes` (from `context.go`) and, verbatim, `compactSummarySystemPrompt` … `trimRunesFromStart` from `context_compact.go` (types → `transcript.`). Add:

```go
// Generator produces one model reply; agent.Model and providers.Runtime satisfy it.
type Generator interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

// CompactInput is what a compaction summarises; History must have messages after
// its newest marker.
type CompactInput struct {
	SessionID    string
	History      []transcript.Message
	BaseTokens   int
	PlanSnapshot string
}

// Compact summarises the history after the newest marker and returns the content of
// the new compaction marker, including the before/after token estimate.
func Compact(ctx context.Context, generator Generator, in CompactInput) (string, error) {
	_, effective := LatestSummary(in.History)
	before := SessionTokens(in.BaseTokens, in.History)
	summary, err := summarize(ctx, generator, in.SessionID, effective, in.PlanSnapshot)
	if err != nil {
		return "", err
	}
	summary = strings.TrimSpace(summary)
	previewContent := compactMessagePrefix + "\n\n" + summary
	preview := transcript.Message{
		Role:    transcript.MessageRoleSystem,
		Content: previewContent,
		Parts:   []transcript.MessagePart{{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: previewContent}}},
	}
	after := SessionTokens(in.BaseTokens, append(append([]transcript.Message(nil), in.History...), preview))
	return fmt.Sprintf("%s: ~%s -> ~%s tokens\n\n%s", compactMessagePrefix, FormatShortNumber(before), FormatShortNumber(after), summary), nil
}

func summarize(ctx context.Context, generator Generator, sessionID string, messages []transcript.Message, planSnapshot string) (string, error) {
	content := "Compact this session history:\n\n" + compactHistoryPrompt(messages)
	if planSnapshot != "" {
		content = "Current session plan:\n" + planSnapshot + "\n\n" + content
	}
	response, err := generator.Generate(ctx, providers.Request{
		SessionID:    sessionID,
		SystemPrompt: compactSummarySystemPrompt(),
		Messages:     []providers.Message{{Role: "user", Content: content}},
	})
	if err != nil {
		return "", err
	}
	if err := StopReasonError(response); err != nil {
		return "", err
	}
	text := strings.TrimSpace(response.Text)
	if text == "" {
		return "", errors.New("compact summary is empty")
	}
	return text, nil
}
```

`summarize` is `generateCompactSummary` minus runtime resolution, plan lookup and Stage 0's step recording (the caller's `Generator` records the step inside `Generate`, so, as in Stage 1, the `compact` step is written before `StopReasonError` can fail the summary). Stage 1 adds no request field to the summary request. Move `compactSessionPlanSnapshot` verbatim to `internal/core/plan_tools.go`.

- [ ] **Step 7: `internal/core/session_context.go`**

```go
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)
```

Move verbatim from `context.go`: `CompactSessionResult`, `ContextBlockKind` + consts, `ContextBlock`, `ContextReport` (Stage 0: `LastProviderUsage *providers.Usage`), `ContextCompact`, `ErrSessionRequired`, `SessionContext`; from `context_report.go`: `sessionContextWindowTokens`, `estimateToolSchemaTokens` (`EstimateTextTokens` → `agentcontext.EstimateTextTokens`), `latestProviderUsage` (Stage 0 version, `Usage.IsZero`). Replace Stage 0's `CompactSession` / `compactSession(ctx, sessionID, runID)` with the functions below and add the other changed functions:

```go
// CompactSession summarises the session history into a compaction marker.
func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	return c.compactSession(ctx, sessionID, "")
}

// compactSession records the summary generation as a step of runID when set.
func (c *Core) compactSession(ctx context.Context, sessionID string, runID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return CompactSessionResult{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	if _, effective := agentcontext.LatestSummary(messages); len(effective) == 0 {
		return CompactSessionResult{}, ErrInvalidInput
	}
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return CompactSessionResult{}, err
	}
	var generator agentcontext.Generator = runtime
	if runID != "" {
		generator = runStepGenerator{c: c, runtime: runtime, runID: runID}
	}
	content, err := agentcontext.Compact(ctx, generator, agentcontext.CompactInput{
		SessionID:    session.ID,
		History:      messages,
		BaseTokens:   c.contextBaseTokens(),
		PlanSnapshot: c.compactSessionPlanSnapshot(ctx, session.ID),
	})
	if err != nil {
		return CompactSessionResult{}, err
	}
	message, err := c.CreateSystemMessage(ctx, session.ID, content)
	if err != nil {
		return CompactSessionResult{}, err
	}
	nextMessages, err := c.store.ListMessages(ctx, session.ID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(session, nextMessages)}, nil
}

// contextBaseTokens estimates the parts of every request that are not history.
func (c *Core) contextBaseTokens() int {
	assistant := c.assistantProfile()
	return agentcontext.EstimateTextTokens(prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)) +
		agentcontext.EstimateTextTokens(assistant.CustomInstructions) +
		c.estimateToolSchemaTokens()
}

func (c *Core) contextReportForSession(session Session, messages []transcript.Message) ContextReport {
	report := c.contextReport(session.ID, messages)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	recommended, reason := agentcontext.Recommendation(report.TokenEstimate, report.WindowTokens)
	report.Compact = ContextCompact{Recommended: recommended, Reason: reason}
	return report
}

func (c *Core) contextReport(sessionID string, messages []transcript.Message) ContextReport {
	assistant := c.assistantProfile()
	systemPrompt := prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)
	customInstructions := strings.TrimSpace(assistant.CustomInstructions)
	marker := agentcontext.LatestMarker(messages)
	blocks := make([]ContextBlock, 0, 4)
	if systemPrompt != "" {
		blocks = append(blocks, ContextBlock{ID: "system", Kind: ContextBlockSystemPrompt, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(systemPrompt), Included: true, CacheStability: "stable"})
	}
	if customInstructions != "" {
		blocks = append(blocks, ContextBlock{ID: "custom_instructions", Kind: ContextBlockCustomInstructions, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(customInstructions), Included: true, CacheStability: "stable"})
	}
	if marker.Summary != "" {
		block := ContextBlock{ID: "compact_summary", Kind: ContextBlockCompactSummary, Source: "session_compact", TokenEstimate: agentcontext.EstimateTextTokens(marker.Summary), Included: true, CacheStability: "stable"}
		if marker.Cleared {
			block.ID, block.Kind, block.Source = "clear_marker", ContextBlockClearMarker, "session_clear"
		}
		blocks = append(blocks, block)
	}
	if len(marker.Effective) > 0 {
		blocks = append(blocks, ContextBlock{ID: "messages", Kind: ContextBlockMessages, Source: "session_history", TokenEstimate: agentcontext.EstimateMessageTokens(marker.Effective), Included: true, CacheStability: "dynamic"})
	}
	if estimate := c.estimateToolSchemaTokens(); estimate > 0 {
		blocks = append(blocks, ContextBlock{ID: "tools", Kind: ContextBlockToolSchemas, Source: "tool_registry", TokenEstimate: estimate, Included: true, CacheStability: "stable"})
	}
	total := 0
	for _, block := range blocks {
		if block.Included {
			total += block.TokenEstimate
		}
	}
	recommended, reason := agentcontext.Recommendation(total, 0)
	return ContextReport{
		SessionID:         sessionID,
		Estimated:         true,
		TokenEstimate:     total,
		MessageCount:      len(marker.Effective),
		Blocks:            blocks,
		LastProviderUsage: latestProviderUsage(marker.Effective),
		Compact:           ContextCompact{Recommended: recommended, Reason: reason},
	}
}
```

(Keep only the imports the file uses after the verbatim moves; `json`, `fmt`, `providers` are needed by `latestProviderUsage`, `sessionContextWindowTokens` and the DTOs as they are on `main`. `runStepGenerator` lives in the temporary file of Step 8; Task 10 folds `compactSession` back into `CompactSession` when the old loop, its only run-bound caller, is gone.)

- [ ] **Step 8: Temporary old-loop compaction file `internal/core/execution_compaction.go`**

```go
package core

import (
	"context"
	"fmt"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// runStepGenerator records each successful summary generation of a run as a compact step.
type runStepGenerator struct {
	c       *Core
	runtime providers.Runtime
	runID   string
}

func (g runStepGenerator) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	started := time.Now()
	response, err := g.runtime.Generate(ctx, request)
	if err == nil {
		g.c.recordRunStep(ctx, g.runID, response, "compact", time.Since(started))
	}
	return response, err
}

func (c *Core) autoCompactSessionIfNeeded(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, report, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	if !report.Compact.Recommended || agentcontext.CompactBackoffActive(messages) {
		return false, nil
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages, turn.RunID)
}

func (c *Core) forceCompactSessionForRetry(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, _, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages, turn.RunID)
}

func (c *Core) providerRequestNeedsCompact(ctx context.Context, turn turnExecution, request providers.Request) bool {
	sessionID := normalizeText(turn.SessionID)
	if sessionID == "" {
		return false
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return false
	}
	window := c.sessionContextWindowTokens(c.decorateSessionLLM(session))
	threshold := agentcontext.Threshold(window)
	return threshold > 0 && agentcontext.EstimateRequestTokens(request) >= threshold
}

func (c *Core) sessionContextSnapshot(ctx context.Context, sessionID string) (Session, []transcript.Message, ContextReport, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return Session{}, nil, ContextReport{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	return session, messages, c.contextReportForSession(session, messages), nil
}

func (c *Core) compactSessionWithLoadedMessages(ctx context.Context, session Session, messages []transcript.Message, runID string) (bool, error) {
	if _, effective := agentcontext.LatestSummary(messages); len(effective) == 0 {
		return false, nil
	}
	if _, err := c.compactSession(ctx, session.ID, runID); err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 9: Delete the moved files, build and test**

```bash
git rm -q internal/core/context.go internal/core/context_compact.go internal/core/context_errors.go \
  internal/core/context_markers.go internal/core/context_report.go internal/core/execution_conversation.go
gofmt -l internal clients; go build ./... && go vet ./... && go test ./...
```

Expected: all `ok`, including `./internal/agent/context/` and the Task 1–3 characterization tests.

- [ ] **Step 10: Commit**

```bash
git add internal/agent/context/attachments.go internal/agent/context/conversation.go internal/agent/context/markers.go \
  internal/agent/context/compact.go internal/agent/context/estimate.go internal/agent/context/errors.go \
  internal/agent/context/conversation_test.go internal/agent/context/tool_step_conversation_test.go \
  internal/core/session_context.go internal/core/execution_compaction.go \
  internal/core/core.go internal/core/execution.go internal/core/execution_request.go internal/core/execution_generation.go internal/core/plan_tools.go \
  internal/core/run_recovery.go internal/daemoncmd/run_helpers.go internal/controlplane/context.go \
  clients/terminal/chat/runtime/app_usage.go
git commit -m "refactor(agent): move conversation, markers, compaction and estimates to agentcontext

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Agent ports, task/outcome, message builders and `agenttest`

**Files:**
- Create: `internal/agent/ports.go`, `internal/agent/task.go`, `internal/agent/messages.go`, `internal/agent/sanitize.go`, `internal/agent/agenttest/agenttest.go`

No behaviour yet; this compiles the vocabulary the engine and its tests share. Tests arrive with the engine in Task 8.

- [ ] **Step 1: `internal/agent/ports.go`**

```go
// Package agent runs the native agent loop over narrow ports implemented by core.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Model generates one assistant turn; providers.Runtime satisfies it.
type Model interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

// Window is the transcript a run starts from, ordered by seq.
type Window struct {
	Messages []transcript.Message
}

// Journal persists one session's transcript; during a run the engine is its only writer.
// BeginStreaming/Stream use the fast progress path; FinishStreaming is the durable
// update of an in-flight assistant message and the only update of a written message.
type Journal interface {
	Load(ctx context.Context, sessionID string) (Window, error)
	Append(ctx context.Context, msg transcript.Message) (seq int64, err error)
	BeginStreaming(ctx context.Context, msg transcript.Message) (seq int64, err error)
	Stream(ctx context.Context, msg transcript.Message) error
	FinishStreaming(ctx context.Context, msg transcript.Message) error
	Checkpoint(ctx context.Context, state State) error
	RecordStep(ctx context.Context, step Step) error
}

// Phase is the durable execution boundary crash recovery resumes from.
type Phase string

const (
	PhaseModel Phase = "model"
	PhaseTool  Phase = "tool"
)

// State is the checkpoint written before each model call and tool execution.
type State struct {
	RunID      string
	Phase      Phase
	ToolCallID string
	ToolName   string
}

// Step is one successful model generation (a run_steps row). StopReason is a
// provider stop reason or "compact" for a summary generation.
type Step struct {
	RunID      string
	Model      string
	Provider   string
	Usage      providers.Usage
	StopReason string
	Latency    time.Duration
	ToolCalls  int
}

// Decision says whether a requested tool call may run; Reason goes back to the model.
type Decision struct {
	Allowed bool
	Reason  string
}

// Tools lists, authorizes, executes and finalizes the tools of one run. Execute
// errors are fatal; tool failures come back as IsError results. Finish runs after
// the result message is written.
type Tools interface {
	Specs(ctx context.Context) []tools.Spec
	Authorize(ctx context.Context, name string, call tools.Call) (Decision, error)
	Execute(ctx context.Context, name string, call tools.Call) (tools.Result, error)
	Finish(ctx context.Context, name string, call tools.Call, result tools.Result, message transcript.Message) error
}

// Pending is a tool call that waits for a user decision.
type Pending struct {
	RunID      string
	SessionID  string
	ToolCallID string
	ToolName   string
	Request    tools.ApprovalRequest
}

// Approvals records approval requests and reports whether any are still open.
type Approvals interface {
	Request(ctx context.Context, p Pending) error
	Pending(ctx context.Context, runID string) (bool, error)
}

// InputKind selects what Inbox.Drain returns.
type InputKind string

const (
	InputSteer    InputKind = "steer"
	InputApproved InputKind = "approved"
)

// Input arrives from outside the engine: steer Text, or a granted approval.
type Input struct {
	Kind       InputKind
	Text       string
	ToolCallID string
	ToolName   string
	WorkingDir string
	Args       json.RawMessage
}

// Inbox delivers outside input. Draining InputSteer consumes it; InputApproved returns
// granted approvals whose call has no result yet on every call.
type Inbox interface {
	Drain(ctx context.Context, runID string, kind InputKind) ([]Input, error)
	Canceled(ctx context.Context, runID string) (bool, error)
}

// EventKind names what Sink receives.
type EventKind string

const (
	EventMessageCreated EventKind = "message.created"
	EventMessageUpdated EventKind = "message.updated"
	EventToolRequested  EventKind = "tool.requested"
	EventToolFinished   EventKind = "tool.finished"
)

// Event is live progress for clients.
type Event struct {
	Kind            EventKind
	SessionID       string
	RunID           string
	Message         transcript.Message
	ToolCallID      string
	ToolName        string
	ResultMessageID string
	Result          tools.Result
}

// Sink fans engine events out to clients.
type Sink interface {
	Emit(event Event)
}

// Prompts supplies the core-owned parts of every request: the system prompt and
// custom instructions, the plan snapshot for summaries, and the context budget.
type Prompts interface {
	System(ctx context.Context, summary string, history []transcript.Message) (system, custom string)
	PlanSnapshot(ctx context.Context) string
	Budget(ctx context.Context) (baseTokens, windowTokens int, err error)
}
```

- [ ] **Step 2: `internal/agent/task.go`**

```go
package agent

import "github.com/Suren878/matrixclaw/internal/transcript"

// Task is one native run to execute.
type Task struct {
	RunID       string
	SessionID   string
	Client      string
	ExternalKey string
	WorkingDir  string
	Model       Model
}

// Status is how a Run call ended.
type Status string

const (
	StatusCompleted       Status = "completed"
	StatusWaitingApproval Status = "waiting_approval"
	StatusInterrupted     Status = "interrupted"
	StatusCanceled        Status = "canceled"
	StatusFailed          Status = "failed"
)

// Outcome is applied by core. Assistant is the final reply (completed), the reply to
// seal (canceled, interrupted) or the errored reply (failed with MarkErrored).
// Reached is what the last step produced before an interruption.
type Outcome struct {
	Status         Status
	Assistant      *transcript.Message
	AssistantSaved bool
	Err            error
	MarkErrored    bool
	Reached        Status
}
```

- [ ] **Step 3: `internal/agent/messages.go`**

```go
package agent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ToolCallMessage is the transcript message of one requested tool call.
func ToolCallMessage(id, sessionID, runID, name string, args []byte, finished bool, at time.Time) transcript.Message {
	return transcript.Message{
		ID:        id,
		SessionID: sessionID,
		RunID:     runID,
		Role:      transcript.MessageRoleAssistant,
		Parts: []transcript.MessagePart{{
			Kind:     transcript.MessagePartKindToolCall,
			ToolCall: &transcript.ToolCallPart{ID: id, Name: name, Input: string(args), Finished: finished},
		}},
		CreatedAt: at,
		UpdatedAt: at,
	}
}

// ToolResultMessage is the transcript message carrying a tool result.
func ToolResultMessage(id, sessionID, runID, callID, name string, result tools.Result, at time.Time) (transcript.Message, error) {
	var metadata json.RawMessage
	if result.Metadata != nil {
		body, err := json.Marshal(result.Metadata)
		if err != nil {
			return transcript.Message{}, err
		}
		metadata = body
	}
	content := strings.TrimSpace(result.Content)
	return transcript.Message{
		ID:        id,
		SessionID: sessionID,
		RunID:     runID,
		Role:      transcript.MessageRoleTool,
		Content:   normalizeToolContent(content),
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindToolResult,
			ToolResult: &transcript.ToolResultPart{
				ToolCallID: callID,
				Name:       name,
				Content:    content,
				MIMEType:   result.MIMEType,
				Metadata:   metadata,
				Status:     string(ToolResultStatus(result)),
				IsError:    result.IsError,
			},
		}},
		CreatedAt: at,
		UpdatedAt: at,
	}, nil
}

// ToolResultStatus is the stored status of a tool result.
func ToolResultStatus(result tools.Result) tools.ResultStatus {
	if result.Status != "" {
		return result.Status
	}
	if result.IsError {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

func normalizeToolContent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Tool completed"
	}
	return value
}

// reasoningParts keeps the reasoning a provider needs back with this
// tool step: plain reasoning_content text and signed or encrypted blocks.
func reasoningParts(response providers.Response) []transcript.MessagePart {
	var parts []transcript.MessagePart
	if response.ReasoningContent != nil {
		parts = append(parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: *response.ReasoningContent}})
	}
	for _, block := range response.Reasoning {
		parts = append(parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: block.Text, Signature: block.Signature, RedactedData: block.RedactedData}})
	}
	return parts
}

// finalReply is the persisted form of a completed text reply: text, plain
// reasoning_content and usage (signed reasoning is kept only on tool steps).
func finalReply(assistant transcript.Message, response providers.Response) transcript.Message {
	assistant.Content = sanitizeAssistantOutput(response.Text)
	assistant.Parts = transcript.NormalizeMessageParts(assistant.Content, nil)
	if response.ReasoningContent != nil {
		assistant.Parts = append(assistant.Parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: *response.ReasoningContent}})
	}
	if finish := usageFinishPart(response.Usage); finish != nil {
		assistant.Parts = append(assistant.Parts, *finish)
	}
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	return assistant
}
```

`reasoningParts` is Stage 1's `responseReasoningParts` (`internal/core/execution_generation.go`, Stage 1 Task 4) copied under the engine's name; the core original is deleted with that file in Task 10. `finalReply` is the part-building half of `completeAssistantTurn` (6176876 `execution_status.go:18-24`) with `providerResponseMessageParts(content, response.ReasoningContent)` inlined: Stage 1 left final replies storing only `ReasoningContent`, and this keeps that. Then copy `providerUsageFinishPart` (Stage 0 version: `providers.Usage` payload, `usage.IsZero()`) from `internal/core/execution_status.go` into this file as `usageFinishPart`, unchanged apart from the name. The core copy is deleted in Task 10.

- [ ] **Step 4: Copy the sanitizer**

```bash
cp internal/core/assistant_sanitize.go internal/agent/sanitize.go
sed -i 's/^package core$/package agent/' internal/agent/sanitize.go
```

(The core copy is deleted in Task 10.)

- [ ] **Step 5: `internal/agent/agenttest/agenttest.go`**

```go
// Package agenttest provides a scripted model and in-memory fakes for the agent ports.
package agenttest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	RunID     = "run_1"
	SessionID = "session_1"
)

// Turn is one scripted answer; Stream deltas are streamed before Response is returned.
type Turn struct {
	Stream   []string
	Response providers.Response
	Err      error
}

// ScriptedModel answers requests with its turns in order and records every request.
type ScriptedModel struct {
	turns    []Turn
	requests []providers.Request
}

// NewScriptedModel returns a model that plays turns in order.
func NewScriptedModel(turns ...Turn) *ScriptedModel {
	return &ScriptedModel{turns: turns}
}

func (m *ScriptedModel) Generate(ctx context.Context, req providers.Request) (providers.Response, error) {
	m.requests = append(m.requests, req)
	if len(m.turns) == 0 {
		return providers.Response{}, errors.New("agenttest: no scripted turn left")
	}
	turn := m.turns[0]
	m.turns = m.turns[1:]
	for _, delta := range turn.Stream {
		if err := providers.StreamText(ctx, delta); err != nil {
			return providers.Response{}, err
		}
	}
	return turn.Response, turn.Err
}

// Requests returns every request the model received.
func (m *ScriptedModel) Requests() []providers.Request {
	return m.requests
}

// ModelFunc adapts a function to agent.Model.
type ModelFunc func(ctx context.Context, req providers.Request) (providers.Response, error)

func (f ModelFunc) Generate(ctx context.Context, req providers.Request) (providers.Response, error) {
	return f(ctx, req)
}

// Journal keeps the transcript in memory and counts streaming writes.
type Journal struct {
	Messages []transcript.Message
	States   []agent.State
	Steps    []agent.Step
	Begins   int
	Streams  int
	Finishes int
	LoadErr  error
	seq      int64
}

// Seed stores messages as if they were written before the run.
func (j *Journal) Seed(messages ...transcript.Message) {
	for _, message := range messages {
		j.insert(message)
	}
}

// Message returns the stored message with the ID.
func (j *Journal) Message(id string) (transcript.Message, bool) {
	for _, message := range j.Messages {
		if message.ID == id {
			return message, true
		}
	}
	return transcript.Message{}, false
}

// Result returns the stored result message of a tool call.
func (j *Journal) Result(callID string) (transcript.Message, bool) {
	for _, message := range j.Messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == callID {
				return message, true
			}
		}
	}
	return transcript.Message{}, false
}

func (j *Journal) Load(context.Context, string) (agent.Window, error) {
	if j.LoadErr != nil {
		return agent.Window{}, j.LoadErr
	}
	return agent.Window{Messages: append([]transcript.Message(nil), j.Messages...)}, nil
}

func (j *Journal) Append(_ context.Context, msg transcript.Message) (int64, error) {
	return j.insert(msg), nil
}

func (j *Journal) BeginStreaming(_ context.Context, msg transcript.Message) (int64, error) {
	j.Begins++
	return j.insert(msg), nil
}

func (j *Journal) Stream(_ context.Context, msg transcript.Message) error {
	j.Streams++
	return j.replace(msg)
}

func (j *Journal) FinishStreaming(_ context.Context, msg transcript.Message) error {
	j.Finishes++
	return j.replace(msg)
}

func (j *Journal) Checkpoint(_ context.Context, state agent.State) error {
	j.States = append(j.States, state)
	return nil
}

func (j *Journal) RecordStep(_ context.Context, step agent.Step) error {
	j.Steps = append(j.Steps, step)
	return nil
}

func (j *Journal) insert(msg transcript.Message) int64 {
	j.seq++
	msg.Seq = j.seq
	j.Messages = append(j.Messages, msg)
	return j.seq
}

func (j *Journal) replace(msg transcript.Message) error {
	for i := range j.Messages {
		if j.Messages[i].ID == msg.ID {
			msg.Seq = j.Messages[i].Seq
			j.Messages[i] = msg
			return nil
		}
	}
	return fmt.Errorf("agenttest: message %q not found", msg.ID)
}

// ToolFunc executes one fake tool call.
type ToolFunc func(call tools.Call) tools.Result

// Tools authorizes registered names only and records executed and finished calls.
type Tools struct {
	Funcs    map[string]ToolFunc
	Calls    []tools.Call
	Finished []string
}

func (t *Tools) Specs(context.Context) []tools.Spec {
	names := make([]string, 0, len(t.Funcs))
	for name := range t.Funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]tools.Spec, 0, len(names))
	for _, name := range names {
		specs = append(specs, tools.Spec{ID: name, Name: name, Description: name})
	}
	return specs
}

func (t *Tools) Authorize(_ context.Context, name string, _ tools.Call) (agent.Decision, error) {
	if _, ok := t.Funcs[name]; !ok {
		return agent.Decision{Reason: fmt.Sprintf("invalid input: unknown tool %q", name)}, nil
	}
	return agent.Decision{Allowed: true}, nil
}

func (t *Tools) Execute(_ context.Context, name string, call tools.Call) (tools.Result, error) {
	t.Calls = append(t.Calls, call)
	return t.Funcs[name](call), nil
}

func (t *Tools) Finish(_ context.Context, _ string, call tools.Call, _ tools.Result, _ transcript.Message) error {
	t.Finished = append(t.Finished, call.ToolCallID)
	return nil
}

// Approvals records requests; Grant, when set, resolves each request at once.
type Approvals struct {
	Requests []agent.Pending
	Open     bool
	Grant    func(agent.Pending)
}

func (a *Approvals) Request(_ context.Context, p agent.Pending) error {
	a.Requests = append(a.Requests, p)
	if a.Grant != nil {
		a.Grant(p)
		return nil
	}
	a.Open = true
	return nil
}

func (a *Approvals) Pending(context.Context, string) (bool, error) {
	return a.Open, nil
}

// Inbox hands out steers once and granted approvals on every drain.
type Inbox struct {
	Steers   []string
	Approved []agent.Input
	Cancel   bool
}

func (in *Inbox) Drain(_ context.Context, _ string, kind agent.InputKind) ([]agent.Input, error) {
	switch kind {
	case agent.InputSteer:
		out := make([]agent.Input, 0, len(in.Steers))
		for _, text := range in.Steers {
			out = append(out, agent.Input{Kind: agent.InputSteer, Text: text})
		}
		in.Steers = nil
		return out, nil
	case agent.InputApproved:
		return in.Approved, nil
	default:
		return nil, fmt.Errorf("agenttest: unknown input kind %q", kind)
	}
}

func (in *Inbox) Canceled(context.Context, string) (bool, error) {
	return in.Cancel, nil
}

// Sink records every event.
type Sink struct {
	Events []agent.Event
}

func (s *Sink) Emit(event agent.Event) {
	s.Events = append(s.Events, event)
}

// Kinds lists the recorded event kinds in order.
func (s *Sink) Kinds() []agent.EventKind {
	kinds := make([]agent.EventKind, 0, len(s.Events))
	for _, event := range s.Events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

// Prompts returns Text as system prompt, extended by the compact summary it is given.
type Prompts struct {
	Text         string
	BaseTokens   int
	WindowTokens int
}

func (p *Prompts) System(_ context.Context, summary string, _ []transcript.Message) (string, string) {
	if summary == "" {
		return p.Text, ""
	}
	return p.Text + "\n\nSession context summary:\n" + summary, ""
}

func (p *Prompts) PlanSnapshot(context.Context) string {
	return ""
}

func (p *Prompts) Budget(context.Context) (int, int, error) {
	return p.BaseTokens, p.WindowTokens, nil
}

// Fixture wires an engine to fresh fakes around one seeded user message.
type Fixture struct {
	Journal   *Journal
	Tools     *Tools
	Approvals *Approvals
	Inbox     *Inbox
	Sink      *Sink
	Prompts   *Prompts
	Clock     time.Time
	ids       int
}

// NewFixture returns fakes with the run's user message already journaled.
func NewFixture() *Fixture {
	clock := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	f := &Fixture{
		Journal:   &Journal{},
		Tools:     &Tools{Funcs: map[string]ToolFunc{}},
		Approvals: &Approvals{},
		Inbox:     &Inbox{},
		Sink:      &Sink{},
		Prompts:   &Prompts{Text: "system"},
		Clock:     clock,
	}
	f.Journal.Seed(transcript.Message{
		ID: "msg_user", SessionID: SessionID, RunID: RunID, Role: transcript.MessageRoleUser,
		Content: "do the task", Parts: transcript.NormalizeMessageParts("do the task", nil),
		CreatedAt: clock, UpdatedAt: clock,
	})
	return f
}

// Engine returns an engine over the fixture's fakes with a fixed clock and sequential IDs.
func (f *Fixture) Engine() *agent.Engine {
	return agent.New(agent.Config{
		Journal:   f.Journal,
		Tools:     f.Tools,
		Approvals: f.Approvals,
		Inbox:     f.Inbox,
		Sink:      f.Sink,
		Prompts:   f.Prompts,
		Now:       func() time.Time { return f.Clock },
		NewID: func(prefix string) string {
			f.ids++
			return fmt.Sprintf("%s_%d", prefix, f.ids)
		},
	})
}

// Task returns the fixture's run for model.
func (f *Fixture) Task(model agent.Model) agent.Task {
	return agent.Task{RunID: RunID, SessionID: SessionID, WorkingDir: "/work", Model: model}
}
```

`agenttest.go` references `agent.New`/`agent.Config`/`agent.Engine`, defined in Task 8; to keep this commit compiling, **do Step 5 as part of Task 8** (create the file there) and commit Steps 1–4 now.

- [ ] **Step 6: Build**

Run: `go build ./... && go vet ./internal/agent/...`
Expected: success (the sanitizer copy is unused until Task 8: `go vet` does not flag unused functions).

- [ ] **Step 7: Commit**

```bash
git add internal/agent/ports.go internal/agent/task.go internal/agent/messages.go internal/agent/sanitize.go
git commit -m "feat(agent): add engine ports, outcome and transcript message builders

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: The engine loop

**Files:**
- Create: `internal/agent/agenttest/agenttest.go` (Task 7 Step 5 code), `internal/agent/engine_test.go`, `internal/agent/engine.go`, `internal/agent/journal.go`, `internal/agent/generate.go`, `internal/agent/request.go`, `internal/agent/tools.go`

- [ ] **Step 1: Write the failing engine tests** — `internal/agent/engine_test.go`

```go
package agent_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

var markerPattern = regexp.MustCompile("^🧠 Context compacted: ~[0-9.]+[kM]? -> ~[0-9.]+[kM]? tokens\n\nSUMMARY$")

func text(value string) agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{Text: value}}
}

func calls(toolCalls ...providers.ToolCall) agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{ToolCalls: toolCalls}}
}

func call(id, name string) providers.ToolCall {
	return providers.ToolCall{ID: id, Name: name, Arguments: []byte(`{}`)}
}

func run(t *testing.T, f *agenttest.Fixture, model agent.Model) agent.Outcome {
	t.Helper()
	outcome, err := f.Engine().Run(context.Background(), f.Task(model))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	return outcome
}

func phases(states []agent.State) string {
	out := make([]string, 0, len(states))
	for _, state := range states {
		if state.ToolCallID == "" {
			out = append(out, string(state.Phase))
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s", state.Phase, state.ToolCallID))
	}
	return strings.Join(out, ",")
}

func hasFinish(message transcript.Message, reason string) bool {
	return transcript.HasFinishReason(message, reason)
}

func toolContent(request providers.Request, callID string) string {
	for _, message := range request.Messages {
		if message.ToolCallID == callID {
			return message.Content
		}
	}
	return ""
}

func writeTool(call tools.Call) tools.Result {
	if !call.Approved {
		return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "write", ToolCallID: call.ToolCallID, Action: "write"}}
	}
	return tools.Result{Content: "written"}
}

func readTool(tools.Call) tools.Result {
	return tools.Result{Content: "file body"}
}

func TestTextReplyCompletesWithFinalMessage(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Response: providers.Response{
		Text: "Hello.", Model: "m1", Provider: "p1", Usage: providers.Usage{PromptTokens: 5, OutputTokens: 1},
	}})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant == nil || outcome.AssistantSaved {
		t.Fatalf("outcome = %+v", outcome)
	}
	reply := *outcome.Assistant
	if reply.Content != "Hello." || reply.Model != "m1" || reply.Provider != "p1" || !hasFinish(reply, "end_turn") {
		t.Fatalf("reply = %+v", reply)
	}
	if len(f.Journal.Steps) != 1 || f.Journal.Steps[0].Usage.PromptTokens != 5 || f.Journal.Steps[0].Model != "m1" || f.Journal.Steps[0].RunID != agenttest.RunID {
		t.Fatalf("steps = %+v", f.Journal.Steps)
	}
	if got := phases(f.Journal.States); got != "model" {
		t.Fatalf("checkpoints = %s", got)
	}
}

func TestToolRoundTripIsJournaledInOrder(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{Text: "Reading.", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read", Arguments: []byte(`{"path":"a"}`)}}}},
		text("Done."),
	)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Done." {
		t.Fatalf("outcome = %+v", outcome)
	}
	messages := f.Journal.Messages
	if len(messages) != 4 || messages[1].Content != "Reading." || !hasFinish(messages[1], "tool_calls") {
		t.Fatalf("messages = %+v", messages)
	}
	if part := messages[2].Parts[0].ToolCall; messages[2].ID != "c1" || part == nil || !part.Finished || part.Input != `{"path":"a"}` {
		t.Fatalf("call message = %+v", messages[2])
	}
	if messages[3].Content != "file body" || messages[3].Parts[0].ToolResult.ToolCallID != "c1" {
		t.Fatalf("result message = %+v", messages[3])
	}
	wantKinds := []agent.EventKind{agent.EventMessageCreated, agent.EventMessageCreated, agent.EventToolRequested, agent.EventMessageUpdated, agent.EventMessageCreated, agent.EventToolFinished}
	if fmt.Sprint(f.Sink.Kinds()) != fmt.Sprint(wantKinds) {
		t.Fatalf("events = %v, want %v", f.Sink.Kinds(), wantKinds)
	}
	if got := phases(f.Journal.States); got != "model,tool:c1,model,model" {
		t.Fatalf("checkpoints = %s", got)
	}
	executed := f.Tools.Calls[0]
	if executed.ToolCallID != "c1" || executed.WorkingDir != "/work" || executed.Approved || executed.RunID != agenttest.RunID {
		t.Fatalf("executed call = %+v", executed)
	}
	if got := toolContent(model.Requests()[1], "c1"); got != "file body" {
		t.Fatalf("second request tool content = %q", got)
	}
	if fmt.Sprint(f.Tools.Finished) != "[c1]" {
		t.Fatalf("finished = %v", f.Tools.Finished)
	}
}

func TestThirtyTwoToolStepsFailTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	turns := make([]agenttest.Turn, 0, 40)
	for i := 0; i < 40; i++ {
		turns = append(turns, calls(call(fmt.Sprintf("c%d", i), "read")))
	}
	model := agenttest.NewScriptedModel(turns...)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || outcome.Err == nil || outcome.Err.Error() != "tool loop exceeded 32 steps" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(model.Requests()) != 32 || len(f.Tools.Calls) != 32 {
		t.Fatalf("requests=%d tool calls=%d", len(model.Requests()), len(f.Tools.Calls))
	}
}

func TestApprovalRequestParksAfterTheRestOfTheBatch(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(calls(call("w1", "write"), call("r1", "read")))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusWaitingApproval {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(f.Approvals.Requests) != 1 || f.Approvals.Requests[0].ToolCallID != "w1" || f.Approvals.Requests[0].Request.Action != "write" {
		t.Fatalf("approval requests = %+v", f.Approvals.Requests)
	}
	if _, ok := f.Journal.Result("r1"); !ok {
		t.Fatal("read after the approval barrier did not run")
	}
	if _, ok := f.Journal.Result("w1"); ok {
		t.Fatal("unapproved write has a result")
	}
	if message, _ := f.Journal.Message("w1"); message.Parts[0].ToolCall.Finished {
		t.Fatal("pending call marked finished")
	}
}

func TestResolvedApprovalContinuesTheSameRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Approvals.Grant = func(p agent.Pending) {
		f.Inbox.Approved = append(f.Inbox.Approved, agent.Input{Kind: agent.InputApproved, ToolCallID: p.ToolCallID, ToolName: p.ToolName, WorkingDir: "/work", Args: []byte(`{}`)})
	}
	model := agenttest.NewScriptedModel(calls(call("w1", "write")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 2 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	if len(f.Tools.Calls) != 2 || f.Tools.Calls[0].Approved || !f.Tools.Calls[1].Approved {
		t.Fatalf("calls = %+v", f.Tools.Calls)
	}
	if result, ok := f.Journal.Result("w1"); !ok || result.Content != "written" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGrantedApprovalIsExecutedBeforeTheNextModelCall(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["write"] = writeTool
	f.Journal.Seed(agent.ToolCallMessage("w1", agenttest.SessionID, agenttest.RunID, "write", []byte(`{}`), false, f.Clock))
	f.Inbox.Approved = []agent.Input{{Kind: agent.InputApproved, ToolCallID: "w1", ToolName: "write", WorkingDir: "/work", Args: []byte(`{}`)}}
	model := agenttest.NewScriptedModel(text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 1 || !f.Tools.Calls[0].Approved {
		t.Fatalf("outcome = %+v calls = %+v", outcome, f.Tools.Calls)
	}
	if message, _ := f.Journal.Message("w1"); !message.Parts[len(message.Parts)-1].ToolCall.Finished {
		t.Fatal("approved call not marked finished")
	}
	if got := toolContent(model.Requests()[0], "w1"); got != "written" {
		t.Fatalf("request tool content = %q", got)
	}
}

func TestUnknownToolIsReturnedAsErrorResult(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(calls(call("x1", "missing")), text("Fixed."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 0 {
		t.Fatalf("outcome = %+v calls = %+v", outcome, f.Tools.Calls)
	}
	if message, _ := f.Journal.Message("x1"); !message.Parts[0].ToolCall.Finished {
		t.Fatal("rejected call not marked finished")
	}
	result, ok := f.Journal.Result("x1")
	if !ok || !result.Parts[0].ToolResult.IsError || !strings.Contains(result.Content, `unknown tool "missing"`) {
		t.Fatalf("result = %+v", result)
	}
	for _, kind := range f.Sink.Kinds() {
		if kind == agent.EventToolRequested {
			t.Fatal("rejected call emitted tool.requested")
		}
	}
}

func TestSteerIsAppendedToTheNextToolResult(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	f.Inbox.Steers = []string{"check the logs"}
	model := agenttest.NewScriptedModel(calls(call("r1", "read")), text("Done."))

	run(t, f, model)

	result, _ := f.Journal.Result("r1")
	if result.Content != "file body\n\nUser guidance: check the logs" || len(f.Inbox.Steers) != 0 {
		t.Fatalf("result = %q steers left = %v", result.Content, f.Inbox.Steers)
	}
	if got := toolContent(model.Requests()[1], "r1"); !strings.Contains(got, "User guidance: check the logs") {
		t.Fatalf("request tool content = %q", got)
	}
}

func TestEmptyRepliesAreRetriedTwiceAndRecorded(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{}, agenttest.Turn{}, text("ok"))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 3 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	if len(f.Journal.Steps) != 3 {
		t.Fatalf("recorded steps = %d, want every generation (3)", len(f.Journal.Steps))
	}
}

func TestPartialOutputIsNotRetried(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Stream: []string{"Unfinished"}, Err: providers.ErrIncompleteResponse})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || !outcome.MarkErrored || !outcome.AssistantSaved || outcome.Assistant.Content != "Unfinished" || !errors.Is(outcome.Err, providers.ErrIncompleteResponse) {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(model.Requests()) != 1 {
		t.Fatalf("requests = %d", len(model.Requests()))
	}
}

func TestStreamingProgressIsBatched(t *testing.T) {
	f := agenttest.NewFixture()
	deltas := make([]string, 4096)
	for i := range deltas {
		deltas[i] = "x"
	}
	model := agenttest.NewScriptedModel(agenttest.Turn{Stream: deltas, Response: providers.Response{Text: strings.Repeat("x", 4096)}})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || !outcome.AssistantSaved {
		t.Fatalf("outcome = %+v", outcome)
	}
	if f.Journal.Begins != 1 || f.Journal.Streams != 1 {
		t.Fatalf("progress writes = %d begins + %d streams, want 1 + 1", f.Journal.Begins, f.Journal.Streams)
	}
}

func TestCancellationSeenAfterGenerationSealsTheReply(t *testing.T) {
	f := agenttest.NewFixture()
	f.Inbox.Cancel = true

	outcome := run(t, f, agenttest.NewScriptedModel(text("Hi")))

	if outcome.Status != agent.StatusCanceled || outcome.Assistant == nil {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestStoppedContextReturnsInterruptedWithTheReachedReply(t *testing.T) {
	f := agenttest.NewFixture()
	ctx, cancel := context.WithCancel(context.Background())
	model := agenttest.ModelFunc(func(context.Context, providers.Request) (providers.Response, error) {
		cancel()
		return providers.Response{Text: "late answer"}, nil
	})

	outcome, err := f.Engine().Run(ctx, f.Task(model))

	if err != nil || outcome.Status != agent.StatusInterrupted || outcome.Reached != agent.StatusCompleted || outcome.Assistant.Content != "late answer" {
		t.Fatalf("outcome = %+v err = %v", outcome, err)
	}
}

func TestAutoCompactionAddsMarkerBeforeTheModelCall(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.BaseTokens = 90_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 2 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if !strings.Contains(requests[0].SystemPrompt, "You compact matrixclaw chat histories") || !strings.Contains(requests[1].SystemPrompt, "Session context summary:\nSUMMARY") {
		t.Fatalf("summary prompt = %q main prompt = %q", requests[0].SystemPrompt, requests[1].SystemPrompt)
	}
	marker := f.Journal.Messages[1]
	if marker.Role != transcript.MessageRoleSystem || marker.RunID != "" || !markerPattern.MatchString(marker.Content) {
		t.Fatalf("marker = %+v", marker)
	}
	if len(f.Journal.Steps) != 2 || f.Journal.Steps[0].StopReason != "compact" {
		t.Fatalf("steps = %+v, want the summary recorded as compact first", f.Journal.Steps)
	}
}

func TestContextLengthErrorCompactsAndRetriesOnce(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("context_length_exceeded")}, text("SUMMARY"), text("Recovered."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Recovered." || len(model.Requests()) != 3 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
	if !markerPattern.MatchString(f.Journal.Messages[1].Content) {
		t.Fatalf("marker = %+v", f.Journal.Messages[1])
	}
}

func TestLoadFailureFailsTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Journal.LoadErr = errors.New("disk gone")

	outcome := run(t, f, agenttest.NewScriptedModel())

	if outcome.Status != agent.StatusFailed || outcome.Err == nil || outcome.Err.Error() != "disk gone" {
		t.Fatalf("outcome = %+v", outcome)
	}
}
```

Also create `internal/agent/agenttest/agenttest.go` with the code from Task 7 Step 5.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/agent/...`
Expected: FAIL to compile (`undefined: agent.New`, `agent.Config`, `agent.Engine`).

- [ ] **Step 3: `internal/agent/journal.go`**

```go
package agent

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// history is the run's in-memory transcript; every write goes through the Journal
// port first and is then announced on the Sink.
type history struct {
	port     Journal
	sink     Sink
	messages []transcript.Message
	index    map[string]int
	calls    map[string]int
	results  map[string]struct{}
}

func newHistory(port Journal, sink Sink, window Window) *history {
	h := &history{port: port, sink: sink, index: map[string]int{}, calls: map[string]int{}, results: map[string]struct{}{}}
	for _, message := range window.Messages {
		h.add(message)
	}
	return h
}

func (h *history) all() []transcript.Message {
	return h.messages
}

func (h *history) message(id string) (transcript.Message, bool) {
	i, ok := h.index[id]
	if !ok {
		return transcript.Message{}, false
	}
	return h.messages[i], true
}

// callMessage is the newest message carrying a tool call with the ID.
func (h *history) callMessage(callID string) (transcript.Message, bool) {
	i, ok := h.calls[strings.TrimSpace(callID)]
	if !ok {
		return transcript.Message{}, false
	}
	return h.messages[i], true
}

func (h *history) hasResult(callID string) bool {
	_, ok := h.results[strings.TrimSpace(callID)]
	return ok
}

func (h *history) append(ctx context.Context, message transcript.Message) error {
	seq, err := h.port.Append(ctx, message)
	if err != nil {
		return err
	}
	message.Seq = seq
	h.add(message)
	h.emit(EventMessageCreated, message)
	return nil
}

func (h *history) beginStreaming(ctx context.Context, message transcript.Message) error {
	seq, err := h.port.BeginStreaming(ctx, message)
	if err != nil {
		return err
	}
	message.Seq = seq
	h.add(message)
	h.emit(EventMessageCreated, message)
	return nil
}

func (h *history) stream(ctx context.Context, message transcript.Message) error {
	if err := h.port.Stream(ctx, message); err != nil {
		return err
	}
	h.replace(message)
	h.emit(EventMessageUpdated, message)
	return nil
}

func (h *history) finish(ctx context.Context, message transcript.Message) error {
	if err := h.port.FinishStreaming(ctx, message); err != nil {
		return err
	}
	h.replace(message)
	h.emit(EventMessageUpdated, message)
	return nil
}

func (h *history) add(message transcript.Message) {
	h.index[message.ID] = len(h.messages)
	h.messages = append(h.messages, message)
	h.indexParts(len(h.messages) - 1)
}

func (h *history) replace(message transcript.Message) {
	i, ok := h.index[message.ID]
	if !ok {
		h.add(message)
		return
	}
	message.Seq = h.messages[i].Seq
	h.messages[i] = message
	h.indexParts(i)
}

func (h *history) indexParts(i int) {
	for _, part := range h.messages[i].Parts {
		if part.ToolCall != nil {
			if id := strings.TrimSpace(part.ToolCall.ID); id != "" {
				h.calls[id] = i
			}
		}
		if part.ToolResult != nil {
			if id := strings.TrimSpace(part.ToolResult.ToolCallID); id != "" {
				h.results[id] = struct{}{}
			}
		}
	}
}

func (h *history) emit(kind EventKind, message transcript.Message) {
	h.sink.Emit(Event{Kind: kind, SessionID: message.SessionID, RunID: message.RunID, Message: message})
}
```

- [ ] **Step 4: `internal/agent/engine.go`**

```go
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const maxSteps = 32

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
}

// Engine runs the native agent loop over its ports.
type Engine struct {
	cfg Config
}

// New returns an engine over the given ports.
func New(cfg Config) *Engine {
	return &Engine{cfg: cfg}
}

// Run executes steps until the run completes, parks, fails or ctx stops. The error
// is reserved for failures that must leave the run's status untouched.
func (e *Engine) Run(ctx context.Context, task Task) (Outcome, error) {
	if task.RunID == "" || task.SessionID == "" || task.Model == nil {
		return Outcome{}, errors.New("agent: task requires run, session and model")
	}
	window, err := e.cfg.Journal.Load(ctx, task.SessionID)
	if err != nil {
		if ctx.Err() != nil {
			return Outcome{Status: StatusInterrupted}, nil
		}
		return Outcome{Status: StatusFailed, Err: err}, nil
	}
	r := &run{Config: e.cfg, task: task, history: newHistory(e.cfg.Journal, e.cfg.Sink, window)}
	for step := 0; step < maxSteps; step++ {
		result := r.step(ctx)
		if result.canceled {
			return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, nil
		}
		if ctx.Err() != nil {
			return r.interrupted(result), nil
		}
		outcome, done, err := r.settle(ctx, result)
		if err != nil || done {
			return outcome, err
		}
	}
	return Outcome{Status: StatusFailed, Err: fmt.Errorf("tool loop exceeded %d steps", maxSteps)}, nil
}

type run struct {
	Config
	task    Task
	history *history
}

type stepKind int

const (
	stepContinue stepKind = iota
	stepWaitingApproval
	stepDone
)

type stepResult struct {
	kind        stepKind
	canceled    bool
	assistant   *transcript.Message
	saved       bool
	response    providers.Response
	err         error
	markErrored bool
}

func failedStep(err error) stepResult {
	return stepResult{kind: stepDone, err: err}
}

func (r *run) step(ctx context.Context) stepResult {
	waiting, err := r.resumeApproved(ctx)
	if err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingApproval}
	}
	if err := r.checkpoint(ctx, PhaseModel, "", ""); err != nil {
		return failedStep(err)
	}
	compacted, err := r.autoCompact(ctx)
	if err != nil {
		return failedStep(err)
	}
	request, err := r.buildRequest(ctx)
	if err != nil {
		return failedStep(err)
	}
	if !compacted && r.requestNeedsCompact(ctx, request) {
		if compacted, err = r.compact(ctx); err != nil {
			return failedStep(err)
		}
		if compacted {
			if request, err = r.buildRequest(ctx); err != nil {
				return failedStep(err)
			}
		}
	}
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && agentcontext.IsContextLengthExceeded(err) {
		compacted, compactErr := r.compact(ctx)
		if compactErr != nil {
			return failedStep(compactErr)
		}
		if compacted {
			retry, buildErr := r.buildRequest(ctx)
			if buildErr != nil {
				return failedStep(buildErr)
			}
			gen, err = r.generateWithRetry(ctx, retry)
		}
	}
	if err != nil {
		return stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: gen.response, err: err, markErrored: true}
	}
	if r.canceled(ctx) {
		return stepResult{kind: stepDone, canceled: true, assistant: &gen.assistant, saved: gen.saved}
	}
	return r.handleResponse(ctx, gen)
}

func (r *run) handleResponse(ctx context.Context, gen generation) stepResult {
	response := gen.response
	response.Text = sanitizeAssistantOutput(response.Text)
	assistant := gen.assistant
	if len(response.ToolCalls) > 0 {
		if err := r.finishToolTurn(ctx, &assistant, gen.saved, response); err != nil {
			return failedStep(err)
		}
		waiting, err := r.executeBatch(ctx, response)
		if err != nil {
			return stepResult{kind: stepDone, assistant: &assistant, saved: true, response: response, err: err}
		}
		if waiting {
			return stepResult{kind: stepWaitingApproval}
		}
		return stepResult{kind: stepContinue}
	}
	if strings.TrimSpace(response.Text) == "" {
		return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response, err: providers.ErrEmptyResponse, markErrored: true}
	}
	return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response}
}

// finishToolTurn writes the model's commentary, reasoning and usage before its tools run.
func (r *run) finishToolTurn(ctx context.Context, assistant *transcript.Message, saved bool, response providers.Response) error {
	assistant.Content = response.Text
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	assistant.Parts = append(reasoningParts(response), transcript.NormalizeMessageParts(assistant.Content, nil)...)
	finish := usageFinishPart(response.Usage)
	if finish == nil {
		finish = &transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{}}
	}
	finish.Finish.Reason = "tool_calls"
	assistant.Parts = append(assistant.Parts, *finish)
	assistant.UpdatedAt = r.Now()
	if saved {
		return r.history.finish(ctx, *assistant)
	}
	assistant.CreatedAt = assistant.UpdatedAt
	return r.history.append(ctx, *assistant)
}

func (r *run) settle(ctx context.Context, result stepResult) (Outcome, bool, error) {
	if result.assistant != nil && r.canceled(ctx) {
		return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, true, nil
	}
	if result.err != nil {
		outcome := Outcome{Status: StatusFailed, Err: result.err}
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
		return Outcome{Status: StatusWaitingApproval}, true, nil
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		return Outcome{Status: StatusCompleted, Assistant: &reply, AssistantSaved: result.saved}, true, nil
	default:
		return Outcome{}, false, nil
	}
}

func (r *run) interrupted(result stepResult) Outcome {
	outcome := Outcome{Status: StatusInterrupted, Assistant: result.assistant, AssistantSaved: result.saved}
	if result.err != nil {
		return outcome
	}
	switch result.kind {
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		outcome.Assistant, outcome.Reached = &reply, StatusCompleted
	case stepWaitingApproval:
		outcome.Reached = StatusWaitingApproval
	}
	return outcome
}

func (r *run) canceled(ctx context.Context) bool {
	canceled, err := r.Inbox.Canceled(ctx, r.task.RunID)
	return err == nil && canceled
}

func (r *run) checkpoint(ctx context.Context, phase Phase, toolCallID string, toolName string) error {
	return r.Journal.Checkpoint(ctx, State{RunID: r.task.RunID, Phase: phase, ToolCallID: toolCallID, ToolName: toolName})
}
```

- [ ] **Step 5: `internal/agent/generate.go`**

```go
package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const progressFlushInterval = 750 * time.Millisecond

var errRunCanceled = errors.New("run canceled")

// generation is one model call and the assistant message it streamed.
type generation struct {
	assistant transcript.Message
	saved     bool
	response  providers.Response
}

// compactStopReason marks summary generations in run_steps.
const compactStopReason = "compact"

// recordStep stores one successful generation as a run step.
func (r *run) recordStep(ctx context.Context, response providers.Response, stopReason string, latency time.Duration) error {
	return r.Journal.RecordStep(ctx, Step{
		RunID:      r.task.RunID,
		Model:      response.Model,
		Provider:   response.Provider,
		Usage:      response.Usage,
		StopReason: stopReason,
		Latency:    latency,
		ToolCalls:  len(response.ToolCalls),
	})
}

// summaryModel is the run's model with every successful summary recorded as a compact step.
type summaryModel struct {
	r *run
}

func (m summaryModel) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	started := time.Now()
	response, err := m.r.task.Model.Generate(ctx, request)
	if err != nil {
		return response, err
	}
	return response, m.r.recordStep(ctx, response, compactStopReason, time.Since(started))
}

// generateWithRetry retries only failures that happened before any output was shown;
// a partial answer stays visible as failed instead of being replayed.
func (r *run) generateWithRetry(ctx context.Context, request providers.Request) (generation, error) {
	backoffs := [...]time.Duration{200 * time.Millisecond, 750 * time.Millisecond}
	for attempt := 0; ; attempt++ {
		gen, err := r.generate(ctx, request)
		if err == nil {
			err = agentcontext.StopReasonError(gen.response)
		}
		if err == nil && sanitizeAssistantOutput(gen.response.Text) == "" && len(gen.response.ToolCalls) == 0 {
			err = providers.ErrEmptyResponse
		}
		if err == nil || gen.saved || gen.assistant.Content != "" || ctx.Err() != nil || attempt >= len(backoffs) || !providers.IsRetryableGenerationError(err) {
			return gen, err
		}
		timer := time.NewTimer(backoffs[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return gen, ctx.Err()
		case <-timer.C:
		}
	}
}

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
	started := time.Now()
	response, err := r.task.Model.Generate(streamCtx, request)
	if err == nil {
		err = r.recordStep(ctx, response, stepStopReason(response), time.Since(started))
	}
	if flushErr := flush(true); flushErr != nil {
		err = errors.Join(err, flushErr)
	}
	gen.response = response
	return gen, err
}

// stepStopReason is the run_steps stop reason of a model generation.
func stepStopReason(response providers.Response) string {
	return string(providers.ResolveStopReason(response.StopReason, len(response.ToolCalls)))
}
```

`stepStopReason` is `generationStopReason` from `internal/core/usage.go` as Stage 1 Task 3 left it; the core copy is deleted in Task 10. Like Stage 0, a step is recorded for every successful `Generate`, including empty replies that are then retried. The `agentcontext.StopReasonError` check sits where Stage 1 put `stopReasonError` in `generateAssistantTurnWithRetry`: after the step is recorded and before the empty-reply check, so a truncated reply is recorded as `max_tokens` and then fails without a retry.
- [ ] **Step 6: `internal/agent/request.go`**

```go
package agent

import (
	"context"
	"fmt"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ToolUseAllowed reports whether the model can receive tool definitions.
func ToolUseAllowed(model Model) bool {
	if profiler, ok := model.(providers.RuntimeProfiler); ok {
		if providers.NormalizeRuntimeProfile(profiler.RuntimeProfile()).ToolUseMode == providers.ToolUseDisabled {
			return false
		}
	}
	capabilities, ok := model.(providers.RuntimeCapabilityProvider)
	if !ok {
		return true
	}
	return capabilities.ModelCapabilities().ToolCalling
}

// ImageInputAllowed reports whether the model accepts inline images.
func ImageInputAllowed(model Model) bool {
	capabilities, ok := model.(providers.RuntimeCapabilityProvider)
	return ok && capabilities.ModelCapabilities().ImageInput
}

func (r *run) buildRequest(ctx context.Context) (providers.Request, error) {
	summary, effective := agentcontext.LatestSummaryForRun(r.history.all(), r.task.RunID)
	system, custom := r.Prompts.System(ctx, summary, effective)
	request := providers.Request{
		RunID:              r.task.RunID,
		SessionID:          r.task.SessionID,
		SystemPrompt:       system,
		CustomInstructions: custom,
		CacheKey:           r.task.SessionID,
	}
	if !ToolUseAllowed(r.task.Model) {
		request.Messages = agentcontext.TextOnlyConversation(effective, r.task.RunID)
		request.Messages = providers.NormalizeMessages(request.Messages, providers.ToolUseDisabled)
		return request, nil
	}
	messages, err := agentcontext.Conversation(ctx, effective, r.Attachments, r.task.RunID, ImageInputAllowed(r.task.Model))
	if err != nil {
		return providers.Request{}, err
	}
	request.Messages = messages
	request.Tools = toolDefinitions(r.Tools.Specs(ctx))
	return request, nil
}

func toolDefinitions(specs []tools.Spec) []providers.ToolDefinition {
	if specs == nil {
		return nil
	}
	definitions := make([]providers.ToolDefinition, 0, len(specs))
	for _, spec := range specs {
		definitions = append(definitions, providers.ToolDefinition{Name: spec.ID, Description: spec.Description, InputSchema: spec.InputJSONSchema})
	}
	return definitions
}

func (r *run) autoCompact(ctx context.Context) (bool, error) {
	messages := r.history.all()
	base, window, err := r.Prompts.Budget(ctx)
	if err != nil {
		return false, err
	}
	recommended, _ := agentcontext.Recommendation(agentcontext.SessionTokens(base, messages), window)
	if !recommended || agentcontext.CompactBackoffActive(messages) {
		return false, nil
	}
	return r.compactHistory(ctx, messages, base)
}

func (r *run) compact(ctx context.Context) (bool, error) {
	base, _, err := r.Prompts.Budget(ctx)
	if err != nil {
		return false, err
	}
	return r.compactHistory(ctx, r.history.all(), base)
}

func (r *run) requestNeedsCompact(ctx context.Context, request providers.Request) bool {
	_, window, err := r.Prompts.Budget(ctx)
	if err != nil {
		return false
	}
	threshold := agentcontext.Threshold(window)
	return threshold > 0 && agentcontext.EstimateRequestTokens(request) >= threshold
}

func (r *run) compactHistory(ctx context.Context, messages []transcript.Message, base int) (bool, error) {
	if _, effective := agentcontext.LatestSummary(messages); len(effective) == 0 {
		return false, nil
	}
	content, err := agentcontext.Compact(ctx, summaryModel{r: r}, agentcontext.CompactInput{
		SessionID:    r.task.SessionID,
		History:      messages,
		BaseTokens:   base,
		PlanSnapshot: r.Prompts.PlanSnapshot(ctx),
	})
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	content = strings.TrimSpace(content)
	now := r.Now()
	marker := transcript.Message{
		ID:        r.NewID("msg"),
		SessionID: r.task.SessionID,
		Role:      transcript.MessageRoleSystem,
		Content:   content,
		Parts:     []transcript.MessagePart{{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: content}}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := r.history.append(ctx, marker); err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}
```

`buildRequest` carries Stage 1's only request field, `CacheKey`; `providers.ToolUseDisabled` still exists after Stage 1b, so `ToolUseAllowed` and the text-only branch keep it.

- [ ] **Step 7: `internal/agent/tools.go`**

```go
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// callRequest is one tool call the engine is about to run.
type callRequest struct {
	id         string
	name       string
	args       json.RawMessage
	workingDir string
	approved   bool
}

// executeBatch runs the response's tool calls in order; approval-bound calls park,
// the rest of the batch still runs.
func (r *run) executeBatch(ctx context.Context, response providers.Response) (bool, error) {
	seen := make(map[string]providers.ToolCall)
	waiting := false
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
		request := callRequest{id: id, name: name, args: toolCall.Arguments, workingDir: r.task.WorkingDir}
		pending, err := r.runCall(ctx, request)
		if err != nil {
			return false, err
		}
		waiting = waiting || pending
	}
	return waiting, nil
}

// resumeApproved runs granted calls that have no result yet and reports whether
// approvals of the run are still open.
func (r *run) resumeApproved(ctx context.Context) (bool, error) {
	pending, err := r.Approvals.Pending(ctx, r.task.RunID)
	if err != nil {
		return false, err
	}
	approved, err := r.Inbox.Drain(ctx, r.task.RunID, InputApproved)
	if err != nil {
		return false, err
	}
	for _, input := range approved {
		if r.history.hasResult(input.ToolCallID) {
			continue
		}
		request := callRequest{id: input.ToolCallID, name: input.ToolName, args: input.Args, workingDir: input.WorkingDir, approved: true}
		if _, err := r.runCall(ctx, request); err != nil {
			return false, err
		}
	}
	return pending, nil
}

// runCall authorizes, journals and executes one call; it reports whether the call
// now waits for approval.
func (r *run) runCall(ctx context.Context, req callRequest) (bool, error) {
	if req.id == "" {
		req.id = r.NewID("tool")
	}
	call := tools.Call{
		SessionID:   r.task.SessionID,
		RunID:       r.task.RunID,
		ToolCallID:  req.id,
		Client:      r.task.Client,
		ExternalKey: r.task.ExternalKey,
		WorkingDir:  req.workingDir,
		Approved:    req.approved,
		Args:        req.args,
	}
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

// rejectCall journals a call that may not run with its error as the result, so the
// model can correct it.
func (r *run) rejectCall(ctx context.Context, req callRequest, reason string) error {
	if err := r.history.append(ctx, r.callMessage(req, true)); err != nil {
		return err
	}
	_, err := r.appendResult(ctx, req, tools.Result{Content: reason, IsError: true})
	return err
}

func (r *run) finishCall(ctx context.Context, req callRequest, call tools.Call, result tools.Result) error {
	finished := ToolCallMessage(req.id, r.task.SessionID, r.task.RunID, req.name, req.args, true, r.Now())
	if existing, ok := r.history.message(req.id); ok {
		finished.CreatedAt = existing.CreatedAt
	}
	if err := r.history.finish(ctx, finished); err != nil {
		return err
	}
	message, err := r.appendResult(ctx, req, result)
	if err != nil {
		return err
	}
	r.Sink.Emit(Event{Kind: EventToolFinished, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name, ResultMessageID: message.ID, Result: result})
	if err := r.Tools.Finish(ctx, req.name, call, result, message); err != nil {
		return err
	}
	return r.checkpoint(ctx, PhaseModel, "", "")
}

// appendResult writes a tool result with any pending steer guidance merged into it.
func (r *run) appendResult(ctx context.Context, req callRequest, result tools.Result) (transcript.Message, error) {
	message, err := ToolResultMessage(r.NewID("tool_result"), r.task.SessionID, r.task.RunID, req.id, req.name, result, r.Now())
	if err != nil {
		return transcript.Message{}, err
	}
	steers, err := r.Inbox.Drain(ctx, r.task.RunID, InputSteer)
	if err != nil {
		return transcript.Message{}, err
	}
	for _, steer := range steers {
		appendUserGuidanceToToolResult(&message, steer.Text)
	}
	return message, r.history.append(ctx, message)
}

func (r *run) callMessage(req callRequest, finished bool) transcript.Message {
	return ToolCallMessage(req.id, r.task.SessionID, r.task.RunID, req.name, req.args, finished, r.Now())
}
```

Then move-copy verbatim into the end of this file: `sameRequestedTool` from `internal/core/execution_tools.go` (uses `bytes`, `json`, `reflect`, `providers`), and `appendUserGuidanceToToolResult` + `appendGuidanceBlock` from `internal/core/session_inputs.go` (types → `transcript.`). The core originals are deleted in Task 10.

- [ ] **Step 8: Run the engine tests**

Run: `go test ./internal/agent/... -count=1 -v`
Expected: all PASS (the retry test takes ~1s).

- [ ] **Step 9: Full suite**

Run: `gofmt -l internal; go build ./... && go vet ./... && go test ./...`
Expected: all `ok` (core still uses its own loop).

- [ ] **Step 10: Commit**

```bash
git add internal/agent/agenttest/agenttest.go internal/agent/engine_test.go internal/agent/engine.go \
  internal/agent/journal.go internal/agent/generate.go internal/agent/request.go internal/agent/tools.go
git commit -m "feat(agent): add the native engine loop over journal, tools, approvals and inbox ports

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Core implements the ports and `ExecuteRun` switches to the engine

**Files:**
- Create: `internal/core/run_execute.go`, `internal/core/run_outcome.go`, `internal/core/agent_journal.go`, `internal/core/agent_tools.go`, `internal/core/agent_inbox.go`, `internal/core/agent_sink.go`
- Modify: `internal/core/agent_prompts.go`, `internal/core/usage.go`, `internal/core/tool_call_prepare.go`, `internal/core/execution.go`, `internal/core/execution_status.go`, `internal/core/execution_turn.go`, `internal/core/execution_compaction.go`, `internal/core/run_recovery.go`

The characterization tests (Tasks 1–3), `native` generation tests and crash-recovery suite are the tests: they must pass **unchanged**.

- [ ] **Step 1: `internal/core/agent_journal.go`**

```go
package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// coreJournal persists engine writes through the store.
type coreJournal struct {
	c *Core
}

func (j coreJournal) Load(ctx context.Context, sessionID string) (agent.Window, error) {
	messages, err := j.c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return agent.Window{}, err
	}
	return agent.Window{Messages: messages}, nil
}

func (j coreJournal) Append(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := j.c.store.AppendMessage(ctx, message)
	if err != nil {
		return 0, err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return seq, nil
}

func (j coreJournal) BeginStreaming(ctx context.Context, message transcript.Message) (int64, error) {
	seq, err := j.c.saveMessageProgress(ctx, message)
	if err != nil {
		return 0, err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return seq, nil
}

func (j coreJournal) Stream(ctx context.Context, message transcript.Message) error {
	if err := j.c.updateMessageProgress(ctx, message); err != nil {
		return err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return nil
}

func (j coreJournal) FinishStreaming(ctx context.Context, message transcript.Message) error {
	if err := j.c.store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	_ = j.c.touchSubagentTaskActivity(ctx, message.RunID, message.UpdatedAt)
	return nil
}

func (j coreJournal) Checkpoint(ctx context.Context, state agent.State) error {
	return j.c.saveRunCheckpoint(ctx, state.RunID, RunCheckpointPhase(state.Phase), state.ToolCallID, state.ToolName)
}

func (j coreJournal) RecordStep(ctx context.Context, step agent.Step) error {
	j.c.recordRunStep(ctx, step)
	return nil
}
```

- [ ] **Step 2: `recordRunStep` takes an `agent.Step` (`internal/core/usage.go`)**

Replace Stage 0's `recordRunStep` with the version below; the `RunStep` field mapping is Stage 0 Task 7's, now read from `agent.Step`:

```go
// recordRunStep stores one generation of a run. A failed write is logged and
// never fails the run.
func (c *Core) recordRunStep(ctx context.Context, step agent.Step) {
	runID := normalizeText(step.RunID)
	if runID == "" || c.store == nil {
		return
	}
	row := RunStep{
		RunID:            runID,
		Model:            step.Model,
		Provider:         step.Provider,
		PromptTokens:     step.Usage.PromptTokens,
		CacheReadTokens:  step.Usage.CacheReadTokens,
		CacheWriteTokens: step.Usage.CacheWriteTokens,
		OutputTokens:     step.Usage.OutputTokens,
		ReasoningTokens:  step.Usage.ReasoningTokens,
		StopReason:       step.StopReason,
		LatencyMillis:    step.Latency.Milliseconds(),
		ToolCalls:        step.ToolCalls,
		CreatedAt:        c.now().UTC(),
	}
	if err := c.store.SaveRunStep(context.WithoutCancel(ctx), row); err != nil {
		log.Printf("core: record step of run %q: %v", runID, err)
	}
}
```

Update its two remaining old-loop callers so they still compile (both are deleted in Task 10): in `generateAssistantTurn` (`execution_turn.go`) the call becomes

```go
		c.recordRunStep(ctx, agent.Step{RunID: turn.RunID, Model: response.Model, Provider: response.Provider, Usage: response.Usage, StopReason: generationStopReason(response), Latency: time.Since(started), ToolCalls: len(response.ToolCalls)})
```

and in `runStepGenerator.Generate` (`execution_compaction.go`)

```go
		g.c.recordRunStep(ctx, agent.Step{RunID: g.runID, Model: response.Model, Provider: response.Provider, Usage: response.Usage, StopReason: "compact", Latency: time.Since(started), ToolCalls: len(response.ToolCalls)})
```

(import `internal/agent` in `usage.go`, `execution_turn.go`, `execution_compaction.go`. `usage.go` keeps its `providers` import for `generationStopReason` until Task 10 deletes that function.)

- [ ] **Step 3: Extract tool validation in `internal/core/tool_call_prepare.go`**

Replace the validation head of `prepareToolCall` with a call to a new function and keep the rest verbatim:

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
	if toolName == delegateTaskToolName && CoreSessionIsExternalAgent(session) {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: delegate_task is available for Matrixclaw sessions only", ErrInvalidInput)
	}
	if isSubagentSession(session) && !subagentToolAllowed(spec) {
		return Session{}, tools.Spec{}, fmt.Errorf("%w: tool %q is not available to child subagents", ErrInvalidInput, toolName)
	}
	return session, spec, nil
}

func (c *Core) prepareToolCall(ctx context.Context, input ExecuteToolInput) (preparedToolCall, error) {
	session, spec, err := c.checkToolCall(ctx, input.SessionID, input.ToolName)
	if err != nil {
		return preparedToolCall{}, err
	}
	sessionID := normalizeText(input.SessionID)
	toolName := normalizeText(input.ToolName)
	workingDir := normalizeWorkingDir(input.WorkingDir)
	if workingDir == "" {
		workingDir = session.WorkingDir
	}
	// ... from `toolCallID := normalizeText(input.ToolCallID)` to the end: unchanged
}
```

- [ ] **Step 4: `internal/core/agent_tools.go`**

```go
package core

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// nativeTurn is what the per-step prompt and tool list of a native run depend on.
type nativeTurn struct {
	RunID              string
	SessionID          string
	WorkingDir         string
	Subagent           bool
	ClientCapabilities ClientCapabilities
	ToolUse            bool
}

// coreTools is the Tools port of one native run.
type coreTools struct {
	c    *Core
	turn nativeTurn
}

func (t coreTools) Specs(ctx context.Context) []tools.Spec {
	specs := t.c.nativeToolSpecs(t.turn)
	for i := range specs {
		if specs[i].ID == delegateTaskToolName || specs[i].ID == spawnSubagentToolName {
			specs[i].Description = t.c.delegateTaskToolDescription(ctx, specs[i].Description)
		}
	}
	return specs
}

func (t coreTools) Authorize(ctx context.Context, name string, call tools.Call) (agent.Decision, error) {
	// Stage 0's isNewToolCallMessage check: a call ID owned by another session fails the run
	// with a clear error instead of a primary-key conflict on the first journal write.
	if existing, err := t.c.store.GetMessage(ctx, call.ToolCallID); err == nil && existing.SessionID != call.SessionID {
		return agent.Decision{}, fmt.Errorf("%w: tool call id %q already belongs to another session", ErrInvalidInput, call.ToolCallID)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return agent.Decision{}, err
	}
	_, _, err := t.c.checkToolCall(ctx, call.SessionID, name)
	if errors.Is(err, ErrInvalidInput) {
		return agent.Decision{Reason: err.Error()}, nil
	}
	if err != nil {
		return agent.Decision{}, err
	}
	return agent.Decision{Allowed: true}, nil
}

func (t coreTools) Execute(ctx context.Context, name string, call tools.Call) (tools.Result, error) {
	session, spec, err := t.c.checkToolCall(ctx, call.SessionID, name)
	if err != nil {
		return tools.Result{}, err
	}
	workingDir := normalizeWorkingDir(call.WorkingDir)
	if workingDir == "" {
		workingDir = session.WorkingDir
	}
	prepared := preparedToolCall{SessionID: call.SessionID, RunID: call.RunID, ToolName: name, Spec: spec, ToolCallID: call.ToolCallID, WorkingDir: workingDir}
	input := ExecuteToolInput{Client: call.Client, ExternalKey: call.ExternalKey, Approved: call.Approved, Args: call.Args}
	result, execErr := t.c.executeToolWithGrant(ctx, prepared, input)
	if result.Approval != nil && !call.Approved {
		return result, nil
	}
	if execErr != nil {
		result = tools.Result{Content: execErr.Error(), IsError: true}
	}
	return result, nil
}

func (t coreTools) Finish(ctx context.Context, name string, call tools.Call, result tools.Result, message transcript.Message) error {
	prepared := preparedToolCall{SessionID: call.SessionID, RunID: call.RunID, ToolName: name, ToolCallID: call.ToolCallID}
	if err := t.c.saveFileVersionSnapshot(ctx, prepared, result, message.CreatedAt); err != nil {
		return err
	}
	return t.c.recordSubagentResultMessage(ctx, result.Metadata, message.ID)
}

// nativeToolSpecs lists the tools a native run may see; nil without a registry.
func (c *Core) nativeToolSpecs(turn nativeTurn) []tools.Spec {
	if c.tools == nil {
		return nil
	}
	specs := c.tools.List()
	out := make([]tools.Spec, 0, len(specs))
	for _, spec := range specs {
		if turn.Subagent && !subagentToolAllowed(spec) {
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

- [ ] **Step 5: `internal/core/agent_inbox.go`**

```go
package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// coreInbox is the Inbox port of one native run.
type coreInbox struct {
	c       *Core
	session Session
}

func (in coreInbox) Drain(ctx context.Context, runID string, kind agent.InputKind) ([]agent.Input, error) {
	switch kind {
	case agent.InputSteer:
		return in.steers(ctx, runID)
	case agent.InputApproved:
		return in.approved(ctx, runID)
	default:
		return nil, fmt.Errorf("core: unknown inbox input %q", kind)
	}
}

func (in coreInbox) Canceled(ctx context.Context, runID string) (bool, error) {
	return in.c.isRunCanceled(ctx, runID)
}

// steers consumes the run's pending steer input in arrival order.
func (in coreInbox) steers(ctx context.Context, runID string) ([]agent.Input, error) {
	inputs, err := in.c.store.ListPendingSteerInputs(ctx, in.session.ID, runID)
	if err != nil {
		return nil, err
	}
	var out []agent.Input
	for _, input := range inputs {
		text := normalizeText(input.Text)
		if text == "" {
			continue
		}
		consumedAt := in.c.now().UTC()
		input.Status = SessionInputStatusConsumed
		input.ConsumedRunID = runID
		input.ConsumedAt = &consumedAt
		input.UpdatedAt = consumedAt
		if err := in.c.store.UpdateSessionInput(ctx, input); err != nil {
			return nil, err
		}
		in.c.publishSessionInputUpdated(input)
		out = append(out, agent.Input{Kind: agent.InputSteer, Text: text})
	}
	return out, nil
}

// approved returns granted approvals of the run whose tool call has no result yet.
func (in coreInbox) approved(ctx context.Context, runID string) ([]agent.Input, error) {
	approvals, err := in.c.store.ListApprovals(ctx, in.session.ID, ApprovalStateApproved)
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
		out = append(out, agent.Input{
			Kind:       agent.InputApproved,
			ToolCallID: callID,
			ToolName:   approval.ToolName,
			WorkingDir: workingDirForApprovalResume(in.session.WorkingDir, spec, approval.Path),
			Args:       args,
		})
	}
	return out, nil
}

// coreApprovals is the Approvals port of one native run.
type coreApprovals struct {
	c         *Core
	sessionID string
}

func (a coreApprovals) Request(ctx context.Context, p agent.Pending) error {
	request := p.Request
	prepared := preparedToolCall{SessionID: p.SessionID, RunID: p.RunID, ToolName: p.ToolName, ToolCallID: p.ToolCallID}
	_, _, _, err := a.c.createPendingApproval(ctx, prepared, ExecuteToolInput{}, tools.Result{Approval: &request}, nil)
	return err
}

func (a coreApprovals) Pending(ctx context.Context, runID string) (bool, error) {
	return a.c.runHasPendingApprovals(ctx, a.sessionID, runID)
}
```

This mirrors Stage 0's `resumeApprovedTools` / `replayApprovedTool` lookups (`HasToolResult`, `sessionToolCallMessage`, `toolCallArgs`).

- [ ] **Step 6: `internal/core/agent_sink.go`**

```go
package core

import "github.com/Suren878/matrixclaw/internal/agent"

// coreSink publishes engine events on the core event bus.
type coreSink struct {
	c *Core
}

func (s coreSink) Emit(event agent.Event) {
	switch event.Kind {
	case agent.EventMessageCreated:
		s.c.publishEvent(Event{Type: EventMessageCreated, SessionID: event.SessionID, RunID: event.RunID, Payload: event.Message})
	case agent.EventMessageUpdated:
		s.c.publishEvent(Event{Type: EventMessageUpdated, SessionID: event.SessionID, RunID: event.RunID, Payload: event.Message})
	case agent.EventToolRequested:
		s.c.publishToolUpdate(event.SessionID, event.RunID, ToolUpdate{
			ToolCallID: event.ToolCallID,
			ToolName:   event.ToolName,
			State:      ToolLifecycleRequested,
			RunID:      event.RunID,
			SessionID:  event.SessionID,
		})
	case agent.EventToolFinished:
		prepared := preparedToolCall{SessionID: event.SessionID, RunID: event.RunID, ToolName: event.ToolName, ToolCallID: event.ToolCallID}
		s.c.publishFinishedToolUpdate(prepared, event.ResultMessageID, event.Result)
	}
}
```

- [ ] **Step 7: `corePrompts` and the native prompt in `internal/core/agent_prompts.go`**

Append (imports: `context`, `strings`, `internal/agent/prompt`, `internal/transcript`):

```go
// corePrompts is the Prompts port of one native run.
type corePrompts struct {
	c    *Core
	turn nativeTurn
}

func (p corePrompts) System(ctx context.Context, summary string, history []transcript.Message) (string, string) {
	assistant := p.c.assistantProfile()
	return p.c.nativeSystemPrompt(ctx, p.turn, assistant, summary, history), assistant.CustomInstructions
}

func (p corePrompts) PlanSnapshot(ctx context.Context) string {
	return p.c.compactSessionPlanSnapshot(ctx, p.turn.SessionID)
}

func (p corePrompts) Budget(ctx context.Context) (int, int, error) {
	session, err := p.c.store.GetSession(ctx, p.turn.SessionID)
	if err != nil {
		return 0, 0, err
	}
	return p.c.contextBaseTokens(), p.c.sessionContextWindowTokens(session), nil
}

func (c *Core) nativeSystemPrompt(ctx context.Context, turn nativeTurn, assistant AssistantProfile, compactSummary string, history []transcript.Message) string {
	sections := []string{prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)}
	if checkpoint, ok, err := c.runCheckpoint(ctx, turn.RunID); err == nil && ok {
		if recoveryPrompt := runCheckpointRecoveryPrompt(checkpoint); recoveryPrompt != "" {
			sections = append(sections, recoveryPrompt)
		}
	}
	workingDir := strings.TrimSpace(turn.WorkingDir)
	if turn.Subagent {
		sections = append(sections, subagentSystemPrompt())
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
	if statusPrompt := c.nativeStatusPrompt(ctx, turn); statusPrompt != "" {
		sections = append(sections, statusPrompt)
	}
	if c.delegateTaskPromptAvailable() {
		sections = append(sections, c.delegateTaskGuidancePrompt(ctx))
	}
	if memoryPrompt := c.MemoryPromptContext(ctx, turn.WorkingDir); memoryPrompt != "" {
		sections = append(sections, memoryPrompt)
	}
	if compactSummary != "" {
		sections = append(sections, "Session context summary:\n"+compactSummary)
	}
	if planPrompt := c.sessionPlanPrompt(ctx, turn.SessionID); planPrompt != "" {
		sections = append(sections, planPrompt)
	}
	if skillsPrompt := c.nativeSkillsPrompt(ctx, turn, history); skillsPrompt != "" {
		sections = append(sections, skillsPrompt)
	}
	return prompt.JoinSections(sections...)
}

func (c *Core) nativeSkillsPrompt(ctx context.Context, turn nativeTurn, history []transcript.Message) string {
	if c == nil || c.skillsContext == nil {
		return ""
	}
	messages := make([]SkillsPromptMessage, 0, len(history))
	for _, message := range history {
		messages = append(messages, SkillsPromptMessage{Role: string(message.Role), Content: message.Content})
	}
	return c.skillsContext.SkillsPromptContext(ctx, SkillsPromptContextRequest{SessionID: turn.SessionID, RunID: turn.RunID, WorkingDir: turn.WorkingDir, Messages: messages})
}

func (c *Core) nativeStatusPrompt(ctx context.Context, turn nativeTurn) string {
	if c == nil || c.runtimeStatus == nil {
		return ""
	}
	return c.runtimeStatus.RuntimeStatusPromptContext(ctx, RuntimeStatusContextRequest{SessionID: turn.SessionID, RunID: turn.RunID, WorkingDir: turn.WorkingDir, ToolIDs: c.nativeStatusToolIDs(turn)})
}

func (c *Core) nativeStatusToolIDs(turn nativeTurn) []string {
	if c == nil || c.tools == nil || !turn.ToolUse {
		return nil
	}
	specs := c.nativeToolSpecs(turn)
	ids := make([]string, 0, len(specs))
	for _, spec := range specs {
		ids = append(ids, spec.ID)
	}
	return ids
}
```

This is `providerSystemPrompt` / `skillsPromptContext` / `runtimeStatusPromptContext` / `runtimeStatusToolIDs` with `runtimeToolUseAllowed(turn.Runtime)` replaced by `turn.ToolUse` and the Task 5 prompt names.

- [ ] **Step 8: `internal/core/run_outcome.go`**

```go
package core

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent"
)

// applyOutcome persists how the engine left a native run.
func (c *Core) applyOutcome(ctx context.Context, run Run, outcome agent.Outcome) error {
	switch outcome.Status {
	case agent.StatusCompleted:
		if outcome.Assistant == nil {
			return nil
		}
		return c.completeAssistantTurn(ctx, &run, run.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		return c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
	case agent.StatusCanceled:
		return c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusFailed:
		if outcome.MarkErrored && outcome.Assistant != nil {
			return c.persistAssistantError(ctx, run, outcome.Assistant, outcome.AssistantSaved, outcome.Err)
		}
		return c.failRunByID(ctx, run, outcome.Err)
	case agent.StatusInterrupted:
		return c.applyInterruptedOutcome(run, outcome)
	default:
		return fmt.Errorf("core: unknown run outcome %q", outcome.Status)
	}
}

// applyInterruptedOutcome commits what the run reached before its context stopped,
// or keeps it running with a recovery checkpoint.
func (c *Core) applyInterruptedOutcome(run Run, outcome agent.Outcome) error {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()
	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if latest.Status == RunStatusCanceled {
		return c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	}
	if subagentRunStatusTerminal(latest.Status) {
		return nil
	}
	switch outcome.Reached {
	case agent.StatusCompleted:
		return c.completeAssistantTurn(ctx, &latest, latest.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, latest.SessionID, latest.ID)
		if err != nil {
			return err
		}
		if pending {
			return c.setRunStatus(ctx, &latest, RunStatusWaitingApproval, "")
		}
	}
	return c.preserveRunForRecovery(ctx, latest, outcome.Assistant, outcome.AssistantSaved)
}
```

- [ ] **Step 9: Change `completeAssistantTurn` in place (`internal/core/execution_status.go`)**

The engine now finalizes the reply and records the step (with its usage refresh). Resulting function:

```go
func (c *Core) completeAssistantTurn(ctx context.Context, run *Run, sessionID string, assistant *transcript.Message, assistantSaved bool) error {
	if run == nil || assistant == nil {
		return errors.New("core: complete assistant turn requires run and assistant")
	}
	finishedAt := c.now().UTC()
	if err := c.CompleteSessionPlanRunStep(ctx, *run, assistant.Content); err != nil {
		return fmt.Errorf("complete plan run step: %w", err)
	}
	if err := c.completeActivePlanItemsIfRunFinished(ctx, sessionID); err != nil {
		return fmt.Errorf("complete active plan items: %w", err)
	}
	if !assistantSaved {
		assistant.CreatedAt = finishedAt
		assistant.UpdatedAt = finishedAt
		run.Status = RunStatusCompleted
		run.Error = ""
		run.FinishedAt = &finishedAt
		run.UpdatedAt = finishedAt
		if err := c.store.CompleteRun(ctx, *assistant, *run); err != nil {
			return fmt.Errorf("complete run: %w", err)
		}
		c.clearRunCheckpoint(ctx, run.ID)
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, RunID: run.ID, Payload: *assistant})
		c.publishEvent(Event{Type: EventRunUpdated, SessionID: sessionID, RunID: run.ID, Payload: *run})
		return nil
	}
	assistant.UpdatedAt = finishedAt
	run.Status = RunStatusCompleted
	run.Error = ""
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt
	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return fmt.Errorf("update assistant message: %w", err)
	}
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	c.clearRunCheckpoint(ctx, run.ID)
	c.publishEvent(Event{Type: EventMessageUpdated, SessionID: sessionID, RunID: run.ID, Payload: *assistant})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: sessionID, RunID: run.ID, Payload: *run})
	return nil
}
```

Drop the last argument (`result.Response`) at its two old-loop call sites (`applyRunTurnResult` in `execution.go`, `applyRunTurnResultAfterContextStopped` in `run_recovery.go`); both are dead after this task and deleted in Task 10.

- [ ] **Step 10: `internal/core/run_execute.go` and remove the old entry point**

```go
package core

import (
	"context"
	"log"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/providers"
)

// ExecuteRun claims a run and executes it with its external agent or the native engine.
func (c *Core) ExecuteRun(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	runCtx, unregisterRun, claimed := c.activeRunContext(ctx, runID)
	if !claimed {
		return nil
	}
	defer unregisterRun()
	defer func() {
		if err := c.afterRunExecution(context.Background(), runID); err != nil {
			log.Printf("core: after run execution for %q failed: %v", runID, err)
		}
	}()
	ready, err := c.prepareClaimedRun(ctx, runID)
	if err != nil || !ready {
		return err
	}
	if handled, err := c.tryExecuteExternalAgentRun(ctx, runCtx, runID); handled || err != nil {
		return err
	}
	run, session, runtime, ok, err := c.prepareNativeRun(ctx, runID)
	if err != nil || !ok {
		return err
	}
	task, engine := c.nativeEngine(run, session, runtime)
	outcome, err := engine.Run(runCtx, task)
	if err != nil {
		return err
	}
	return c.applyOutcome(ctx, run, outcome)
}

// prepareNativeRun marks an accepted native run as running and resolves its model.
func (c *Core) prepareNativeRun(ctx context.Context, runID string) (Run, Session, providers.Runtime, bool, error) {
	run, err := c.store.GetRun(ctx, normalizeText(runID))
	if err != nil {
		return Run{}, Session{}, nil, false, err
	}
	switch run.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
		return Run{}, Session{}, nil, false, nil
	case RunStatusRunning:
		return Run{}, Session{}, nil, false, c.failOrphanedRun(ctx, run)
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return Run{}, Session{}, nil, false, c.failRunByID(ctx, run, err)
	}
	session = c.decorateSessionLLM(session)
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return Run{}, Session{}, nil, false, c.failRunByID(ctx, run, err)
	}
	if err := c.setRunStatus(ctx, &run, RunStatusRunning, ""); err != nil {
		return Run{}, Session{}, nil, false, err
	}
	return run, session, runtime, true, nil
}

// nativeEngine builds the engine and task of one native run.
func (c *Core) nativeEngine(run Run, session Session, runtime providers.Runtime) (agent.Task, *agent.Engine) {
	turn := nativeTurn{
		RunID:              run.ID,
		SessionID:          session.ID,
		WorkingDir:         session.WorkingDir,
		Subagent:           isSubagentSession(session),
		ClientCapabilities: run.ClientCapabilities,
		ToolUse:            agent.ToolUseAllowed(runtime),
	}
	engine := agent.New(agent.Config{
		Journal:     coreJournal{c: c},
		Tools:       coreTools{c: c, turn: turn},
		Approvals:   coreApprovals{c: c, sessionID: session.ID},
		Inbox:       coreInbox{c: c, session: session},
		Sink:        coreSink{c: c},
		Prompts:     corePrompts{c: c, turn: turn},
		Attachments: c.attachments,
		Now:         func() time.Time { return c.now().UTC() },
		NewID:       c.newID,
	})
	task := agent.Task{
		RunID:       run.ID,
		SessionID:   session.ID,
		Client:      run.Client,
		ExternalKey: run.ExternalKey,
		WorkingDir:  session.WorkingDir,
		Model:       runtime,
	}
	return task, engine
}
```

Delete `ExecuteRun` and `prepareRunExecution` from `internal/core/execution.go` (the rest of that file becomes dead and is deleted in Task 10).

- [ ] **Step 11: Run the characterization and recovery tests unchanged**

Run: `go test ./internal/core/ -count=1 -run 'Native|Approval|Steer|Cancel|Compact|ContextLength|MaxOutput|Subagent|Recover|Graceful|Startup|Tool|Empty|Repeated|Unknown|ModelFailures|Persisted'`
Expected: PASS without editing any test.

- [ ] **Step 12: Full suite and commit**

Run: `gofmt -l internal; go build ./... && go vet ./... && go test ./... -count=1`
Expected: all `ok`.

```bash
git add internal/core/run_execute.go internal/core/run_outcome.go internal/core/agent_journal.go \
  internal/core/agent_tools.go internal/core/agent_inbox.go internal/core/agent_sink.go \
  internal/core/agent_prompts.go internal/core/usage.go internal/core/tool_call_prepare.go \
  internal/core/execution.go internal/core/execution_status.go internal/core/execution_turn.go \
  internal/core/execution_compaction.go internal/core/run_recovery.go
git commit -m "refactor(core): execute native runs with the agent engine

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Delete the old loop

**Files:**
- Create: `internal/core/run_status.go`
- Delete: `internal/core/execution.go`, `execution_turn.go`, `execution_request.go`, `execution_generation.go`, `execution_status.go`, `execution_tools.go`, `execution_compaction.go`, `assistant_sanitize.go`, `message_progress_test.go`
- Move: `internal/core/execution_tools_test.go` → `internal/agent/tools_internal_test.go`; `internal/core/execution_generation_test.go` → `internal/core/native_run_test.go`
- Modify: `internal/core/run_outcome.go`, `internal/core/plan.go`, `internal/core/tool_approvals.go`, `internal/core/run_execute.go`, `internal/core/agent_tools.go`, `internal/core/run_recovery.go`, `internal/core/session_inputs.go`, `internal/core/tool_call_prepare.go`, `internal/core/tool_call_finish.go`, `internal/core/tool_helpers.go`

- [ ] **Step 1: Move the survivors out of the old files (verbatim)**

- `internal/core/run_status.go` (`package core`): `orphanedRunError`, `failOrphanedRun`, `persistAssistantError` (from `execution.go`); `setRunStatus`, `markAssistantErrored`, `saveAssistantErrored`, `appendErrorFinishPart`, `appendCanceledFinishPart`, `isRunCanceled`, `finishCanceledAssistant`, `failAcceptedRun`, `failRunByID` (from `execution_status.go`).
- `completeAssistantTurn` → end of `internal/core/run_outcome.go`.
- `planRunLooksBlocked`, `completeDonePlanParents`, `allPlanChildrenTerminal`, `completeActivePlanItemsIfRunFinished` → `internal/core/plan.go`.
- `runHasPendingApprovals` → `internal/core/tool_approvals.go`; `resolveSessionRuntime` → `internal/core/run_execute.go`.
- `clientSupportsVoiceDelivery`, `clientSupportsDocumentDelivery` → `internal/core/agent_tools.go`.

- [ ] **Step 2: Delete dead code outside the old files**

- `internal/core/run_recovery.go`: delete `applyRunTurnResultAfterContextStopped`.
- `internal/core/session_inputs.go`: delete `drainPendingSteersIntoLatestToolResult`, `injectPendingSteersIntoToolResultMessage`, `messageHasToolResult`, `appendUserGuidanceToToolResult`, `appendGuidanceBlock`.
- `internal/core/tool_call_prepare.go`: delete `newToolCallMessage`; in `prepareToolCall` use `agent.ToolCallMessage(toolCallID, sessionID, runID, toolName, input.Args, false, c.now().UTC())`.
- `internal/core/tool_call_finish.go`: in `finishToolCall` use `agent.ToolCallMessage(prepared.ToolCallID, prepared.SessionID, prepared.RunID, prepared.ToolName, input.Args, true, prepared.Message.CreatedAt)`; in `publishFinishedToolUpdate` use `agent.ToolResultStatus(result)`; replace `saveToolResultMessage` with:

```go
func (c *Core) saveToolResultMessage(ctx context.Context, prepared preparedToolCall, result tools.Result) (*transcript.Message, error) {
	message, err := agent.ToolResultMessage(c.newID("tool_result"), prepared.SessionID, prepared.RunID, prepared.ToolCallID, prepared.ToolName, result, c.now().UTC())
	if err != nil {
		return nil, err
	}
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return nil, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: prepared.SessionID, RunID: message.RunID, Payload: message})
	return &message, nil
}
```

- `internal/core/tool_helpers.go`: delete `toolResultStatus` (keep `normalizeToolContent` until Task 12, `marshalJSONRaw`, `errorText`, `toolResultCallIDs` stay).
- `internal/core/usage.go`: delete `generationStopReason` (the engine's `stepStopReason` replaced it) and the now-unused `providers` import.
- `internal/core/session_context.go`: with the old loop gone nothing compacts on behalf of a run from core, so fold `compactSession` back into `CompactSession` (manual `/compact` records no step, as in Stage 0):

```go
// CompactSession summarises the session history into a compaction marker.
func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return CompactSessionResult{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	if _, effective := agentcontext.LatestSummary(messages); len(effective) == 0 {
		return CompactSessionResult{}, ErrInvalidInput
	}
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return CompactSessionResult{}, err
	}
	content, err := agentcontext.Compact(ctx, runtime, agentcontext.CompactInput{
		SessionID:    session.ID,
		History:      messages,
		BaseTokens:   c.contextBaseTokens(),
		PlanSnapshot: c.compactSessionPlanSnapshot(ctx, session.ID),
	})
	if err != nil {
		return CompactSessionResult{}, err
	}
	message, err := c.CreateSystemMessage(ctx, session.ID, content)
	if err != nil {
		return CompactSessionResult{}, err
	}
	nextMessages, err := c.store.ListMessages(ctx, session.ID, 0)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(session, nextMessages)}, nil
}
```

(`runStepGenerator` goes away with `execution_compaction.go` in Step 4.)

- [ ] **Step 3: Move and delete the test files**

```bash
git mv internal/core/execution_tools_test.go internal/agent/tools_internal_test.go
sed -i 's/^package core$/package agent/' internal/agent/tools_internal_test.go
git mv internal/core/execution_generation_test.go internal/core/native_run_test.go
git rm -q internal/core/message_progress_test.go
```

(`message_progress_test.go` tested the old `generateAssistantTurn`; `TestStreamingProgressIsBatched` in `internal/agent/engine_test.go` covers the same behaviour. `native_run_test.go` content is unchanged.)

- [ ] **Step 4: Delete the old loop**

```bash
git rm -q internal/core/execution.go internal/core/execution_turn.go internal/core/execution_request.go \
  internal/core/execution_generation.go internal/core/execution_status.go internal/core/execution_tools.go \
  internal/core/execution_compaction.go internal/core/assistant_sanitize.go
```

- [ ] **Step 5: Verify nothing old is left**

Run:
```bash
ls internal/core | grep -E '^(execution|context)' ; \
grep -rn "maxRunToolSteps\|turnExecution\|IsPlanRunPromptMessage\|latestCompactSummary\|generationStopReason\|runStepGenerator\|compactSession(\|ContextClearedMessageContent\|IsProviderSupportedImageMIMEType" --include=*.go internal clients
```
Expected: no output.

Run: `gofmt -l internal clients; go build ./... && go vet ./... && go test ./... -count=1`
Expected: all `ok`; the characterization tests are unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/core/run_status.go internal/core/run_outcome.go internal/core/plan.go internal/core/tool_approvals.go \
  internal/core/run_execute.go internal/core/agent_tools.go internal/core/run_recovery.go internal/core/session_inputs.go \
  internal/core/tool_call_prepare.go internal/core/tool_call_finish.go internal/core/tool_helpers.go \
  internal/core/usage.go internal/core/session_context.go \
  internal/agent/tools_internal_test.go internal/core/native_run_test.go
git status --short internal/core internal/agent
git commit -m "refactor(core): delete the pre-engine native loop

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

The deletions and renames are already staged by the `git rm`/`git mv` of Steps 3–4, so no path is added by pattern. Before committing, `git status --short internal/core internal/agent` must show only `D`, `R`, `M` and `A` entries for the files this task names; anything else belongs to another session and stays unstaged.

---

### Task 11: Reschedule interrupted runs

**Files:**
- Modify: `internal/core/core.go`, `internal/core/run_execute.go`, `internal/core/run_outcome.go`, `internal/daemoncmd/run.go`
- Test: `internal/core/native_run_characterization_test.go` is **not** changed; new tests go to `internal/core/run_reschedule_test.go`

- [ ] **Step 1: Write the failing tests** — `internal/core/run_reschedule_test.go`

```go
package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func interruptNativeRun(t *testing.T, app *core.Core, runID string, runtime *interruptibleRecoveryRuntime) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(ctx, runID) }()
	waitRecoverySignal(t, runtime.started, "native generation start")
	cancel()
	if err := waitRecoveryError(t, done, "interrupted run"); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
}

func TestInterruptedNativeRunIsRescheduledWhileTheDaemonRuns(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	runtime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	_, run := saveCrashRecoveryRun(t, db, "reschedule", core.RunStatusAccepted, false)

	interruptNativeRun(t, app, run.ID, runtime)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusRunning)
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("reschedules = %d, want 1", got)
	}
}

func TestInterruptedNativeRunWaitsForStartupRecoveryAfterShutdown(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	lifetime, stop := context.WithCancel(context.Background())
	stop()
	app.WithLifetime(lifetime)
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	runtime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	_, run := saveCrashRecoveryRun(t, db, "shutdown", core.RunStatusAccepted, false)

	interruptNativeRun(t, app, run.ID, runtime)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusRunning)
	if got := starter.count(run.ID); got != 0 {
		t.Fatalf("reschedules = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/core/ -run 'TestInterruptedNativeRun' -count=1`
Expected: FAIL to compile (`app.WithLifetime undefined`).

- [ ] **Step 3: Implement**

`internal/core/core.go`: add field `lifetime context.Context` to `Core`, set `lifetime: context.Background()` in `New`, and:

```go
// WithLifetime sets the daemon lifetime; interrupted runs are rescheduled only while it is alive.
func (c *Core) WithLifetime(ctx context.Context) *Core {
	if ctx != nil {
		c.lifetime = ctx
	}
	return c
}
```

`internal/core/run_outcome.go`: `applyOutcome` and `applyInterruptedOutcome` return `(bool, error)` — `true` only when the run was kept for recovery. Change every `return X` in `applyOutcome` to `return false, X` except the interrupted case (`return c.applyInterruptedOutcome(run, outcome)`), and end `applyInterruptedOutcome` with:

```go
	if err := c.preserveRunForRecovery(ctx, latest, outcome.Assistant, outcome.AssistantSaved); err != nil {
		return false, err
	}
	current, err := c.store.GetRun(ctx, latest.ID)
	if err != nil {
		return false, err
	}
	return !subagentRunStatusTerminal(current.Status), nil
```

(every earlier `return X` in it becomes `return false, X`).

`internal/core/run_execute.go`: in `ExecuteRun` register the reschedule before the claim so it runs after `unregisterRun`:

```go
func (c *Core) ExecuteRun(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	reschedule := false
	defer func() {
		if reschedule {
			c.rescheduleInterruptedRun(runID)
		}
	}()
	runCtx, unregisterRun, claimed := c.activeRunContext(ctx, runID)
	// ... unchanged down to engine.Run ...
	outcome, err := engine.Run(runCtx, task)
	if err != nil {
		return err
	}
	reschedule, err = c.applyOutcome(ctx, run, outcome)
	return err
}

// rescheduleInterruptedRun hands a run kept for recovery back to the run starter
// while the daemon keeps running.
func (c *Core) rescheduleInterruptedRun(runID string) {
	if c.lifetime.Err() != nil {
		return
	}
	if err := c.startRun(context.Background(), runID); err != nil {
		log.Printf("core: reschedule interrupted run %q failed: %v", runID, err)
	}
}
```

`internal/daemoncmd/run.go`: right after `defer func() { _ = runStarter.Close() }()` add

```go
	lifetime, stopLifetime := context.WithCancel(ctx)
	defer stopLifetime()
	app.WithLifetime(lifetime)
```

(deferred after the adapter's `Close`, so it runs first: runs interrupted by the adapter shutdown are left to startup recovery.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/core/ -count=1 && go build ./... && go vet ./... && go test ./...`
Expected: all `ok`; `TestGracefulExecutorStopPreservesAndRecoversNativeGeneration` still passes (no starter configured, the reschedule attempt is only logged).

- [ ] **Step 5: Commit**

```bash
git add internal/core/core.go internal/core/run_execute.go internal/core/run_outcome.go \
  internal/core/run_reschedule_test.go internal/daemoncmd/run.go
git commit -m "feat(core): reschedule interrupted native runs while the daemon runs

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Async subagent completion no longer rewrites the parent's tool result

**Files:**
- Modify: `internal/core/subagents_lifecycle.go`, `internal/core/tool_approvals.go`, `internal/core/subagents_presentation.go`, `internal/core/tool_helpers.go`
- Test: `internal/core/subagents_async_result_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package core_test

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestAsyncSubagentCompletionKeepsTheSpawnResult(t *testing.T) {
	scenario := runAsyncSubagentScenario(t)

	var spawnResult string
	for _, message := range sessionMessages(t, scenario.db, scenario.session.ID) {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == "call-spawn" {
				spawnResult = message.Content
			}
		}
	}
	if !strings.Contains(spawnResult, " started") || strings.Contains(spawnResult, " finished") {
		t.Fatalf("spawn result was rewritten: %q", spawnResult)
	}
	finished := false
	for _, event := range scenario.events {
		if update, ok := event.Payload.(core.ToolUpdate); ok && update.ToolCallID == "call-spawn" && update.State == core.ToolLifecycleCompleted {
			finished = true
		}
	}
	if !finished {
		t.Fatal("no completed tool update for the spawn call")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/core/ -run TestAsyncSubagentCompletionKeepsTheSpawnResult -count=1`
Expected: FAIL, "spawn result was rewritten" (content says "Subagent … finished").

- [ ] **Step 3: Implement**

In `internal/core/subagents_lifecycle.go` replace `updateSubagentResultMessage` with:

```go
// publishSubagentToolUpdate tells clients the parent's spawn_subagent call finished;
// the result itself reaches the parent through the completion run.
func (c *Core) publishSubagentToolUpdate(task SubagentTask) {
	resultMessageID := normalizeText(task.ResultMessageID)
	if resultMessageID == "" {
		return
	}
	c.publishToolUpdate(task.ParentSessionID, task.ParentRunID, ToolUpdate{
		ToolCallID:      task.ParentToolCallID,
		ToolName:        spawnSubagentToolName,
		State:           subagentTaskToolLifecycleState(task),
		ResultStatus:    string(subagentTaskToolResultStatus(task)),
		RunID:           task.ParentRunID,
		SessionID:       task.ParentSessionID,
		ResultMessageID: resultMessageID,
		Error:           task.Error,
	})
}
```

In `syncAsyncSubagentTaskAfterRun` replace `if err := c.updateSubagentResultMessage(ctx, task); err != nil { return err }` with `c.publishSubagentToolUpdate(task)`; in `resolveSubagentApprovalBridge` (`tool_approvals.go`, async denial branch) the same. Delete `subagentFinishedResultContent` (`subagents_presentation.go`) and `normalizeToolContent` (`tool_helpers.go`), now unused.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: all `ok`, including `TestAsyncSubagentCompletionStartsParentFollowUpRun`.

- [ ] **Step 5: Commit**

```bash
git add internal/core/subagents_lifecycle.go internal/core/tool_approvals.go internal/core/subagents_presentation.go \
  internal/core/tool_helpers.go internal/core/subagents_async_result_test.go
git commit -m "fix(core): stop rewriting the parent's spawn_subagent result on completion

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Reject `/compact` and `/clear` while a run executes

**Files:**
- Modify: `internal/core/errors.go`, `internal/core/session_context.go`, `internal/core/system_messages.go`, `internal/agent/context/markers.go`, `internal/api/respond.go`
- Test: `internal/core/context_guard_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package core_test

import (
	"context"
	"errors"
	"testing"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestContextMarkersAreRejectedWhileANativeRunExecutes(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	session, run := saveCrashRecoveryRun(t, db, "guard", core.RunStatusAccepted, false)
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(ctx, run.ID) }()
	waitRecoverySignal(t, runtime.started, "native generation start")

	if _, err := app.CompactSession(ctx, session.ID); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("CompactSession error = %v, want ErrRunActive", err)
	}
	if _, err := app.CreateSystemMessage(ctx, session.ID, agentcontext.ClearedMarkerContent()); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("clear marker error = %v, want ErrRunActive", err)
	}
	if _, err := app.CreateSystemMessage(ctx, session.ID, "Model changed."); err != nil {
		t.Fatalf("plain system notice: %v", err)
	}

	if _, err := app.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled run"); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/core/ -run TestContextMarkersAreRejected -count=1`
Expected: FAIL to compile (`core.ErrRunActive` undefined).

- [ ] **Step 3: Implement**

`internal/core/errors.go`: add `ErrRunActive = errors.New("run is active")`.

`internal/agent/context/markers.go`:

```go
// IsMarker reports whether system message content is a compaction or clear marker.
func IsMarker(content string) bool {
	content = strings.TrimSpace(content)
	return strings.HasPrefix(content, compactMessagePrefix) || strings.HasPrefix(content, contextClearedMessagePrefix)
}
```

`internal/core/session_context.go`:

```go
// sessionRunExecuting reports whether a run of the session is executing in this daemon.
func (c *Core) sessionRunExecuting(ctx context.Context, sessionID string) (bool, error) {
	run, err := c.store.GetActiveRunBySession(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return c.runIsActive(run.ID), nil
}
```

In `CompactSession`, right after the empty-ID check, add:

```go
	if executing, err := c.sessionRunExecuting(ctx, sessionID); err != nil {
		return CompactSessionResult{}, err
	} else if executing {
		return CompactSessionResult{}, fmt.Errorf("%w: wait for the current run to finish before compacting", ErrRunActive)
	}
```

In `CreateSystemMessage` (`system_messages.go`), after the `GetSession` check:

```go
	if agentcontext.IsMarker(content) {
		if executing, err := c.sessionRunExecuting(ctx, sessionID); err != nil {
			return transcript.Message{}, err
		} else if executing {
			return transcript.Message{}, fmt.Errorf("%w: wait for the current run to finish before clearing context", ErrRunActive)
		}
	}
```

`internal/api/respond.go` `statusForCoreError`: add `case errors.Is(err, core.ErrRunActive): return http.StatusConflict`.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: all `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/core/errors.go internal/core/session_context.go internal/core/system_messages.go \
  internal/agent/context/markers.go internal/api/respond.go internal/core/context_guard_test.go
git commit -m "feat(core): reject compaction and clear while a session run executes

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: Final verification

- [ ] **Step 1: Static checks**

Run:
```bash
gofmt -l internal clients cmd
go vet ./...
go test ./... -count=1
go test ./internal/core/ ./internal/agent/... -count=5
```
Expected: no gofmt output, vet clean, all `ok` five times (flake check for the timing-based tests).

- [ ] **Step 2: Boundary checks**

```bash
go list -deps ./internal/agent/... | grep matrixclaw/internal/core    # expect no output
grep -rn "c.store\.\(SaveMessage\|UpdateMessage\)" internal/core/*.go | grep -v _test
```
Expected: `internal/agent` does not depend on `core`. Remaining transcript writes in core are only: `CreateSystemMessage`, run acceptance, run-less `ExecuteTool`/`finishToolCall`/`saveToolResultMessage`, crash recovery seals, outcome application (`completeAssistantTurn`, `markAssistantErrored`, `saveAssistantErrored`, `finishCanceledAssistant`, `preserveRunForRecovery`), external agents, and `coreJournal`.

- [ ] **Step 3: Manual smoke test on the test stand (owner-run)**

Build and run the daemon + TUI as usual for the stand, then in one session: ask for a multi-step task that reads files and runs a command needing approval; approve it; steer once while a tool runs; `/compact` during the run (expect the 409 message), then after it; delegate a small subtask to a matrixclaw subagent; cancel a second run mid-stream. Confirm the transcript, tool cards and run status look like before the change.

---

## Self-review

**Spec coverage (Stage 2a row + contract):** `internal/agent` with `Engine.Run(ctx, Task) (Outcome, error)` — Task 8. Ports Model/Journal/Tools/Approvals/Inbox/Sink (+ Prompts) — Task 7, deviations §1. `agenttest.ScriptedModel` + fakes — Tasks 7/8. In-memory single-writer journal — Task 8 (`history`). Core adapters — Task 9. `ExecuteRun` switched — Task 9. `execution*.go` and `context*.go` deleted after the move — Tasks 5, 6, 10 (context API kept in `session_context.go`, deviation 9). Moved code under `internal/agent`, `internal/agent/context`, `internal/agent/prompt` — Tasks 5–8. `interrupted` + rescheduling via `startRun` — Tasks 8, 11. 32-step failure, retry, compaction thresholds/markers, approval park/resume, crash recovery, streaming, blocking/async subagents, external agents — pinned by Tasks 1–3 and existing suites, unchanged through Tasks 9–10. Writers that mutated history: steer injection (B1, Tasks 8–10), `updateSubagentResultMessage` (B3, Task 12), steer-merged tool result rewrite (B2, Task 8; `attachReasoningToToolCallMessage` was already removed by Stage 1 Task 4), streaming flush (`BeginStreaming/Stream/FinishStreaming`, Task 8), auto-compaction marker (engine-appended, Task 8), `/compact`/`/clear` (B4, Task 13). Not in 2a by design: budget/final turn/loop guard/`stop_reason` (2b), seq boundary and stable prompt (3), denial returned to the model (4a), permissions and `Authorize` dry-run (4b), scheduler and engine-only checkpoints (4c), todo (5), tasks/await (6), cancel cascade (6c).

**Placeholder scan:** The only deferred content is explicitly verbatim-moved code (exact source function → destination, identifier changes listed). Stage 0/1/1b names and bodies (`generationStopReason` → `stepStopReason`, `stopReasonError` → `agentcontext.StopReasonError`, `responseReasoningParts` → `reasoningParts`, `CacheKey`, `ToolUseDisabled`) are written out from their plans. No TBD/TODO.

**Type consistency:** `agent.Step` has no step number (the store assigns it) and a string `StopReason`; `recordRunStep(ctx, agent.Step)` is used by `coreJournal.RecordStep` and the two old-loop callers patched in Task 9. `Journal.Append`/`BeginStreaming` return the seq from `AppendMessage`/`SaveMessageProgress` (Task 4). `agent.Outcome` has no `Response` field; `completeAssistantTurn(ctx, run, sessionID, assistant, saved)` is used with 5 arguments in Tasks 9–11. `Tools.Finish(ctx, name, call, result, message)` matches `coreTools.Finish`, `agenttest.Tools.Finish` and `run.finishCall`. `Inbox.Drain(ctx, runID, kind)` matches `coreInbox` and `agenttest.Inbox`. `Prompts.Budget` returns `(int, int, error)` everywhere. `applyOutcome`/`applyInterruptedOutcome` return `error` in Task 9 and `(bool, error)` from Task 11 on. `agentcontext.Threshold`, `Recommendation`, `SessionTokens`, `EstimateRequestTokens`, `LatestMarker`, `LatestSummary`, `LatestSummaryForRun`, `CompactBackoffActive`, `Compact`, `StopReasonError`, `ClearedMarkerContent`, `IsMarker` are defined in Tasks 6/13 before use. `agent.ToolCallMessage`, `ToolResultMessage`, `ToolResultStatus`, `ToolUseAllowed` are defined in Tasks 7/8 before core uses them in Tasks 9/10.

## Risks

1. **Stage 0/1 coupling** (`recordRunStep`/`generationStopReason` bodies after Stage 1, `insertMessage` callers, `stopReasonError` sites, `responseReasoningParts`, request fields, Stage 1's `tool_step_conversation_test.go`). Mitigation: the Task 0 table states each one as the earlier plans leave it, and every coupling point is a single, named place in this plan; a mismatch means the earlier stage deviated and must be reconciled first.
2. **Hidden ordering dependencies of the conversation builder** (Stage 1 groups parallel calls by persisted order). The engine writes call/result messages in exactly today's order; `TestToolRoundTripIsJournaledInOrder` and the event-trace characterization pin it.
3. **B1 steer timing**: guidance arriving during the model's final text generation is re-queued as a new run instead of being injected into an old tool result. Visible only in that window.
4. **B5 foreign writes during a run** are invisible to that run (run-less tools, notices). Rare; next run sees them.
5. **Rescheduling side effects (B6)**: a blocking child interrupted by its parent's cancel is rescheduled and runs to completion (today it did so after the next restart); rescheduled runs consume the 8-attempt recovery budget and carry the "daemon restarted" notice. Production daemon ctx is never cancelled (no signal handling), so `WithLifetime` only guards the adapter-close path.
6. **go-workflows activity cancellation semantics** (when it cancels an activity while the worker lives) are not documented here; the reschedule path is exercised only by tests with direct `ExecuteRun`.
7. **`INSERT … RETURNING`** needs SQLite ≥ 3.35 (the bundled `modernc.org/sqlite` is newer); every `insertMessage` caller in `internal/store` must switch to the two-value form in the same commit or the build breaks.
8. **Timing-based tests** (retry backoff ≈1 s, async subagent polling ≤10 s, fixed-clock event trace) may flake on a loaded CI; Task 14 runs them five times.
9. **Temporary duplication** between Tasks 7–8 and Task 10 (sanitizer, `sameRequestedTool`, guidance helpers, usage finish part, `reasoningParts` = Stage 1's `responseReasoningParts`) is intentional and removed in Task 10; do not stop between those tasks for long.
