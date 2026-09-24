# Stage 2b — Budget, Final Turn and /continue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A native run stops on an explicit budget (steps, active time, tokens) or on a no-progress loop with a tool-less final turn, ends `completed` with a `stop_reason`, keeps its counters across approval parks and restarts, continues replies cut by the output limit, and can be continued with `/continue`; budgets are shown and set with `/budget`.

**Architecture:** The engine (`internal/agent`) owns all loop logic as pure code tested with `agenttest`: `budget.go` (limits, wrap-up note, final turn), `loopguard.go` (no-progress detector), `output_limit.go` (cut replies), `notes.go` (engine messages). Its progress lives in `agent.Counters`, which travels in every `agent.State` checkpoint and is stored by core as JSON in `run_checkpoints.engine_state`. Core picks the run's `agent.Budget` from daemon defaults per trigger plus per-session overrides, stores `stop_reason` / `continues_run_id` / `trigger_kind` on `runs`, and exposes `/continue` and `/budget` through the API, `daemonclient`, `clientruntime` and `controlplane`, which TUI and Telegram share.

**Tech Stack:** Go 1.26, SQLite (modernc, `ensureColumn` migrations), bubbletea TUI, Telegram Bot API, Swift package (iOS models).

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session commits to this repo in parallel (HEAD moved from `689ba5c` to `6e6c660` while this plan was written): **locate code by function name, not line number**, run `git status --short` before each commit and stage only the paths listed in the task with explicit `git add <paths>` (never `-A`, `-u` or directories).
- Run `gofmt -w` on every Go file you touch (the code blocks below are not guaranteed to be column-aligned). Every commit must pass `go build ./... && go vet ./... && go test ./...` (run the full suite before committing).
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger.

## Decisions taken in this plan (owner should know)

1. **Engine messages are `role: system` + `origin: engine`.** Clients already render system rows as notes, and nothing that looks for user turns (auto titles, subagent auto-resume, steer) mistakes them for the user. The conversation builder sends them to the model as `user` text (Anthropic merges consecutive user turns; the other adapters accept them).
2. **Completion check is deferred to Stage 5**, where `todo` and `session_todos` appear. A port that always returns "no open items" would be dead code. `runs.continues_run_id` is stored now, so Stage 5 can scope the check to the run chain.
3. **Default step limit stays 32 for every trigger** (spec row 2b). Time limits apply now: user 4 h, subagent 1 h, automation / auto-wake 30 min; tokens 0 = unlimited. Stage 3 raises steps to 300 / 100 / 50 in `core.DefaultRunBudgets`.
4. **Trigger is stored** in `runs.trigger_kind`: empty = user message, `automation` = every run accepted through `AcceptTriggeredRun` (scheduled jobs and subagent-completion auto-wake). Subagent runs are recognised by their session (`isSubagentSession`).
5. **What counts:** a *step* is one main-loop model call (the final turn included; compaction summaries are not steps). *Tokens* are `PromptTokens + OutputTokens` of every generation, summaries included. *Active time* is measured only while `Engine.Run` executes, so time parked in `waiting_approval` (and later `waiting_events`) is excluded automatically.
6. **Counters** (steps, tokens, active time, wrap-up flag, loop-guard streak, continuation count, raised output limit) are JSON in `run_checkpoints.engine_state`; core's read-modify-write keeps them through approval parks, recovery marks and interruptions. A checkpoint without them starts from zero.
7. **Truncated tool-call JSON** is already dropped by the adapters (Stage 1). The engine therefore sees `max_tokens` with no text and no calls, which means "cut before anything usable": it raises `MaxOutputTokens` once (×2, capped by the model's ceiling) through a new optional `providers.OutputLimiter` (openaicompat, anthropiccompat, gemini). Codex sends no output limit, has no limiter, and such a reply fails the run.
8. **`max_tokens` with complete tool calls runs the calls** (spec branch order: tool calls first, regardless of stop reason).
9. **`refusal` / `content_filter`** complete the run (`stop_reason=done`); an empty reply gets the text `The provider stopped this reply (<reason>).`
10. **Final turn:** tool calls a provider returns despite `tool_choice: none` are dropped; an empty final reply becomes a short stop note. A failed final-turn generation fails the run like any other generation.
11. **Loop guard** hashes `(name, canonical JSON args, result content, is_error)`; rejected calls count too. 3 identical consecutive triples → one warning note per streak; 5 → final turn with `loop_detected`.
12. **Session budget** lives in a new table `session_budgets` with nullable columns (NULL = inherit). `/budget` accepts `steps N`, `time 2h`, `tokens N`, `tokens off`, `reset`.
13. **`/continue`** is `POST /v1/messages` with `continue: true`. It needs no active run and at least one earlier run; "latest run" is ordered by its user message `seq`. Any stop reason may be continued; Telegram offers the button only for `budget_exhausted` / `loop_detected`.
14. **`core.Run.StopReason` has type `agent.StopReason`** (single definition; `Continuable()` is used by TUI and Telegram).
15. **Prompt:** only the line "Use the fewest tool calls…" is removed. "Prefer one direct path over parallel or repeated searches" stays until Stage 4c adds the parallel-call guidance.

Out of scope here (later stages): auto-wake chain limit of 20 (6b), children's usage reported to the parent (6c), context message instead of prompt changes (3), `run_step` events.

## File structure

Created:
- `internal/agent/budget.go` — `Counters`, limit checks, wrap-up and final-turn preparation, `finalTurn`.
- `internal/agent/notes.go` — `appendEngineMessage`.
- `internal/agent/loopguard.go` — call hashing and streak thresholds.
- `internal/agent/output_limit.go` — continuation of cut replies, output-limit raise.
- `internal/agent/budget_test.go`, `loopguard_test.go`, `output_limit_test.go` — engine tests.
- `internal/agent/prompt/guidance_test.go`.
- `internal/core/run_budget.go` — `RunBudgets`, defaults per trigger, session budget API.
- `internal/core/types_budget.go` — `SessionBudget`, `SessionBudgetReport`.
- `internal/core/run_continue.go` — `/continue` acceptance.
- `internal/core/run_budget_test.go`, `run_continue_test.go`, `output_limit_test.go`, `run_trigger_test.go`.
- `internal/store/sqlite_session_budgets.go` (+ test), `internal/store/sqlite_runs_test.go`.
- `internal/api/budget.go` (+ test).
- `internal/daemoncmd/run_budgets.go` (+ test).
- `internal/controlplane/continue.go`, `budget.go`, `run_stop.go` (+ tests).
- `clients/terminal/chat/runtime/run_stop_notice.go` (+ test), `clients/terminal/chat/viewmodel/surface_adapter_test.go`.
- `clients/telegram/run_notes.go` (+ test).

Modified: `internal/transcript/message.go`, `internal/agent/{engine,generate,request,ports,task,tools}.go`, `internal/agent/context/{conversation,errors}.go`, `internal/agent/prompt/guidance.go`, `internal/providers/contract.go` and three adapters, `internal/core/{types_run,run_checkpoint,run_execute,run_outcome,agent_journal,core,runs,session_inputs,ports,contracts}.go`, `internal/store/{schema,sqlite_messages,sqlite_run_checkpoints}.go`, `internal/api/sessions.go`, `internal/daemonclient/sessions.go`, `internal/clientruntime/controlplane_runtime.go`, `internal/commandcatalog/catalog.go`, `internal/controlplane/{catalog,dispatcher}.go`, `internal/setup/types.go`, `internal/daemoncmd/{bootstrap,run}.go`, TUI `surface_adapter.go` / `app_runtime_events.go`, Telegram `delivery.go` / `worker_types.go`, iOS `Models.swift` + test, and tests named per task.

---

### Task 1: `origin: engine` on transcript messages

**Files:**
- Modify: `internal/transcript/message.go` (type `Message`, new type `Origin`)
- Modify: `internal/store/schema.go` (`applyCanonicalSchema`)
- Modify: `internal/store/sqlite_messages.go` (`messageColumns`, `insertMessage`, `scanMessage`)
- Test: `internal/store/sqlite_messages_test.go`

- [ ] **Step 1: Write the failing test** — append to `internal/store/sqlite_messages_test.go`:

```go
func TestMessageOriginIsStored(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	saveTestMessage(t, st, transcript.Message{ID: "note", SessionID: "s1", RunID: "r1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "plain", SessionID: "s1", CreatedAt: testEpoch})

	stored, err := st.GetMessage(ctx, "note")
	if err != nil || stored.Origin != transcript.OriginEngine {
		t.Fatalf("stored note = %+v err = %v", stored, err)
	}
	listed, err := st.ListMessages(ctx, "s1", 0)
	if err != nil || len(listed) != 2 || listed[0].Origin != transcript.OriginEngine || listed[1].Origin != "" {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/ -run TestMessageOriginIsStored`
Expected: build failure `undefined: transcript.OriginEngine` / `unknown field Origin`.

- [ ] **Step 3: Add the type and field** — in `internal/transcript/message.go` replace the `Message` struct with the version below and add `Origin` right after it:

```go
type Message struct {
	ID        string        `json:"id"`
	Seq       int64         `json:"seq,omitempty"`
	SessionID string        `json:"session_id"`
	RunID     string        `json:"run_id"`
	Role      MessageRole   `json:"role"`
	Origin    Origin        `json:"origin,omitempty"`
	Content   string        `json:"content"`
	Parts     []MessagePart `json:"parts,omitempty"`
	Model     string        `json:"model,omitempty"`
	Provider  string        `json:"provider,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// Origin says who wrote a message when its role alone does not.
type Origin string

// OriginEngine marks notes the agent engine writes for the model; clients show
// them as system notes.
const OriginEngine Origin = "engine"
```

- [ ] **Step 4: Add the column** — in `internal/store/schema.go`, `applyCanonicalSchema`, directly after the block

```go
	if err := migrateMessageSeq(db); err != nil {
		return err
	}
```

insert

```go
	if err := ensureColumn(db, "messages", "origin", `ALTER TABLE messages ADD COLUMN origin TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
```

(It must run before `migrateMessageSearch`, which scans rows with `messageColumns`.)

- [ ] **Step 5: Read and write the column** — in `internal/store/sqlite_messages.go` replace `messageColumns`, `insertMessage` and `scanMessage` with:

```go
const messageColumns = `id, session_id, run_id, role, origin, content, parts_json, model, provider, created_at, updated_at, seq`
```

```go
// insertMessage assigns the next database-wide seq in the same statement and returns it.
func insertMessage(ctx context.Context, queryer sqlRowQueryer, message transcript.Message) (int64, error) {
	var seq int64
	err := queryer.QueryRowContext(ctx, `
INSERT INTO messages(id, session_id, run_id, role, origin, content, parts_json, model, provider, created_at, updated_at, seq)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages))
RETURNING seq`,
		message.ID,
		message.SessionID,
		message.RunID,
		string(message.Role),
		string(message.Origin),
		message.Content,
		marshalMessageParts(message),
		message.Model,
		message.Provider,
		formatTime(message.CreatedAt),
		formatTime(messageUpdatedAt(message)),
	).Scan(&seq)
	return seq, err
}

func scanMessage(scanner messageScanner) (transcript.Message, error) {
	var message transcript.Message
	var role string
	var origin string
	var partsJSON string
	var model string
	var provider string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&message.ID, &message.SessionID, &message.RunID, &role, &origin, &message.Content, &partsJSON, &model, &provider, &createdAt, &updatedAt, &message.Seq); err != nil {
		return transcript.Message{}, fmt.Errorf("store: scan message: %w", err)
	}
	message.Role = transcript.MessageRole(role)
	message.Origin = transcript.Origin(origin)
	message.Parts = unmarshalMessageParts(partsJSON)
	message.Model = model
	message.Provider = provider
	message.CreatedAt = mustParseTime(createdAt)
	message.UpdatedAt = message.CreatedAt
	if strings.TrimSpace(updatedAt) != "" {
		message.UpdatedAt = mustParseTime(updatedAt)
	}
	return message, nil
}
```

`updateMessageRow` stays as is: the origin of a written message never changes.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/store/ ./internal/transcript/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/transcript/message.go internal/store/schema.go internal/store/sqlite_messages.go internal/store/sqlite_messages_test.go
git commit -m "$(cat <<'EOF'
feat(transcript): mark engine-written messages with origin

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Engine notes reach the model as user text

**Files:**
- Modify: `internal/agent/context/conversation.go` (`TextOnlyConversation`, `toProviderMessages`, new `engineNoteMessage`)
- Test: `internal/agent/context/tool_step_conversation_test.go`

- [ ] **Step 1: Write the failing tests** — append to `internal/agent/context/tool_step_conversation_test.go`:

```go
func TestEngineNotesReachTheModelAsUserText(t *testing.T) {
	stepEnd := transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}}
	note := transcript.Message{Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note: about 2 steps left."}
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect a"},
		{Role: transcript.MessageRoleSystem, Content: "a plain system row stays hidden"},
		{Role: transcript.MessageRoleAssistant, Content: "Reading.", Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: "Reading."}}, stepEnd}},
		stepCallMessage("a"), stepResultMessage("a", "A", false),
		note,
	}

	conversation, err := Conversation(context.Background(), history, nil, "", false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 4 || len(conversation[1].ToolCalls) != 1 || conversation[2].ToolCallID != "a" {
		t.Fatalf("conversation = %+v, want user, step, result, note", conversation)
	}
	if last := conversation[3]; last.Role != "user" || last.Content != note.Content {
		t.Fatalf("note = %+v", last)
	}

	text := TextOnlyConversation(history, "")
	if last := text[len(text)-1]; last.Role != "user" || last.Content != note.Content {
		t.Fatalf("text-only conversation = %+v", text)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/agent/context/ -run TestEngineNotesReachTheModelAsUserText`
Expected: FAIL — `conversation = [...]` has 3 messages (the note is dropped).

- [ ] **Step 3: Implement** — in `internal/agent/context/conversation.go`:

Add below `skipInternalPlanPromptForProvider`:

```go
// engineNoteMessage is the user text the model reads for an engine note; other
// system rows never reach the model.
func engineNoteMessage(message transcript.Message) (providers.Message, bool) {
	content := strings.TrimSpace(message.Content)
	if message.Origin != transcript.OriginEngine || content == "" {
		return providers.Message{}, false
	}
	return providers.Message{Role: string(transcript.MessageRoleUser), Content: content}, true
}
```

Replace `TextOnlyConversation` with:

```go
// TextOnlyConversation renders history as plain text for models without tool calling.
func TextOnlyConversation(history []transcript.Message, currentRunID string) []providers.Message {
	conversation := make([]providers.Message, 0, len(history))
	for _, message := range history {
		if skipInternalPlanPromptForProvider(message, currentRunID) || transcript.HasFinishReason(message, restartFinishReason) {
			continue
		}
		if message.Role == transcript.MessageRoleSystem {
			if note, ok := engineNoteMessage(message); ok {
				conversation = append(conversation, note)
			}
			continue
		}
		role := string(message.Role)
		content := textOnlyProviderContent(message)
		if strings.TrimSpace(content) == "" {
			continue
		}
		if message.Role == transcript.MessageRoleTool {
			role = string(transcript.MessageRoleUser)
		}
		conversation = append(conversation, providers.Message{
			Role:    role,
			Content: content,
		})
	}
	return conversation
}
```

In `toProviderMessages` replace the opening guard

```go
	if message.Role == transcript.MessageRoleSystem {
		return nil, nil
	}
```

with

```go
	if message.Role == transcript.MessageRoleSystem {
		if note, ok := engineNoteMessage(message); ok {
			return []providers.Message{note}, nil
		}
		return nil, nil
	}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/context/conversation.go internal/agent/context/tool_step_conversation_test.go
git commit -m "$(cat <<'EOF'
feat(agent): send engine notes to the model as user text

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: `providers.OutputLimiter`

**Files:**
- Modify: `internal/providers/contract.go`
- Modify: `internal/providers/ai/openaicompat/config.go` (new method after `Identity`)
- Modify: `internal/providers/ai/anthropiccompat/adapter.go` (new method after `Identity`)
- Modify: `internal/providers/ai/gemini/adapter.go` (new method after `Identity`)
- Test: `internal/providers/ai/openaicompat/output_limits_test.go`, `internal/providers/ai/anthropiccompat/output_limits_test.go`, `internal/providers/ai/gemini/output_limits_test.go`

`openaicodex` gets no implementation: it never sends an output limit.

- [ ] **Step 1: Write the failing tests**

`internal/providers/ai/openaicompat/output_limits_test.go`:

```go
package openaicompat

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var _ providers.OutputLimiter = (*Runtime)(nil)

func TestOutputLimitsFollowConfigCatalogAndLearnedCaps(t *testing.T) {
	providers.RegisterModelMetadata("openai-limits", providers.TypeOpenAICompat, "openai-limits-model", providers.ModelMetadataRegistration{MaxOutputTokens: 6000})
	r := &Runtime{model: "openai-limits-model", metadataID: "openai-limits", maxOutputTokens: 4000}
	if current, ceiling := r.OutputLimits(); current != 4000 || ceiling != 6000 {
		t.Fatalf("limits = %d/%d, want 4000/6000", current, ceiling)
	}
	r.rememberMaxTokensRejection(5000, false)
	if current, ceiling := r.OutputLimits(); current != 4000 || ceiling != 5000 {
		t.Fatalf("limits after a learned cap = %d/%d, want 4000/5000", current, ceiling)
	}
	r.rememberMaxTokensRejection(0, true)
	if current, ceiling := r.OutputLimits(); current != 0 || ceiling != 0 {
		t.Fatalf("limits without the field = %d/%d, want 0/0", current, ceiling)
	}
}
```

`internal/providers/ai/anthropiccompat/output_limits_test.go`:

```go
package anthropiccompat

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var _ providers.OutputLimiter = (*Runtime)(nil)

func TestOutputLimitsUseConfigThenDefaultUnderTheCatalogCeiling(t *testing.T) {
	providers.RegisterModelMetadata("anthropic-limits", providers.TypeAnthropic, "claude-limits-model", providers.ModelMetadataRegistration{MaxOutputTokens: 64000})
	for _, tc := range []struct {
		configured, current int64
	}{
		{2000, 2000},
		{0, providers.DefaultMaxOutputTokens},
	} {
		r := &Runtime{model: "claude-limits-model", providerID: "anthropic-limits", maxTokens: tc.configured}
		if current, ceiling := r.OutputLimits(); current != tc.current || ceiling != 64000 {
			t.Errorf("configured %d: limits = %d/%d, want %d/64000", tc.configured, current, ceiling, tc.current)
		}
	}
}
```

`internal/providers/ai/gemini/output_limits_test.go`:

```go
package gemini

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var _ providers.OutputLimiter = (*Runtime)(nil)

func TestOutputLimitsUseConfigThenDefaultUnderTheCatalogCeiling(t *testing.T) {
	providers.RegisterModelMetadata("gemini-limits", providers.TypeGemini, "gemini-limits-model", providers.ModelMetadataRegistration{MaxOutputTokens: 65536})
	for _, tc := range []struct {
		configured, current int64
	}{
		{3000, 3000},
		{0, providers.DefaultMaxOutputTokens},
	} {
		r := &Runtime{model: "gemini-limits-model", providerID: "gemini-limits", maxOutputTokens: tc.configured}
		if current, ceiling := r.OutputLimits(); current != tc.current || ceiling != 65536 {
			t.Errorf("configured %d: limits = %d/%d, want %d/65536", tc.configured, current, ceiling, tc.current)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/providers/ai/...`
Expected: build failure `undefined: providers.OutputLimiter`.

- [ ] **Step 3: Add the interface** — in `internal/providers/contract.go`, after `RuntimeIdentifier`:

```go
// OutputLimiter reports the output limit a runtime sends when a request names
// none and the most its model accepts (0 = unknown). A runtime without it sends
// no limit the engine could raise.
type OutputLimiter interface {
	OutputLimits() (current int64, ceiling int64)
}
```

- [ ] **Step 4: Implement it in the adapters**

`internal/providers/ai/openaicompat/config.go`, after `Identity`:

```go
func (r *Runtime) OutputLimits() (int64, int64) {
	capTokens, omit := r.learnedMaxTokensLimit()
	if omit {
		return 0, 0
	}
	current := providers.ResolveMaxOutputTokens(0, r.maxOutputTokens, r.metadataID, providers.TypeOpenAICompat, r.model)
	ceiling := int64(providers.ResolveModelMetadata(r.metadataID, providers.TypeOpenAICompat, r.model).MaxOutputTokens)
	if capTokens > 0 && (ceiling == 0 || capTokens < ceiling) {
		ceiling = capTokens
	}
	if ceiling > 0 && current > ceiling {
		current = ceiling
	}
	return current, ceiling
}
```

`internal/providers/ai/anthropiccompat/adapter.go`, after `Identity`:

```go
func (r *Runtime) OutputLimits() (int64, int64) {
	current := providers.ResolveMaxOutputTokens(0, r.maxTokens, r.providerID, providers.TypeAnthropic, r.model)
	return current, int64(providers.ResolveModelMetadata(r.providerID, providers.TypeAnthropic, r.model).MaxOutputTokens)
}
```

`internal/providers/ai/gemini/adapter.go`, after `Identity`:

```go
func (r *Runtime) OutputLimits() (int64, int64) {
	current := providers.ResolveMaxOutputTokens(0, r.maxOutputTokens, r.providerID, providers.TypeGemini, r.model)
	return current, int64(providers.ResolveModelMetadata(r.providerID, providers.TypeGemini, r.model).MaxOutputTokens)
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/providers/...`
Expected: PASS.

- [ ] **Step 6: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/providers/contract.go internal/providers/ai/openaicompat/config.go internal/providers/ai/anthropiccompat/adapter.go internal/providers/ai/gemini/adapter.go internal/providers/ai/openaicompat/output_limits_test.go internal/providers/ai/anthropiccompat/output_limits_test.go internal/providers/ai/gemini/output_limits_test.go
git commit -m "$(cat <<'EOF'
feat(providers): report the output limit a runtime sends and its ceiling

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Run stop reason, continuation, trigger and checkpoint engine state

**Files:**
- Modify: `internal/agent/task.go` (new type `StopReason`)
- Modify: `internal/core/types_run.go` (type `Run`, new type `RunTrigger`)
- Modify: `internal/core/run_checkpoint.go` (type `RunCheckpoint`)
- Modify: `internal/core/runs.go` (`AcceptTriggeredRun`)
- Modify: `internal/store/schema.go` (`applyCanonicalSchema`)
- Modify: `internal/store/sqlite_messages.go` (`GetRun`, `GetActiveRunBySession`, `ListActiveRuns`, `insertRun`, `scanRun`, `updateRun`, new `runColumns`)
- Modify: `internal/store/sqlite_run_checkpoints.go` (`SaveRunCheckpoint`, `GetRunCheckpoint`)
- Test: `internal/store/sqlite_runs_test.go` (new), `internal/core/run_trigger_test.go` (new)

- [ ] **Step 1: Write the failing store tests** — create `internal/store/sqlite_runs_test.go`:

```go
package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRunStopContinuationAndTriggerAreStored(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	run := core.Run{ID: "r2", SessionID: "s1", UserMessageID: "m2", Trigger: core.RunTriggerAutomation, ContinuesRunID: "r1", Status: core.RunStatusRunning, StartedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.Status = core.RunStatusCompleted
	run.StopReason = agent.StopBudgetExhausted
	if err := st.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetRun(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Trigger != core.RunTriggerAutomation || stored.ContinuesRunID != "r1" || stored.StopReason != agent.StopBudgetExhausted {
		t.Fatalf("stored run = %+v", stored)
	}
}

func TestRunCheckpointKeepsEngineState(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r1")
	checkpoint := core.RunCheckpoint{RunID: "r1", Phase: core.RunCheckpointPhaseModel, EngineState: json.RawMessage(`{"steps":3}`), UpdatedAt: testEpoch}
	if err := st.SaveRunCheckpoint(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetRunCheckpoint(ctx, "r1")
	if err != nil || string(stored.EngineState) != `{"steps":3}` {
		t.Fatalf("stored checkpoint = %+v err = %v", stored, err)
	}
}
```

- [ ] **Step 2: Write the failing core test** — create `internal/core/run_trigger_test.go`:

```go
package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestTriggeredRunsAreMarkedAsAutomation(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	session, _ := saveCrashRecoveryRun(t, db, "trigger", core.RunStatusCompleted, false)

	result, err := app.AcceptTriggeredRun(context.Background(), core.HandleTriggeredRunInput{TriggerID: "job-1", SessionID: session.ID, Text: "scheduled check"})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), result.Run.ID)
	if err != nil || stored.Trigger != core.RunTriggerAutomation {
		t.Fatalf("triggered run = %+v err = %v", stored, err)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/store/ ./internal/core/ -run 'TestRunStopContinuationAndTriggerAreStored|TestRunCheckpointKeepsEngineState|TestTriggeredRunsAreMarkedAsAutomation'`
Expected: build failure (`undefined: agent.StopBudgetExhausted`, `unknown field Trigger`, `unknown field EngineState`).

- [ ] **Step 4: Add `agent.StopReason`** — append to `internal/agent/task.go`:

```go
// StopReason says why a completed run stopped.
type StopReason string

const (
	StopDone            StopReason = "done"
	StopBudgetExhausted StopReason = "budget_exhausted"
	StopLoopDetected    StopReason = "loop_detected"
)

// Continuable reports whether a run stopped before its work was done.
func (r StopReason) Continuable() bool {
	return r == StopBudgetExhausted || r == StopLoopDetected
}
```

- [ ] **Step 5: Extend `core.Run`** — in `internal/core/types_run.go` add the import `"github.com/Suren878/matrixclaw/internal/agent"` and replace the `Run` struct with:

```go
// RunTrigger is what started a run and selects its default budget; empty means a
// user message.
type RunTrigger string

// RunTriggerAutomation covers scheduled jobs and runs woken by finished background work.
const RunTriggerAutomation RunTrigger = "automation"

type Run struct {
	ID                 string             `json:"id"`
	SessionID          string             `json:"session_id"`
	UserMessageID      string             `json:"user_message_id"`
	Client             string             `json:"client,omitempty"`
	ExternalKey        string             `json:"external_key,omitempty"`
	ClientCapabilities ClientCapabilities `json:"client_capabilities,omitempty"`
	Trigger            RunTrigger         `json:"trigger,omitempty"`
	ContinuesRunID     string             `json:"continues_run_id,omitempty"`
	Status             RunStatus          `json:"status"`
	StopReason         agent.StopReason   `json:"stop_reason,omitempty"`
	Error              string             `json:"error,omitempty"`
	StartedAt          time.Time          `json:"started_at"`
	FinishedAt         *time.Time         `json:"finished_at,omitempty"`
	UpdatedAt          time.Time          `json:"updated_at"`
}
```

- [ ] **Step 6: Extend `RunCheckpoint`** — in `internal/core/run_checkpoint.go` add the import `"encoding/json"` and replace the struct with:

```go
type RunCheckpoint struct {
	RunID          string             `json:"run_id"`
	Phase          RunCheckpointPhase `json:"phase"`
	ToolCallID     string             `json:"tool_call_id,omitempty"`
	ToolName       string             `json:"tool_name,omitempty"`
	RecoveryCount  int                `json:"recovery_count,omitempty"`
	RecoveryReason string             `json:"recovery_reason,omitempty"`
	EngineState    json.RawMessage    `json:"engine_state,omitempty"`
	UpdatedAt      time.Time          `json:"updated_at"`
}
```

- [ ] **Step 7: Mark triggered runs** — in `internal/core/runs.go`, `AcceptTriggeredRun`, add `Trigger: RunTriggerAutomation,` to the `run := Run{...}` literal (after `ClientCapabilities: input.ClientCapabilities,`).

- [ ] **Step 8: Add the columns** — in `internal/store/schema.go`, `applyCanonicalSchema`, directly after the `ensureColumn(db, "runs", "client_capabilities_json", ...)` block insert:

```go
	if err := ensureColumn(db, "runs", "stop_reason", `ALTER TABLE runs ADD COLUMN stop_reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "continues_run_id", `ALTER TABLE runs ADD COLUMN continues_run_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "trigger_kind", `ALTER TABLE runs ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "run_checkpoints", "engine_state", `ALTER TABLE run_checkpoints ADD COLUMN engine_state TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
```

(`trigger` is an SQLite keyword, hence `trigger_kind`.)

- [ ] **Step 9: Read and write the run columns** — in `internal/store/sqlite_messages.go`:

Add next to `messageColumns`:

```go
const runColumns = `id, session_id, user_message_id, client, external_key, client_capabilities_json, status, error, stop_reason, continues_run_id, trigger_kind, started_at, finished_at, updated_at`
```

Replace the three SELECT column lists with `runColumns`:

```go
func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, runID)

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Run{}, core.ErrNotFound
		}
		return core.Run{}, fmt.Errorf("store: get run: %w", err)
	}
	return run, nil
}

func (s *SQLiteStore) GetActiveRunBySession(ctx context.Context, sessionID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE session_id = ?
  AND status IN (?, ?, ?)
ORDER BY started_at DESC, updated_at DESC
LIMIT 1`,
		strings.TrimSpace(sessionID),
		string(core.RunStatusAccepted),
		string(core.RunStatusRunning),
		string(core.RunStatusWaitingApproval),
	)

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Run{}, core.ErrNotFound
		}
		return core.Run{}, fmt.Errorf("store: get active run by session: %w", err)
	}
	return run, nil
}
```

In `ListActiveRuns` replace the SELECT with

```go
	rows, err := s.db.QueryContext(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE status IN (?, ?, ?)
ORDER BY started_at ASC, updated_at ASC`,
```

(keep its arguments and the rest of the function).

Replace `insertRun`, `scanRun` and `updateRun`:

```go
func insertRun(ctx context.Context, execer sqlExecer, run core.Run) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO runs(`+runColumns+`)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.SessionID,
		run.UserMessageID,
		run.Client,
		run.ExternalKey,
		marshalClientCapabilities(run.ClientCapabilities),
		string(run.Status),
		run.Error,
		string(run.StopReason),
		run.ContinuesRunID,
		string(run.Trigger),
		formatTime(run.StartedAt),
		nullableTime(run.FinishedAt),
		formatTime(run.UpdatedAt),
	)
	return err
}

func scanRun(scanner runScanner) (core.Run, error) {
	var run core.Run
	var status string
	var stopReason string
	var trigger string
	var capabilitiesJSON string
	var startedAt string
	var finishedAt sql.NullString
	var updatedAt string
	if err := scanner.Scan(&run.ID, &run.SessionID, &run.UserMessageID, &run.Client, &run.ExternalKey, &capabilitiesJSON, &status, &run.Error, &stopReason, &run.ContinuesRunID, &trigger, &startedAt, &finishedAt, &updatedAt); err != nil {
		return core.Run{}, err
	}
	run.ClientCapabilities = unmarshalClientCapabilities(capabilitiesJSON)
	run.Status = core.RunStatus(status)
	run.StopReason = agent.StopReason(stopReason)
	run.Trigger = core.RunTrigger(trigger)
	run.StartedAt = mustParseTime(startedAt)
	if finishedAt.Valid {
		parsed := mustParseTime(finishedAt.String)
		run.FinishedAt = &parsed
	}
	run.UpdatedAt = mustParseTime(updatedAt)
	return run, nil
}

func updateRun(ctx context.Context, execer sqlExecer, run core.Run) (sql.Result, error) {
	return execer.ExecContext(ctx, `
UPDATE runs
SET client_capabilities_json = ?, status = ?, error = ?, stop_reason = ?, finished_at = ?, updated_at = ?
WHERE id = ?`,
		marshalClientCapabilities(run.ClientCapabilities),
		string(run.Status),
		run.Error,
		string(run.StopReason),
		nullableTime(run.FinishedAt),
		formatTime(run.UpdatedAt),
		run.ID,
	)
}
```

Add the import `"github.com/Suren878/matrixclaw/internal/agent"` to `sqlite_messages.go`.

- [ ] **Step 10: Read and write the checkpoint engine state** — replace `SaveRunCheckpoint` and `GetRunCheckpoint` in `internal/store/sqlite_run_checkpoints.go` (add the import `"encoding/json"`):

```go
func (s *SQLiteStore) SaveRunCheckpoint(ctx context.Context, checkpoint core.RunCheckpoint) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_checkpoints(run_id, phase, tool_call_id, tool_name, recovery_count, recovery_reason, engine_state, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    phase = excluded.phase,
    tool_call_id = excluded.tool_call_id,
    tool_name = excluded.tool_name,
    recovery_count = excluded.recovery_count,
    recovery_reason = excluded.recovery_reason,
    engine_state = excluded.engine_state,
    updated_at = excluded.updated_at`,
		checkpoint.RunID,
		string(checkpoint.Phase),
		checkpoint.ToolCallID,
		checkpoint.ToolName,
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

func (s *SQLiteStore) GetRunCheckpoint(ctx context.Context, runID string) (core.RunCheckpoint, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT run_id, phase, tool_call_id, tool_name, recovery_count, recovery_reason, engine_state, updated_at
FROM run_checkpoints
WHERE run_id = ?`, runID)
	var checkpoint core.RunCheckpoint
	var phase string
	var engineState string
	var updatedAt string
	if err := row.Scan(
		&checkpoint.RunID,
		&phase,
		&checkpoint.ToolCallID,
		&checkpoint.ToolName,
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
	if engineState != "" {
		checkpoint.EngineState = json.RawMessage(engineState)
	}
	checkpoint.UpdatedAt = mustParseTime(updatedAt)
	return checkpoint, nil
}
```

- [ ] **Step 11: Run the tests**

Run: `go test ./internal/store/ ./internal/core/`
Expected: PASS.

- [ ] **Step 12: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/task.go internal/core/types_run.go internal/core/run_checkpoint.go internal/core/runs.go internal/core/run_trigger_test.go internal/store/schema.go internal/store/sqlite_messages.go internal/store/sqlite_run_checkpoints.go internal/store/sqlite_runs_test.go
git commit -m "$(cat <<'EOF'
feat(store): keep run stop reason, continuation, trigger and engine state

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Run budget, wrap-up note and tool-less final turn

Engine part first (pure, `agenttest`), then core wiring in the same commit, so `main` never runs without a step limit. Run only `./internal/agent/...` until Step 9: until Step 9 deletes it, the old core test that expects "tool loop exceeded 32 steps" would loop forever against the new engine.

**Files:**
- Create: `internal/agent/budget.go`, `internal/agent/notes.go`, `internal/agent/budget_test.go`
- Modify: `internal/agent/task.go` (`Task`, new `Budget`, `Outcome`)
- Modify: `internal/agent/ports.go` (`State`)
- Modify: `internal/agent/engine.go` (whole file below)
- Modify: `internal/agent/generate.go` (`recordStep`)
- Modify: `internal/agent/request.go` (`buildRequest`)
- Modify: `internal/agent/engine_test.go` (`TestTextReplyCompletesWithFinalMessage`; delete `TestThirtyTwoToolStepsFailTheRun`)
- Create: `internal/core/run_budget.go`, `internal/core/run_budget_test.go`
- Modify: `internal/core/core.go` (`Core`, `New`)
- Modify: `internal/core/run_checkpoint.go` (`saveRunCheckpoint`, new `saveEngineCheckpoint`, `updateRunCheckpoint`, `resumeCounters`)
- Modify: `internal/core/agent_journal.go` (`Checkpoint`)
- Modify: `internal/core/run_execute.go` (`ExecuteRun`, `nativeEngine`)
- Modify: `internal/core/run_outcome.go` (`applyOutcome`, `applyInterruptedOutcome`)
- Modify: `internal/core/native_run_characterization_test.go` (delete `TestNativeRunFailsAfterThirtyTwoToolSteps`)

- [ ] **Step 1: Write the failing engine tests** — create `internal/agent/budget_test.go`:

```go
package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func runTask(t *testing.T, f *agenttest.Fixture, task agent.Task) agent.Outcome {
	t.Helper()
	outcome, err := f.Engine().Run(context.Background(), task)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	return outcome
}

// counterTool returns new output on every call, so the loop guard never fires.
func counterTool() agenttest.ToolFunc {
	calls := 0
	return func(tools.Call) tools.Result {
		calls++
		return tools.Result{Content: fmt.Sprintf("output %d", calls)}
	}
}

func toolSteps(n int, name string) []agenttest.Turn {
	turns := make([]agenttest.Turn, 0, n)
	for i := 1; i <= n; i++ {
		turns = append(turns, calls(call(fmt.Sprintf("%s%d", name, i), name)))
	}
	return turns
}

func engineNotes(messages []transcript.Message) []transcript.Message {
	var notes []transcript.Message
	for _, message := range messages {
		if message.Origin == transcript.OriginEngine {
			notes = append(notes, message)
		}
	}
	return notes
}

func lastMessage(request providers.Request) providers.Message {
	if len(request.Messages) == 0 {
		return providers.Message{}
	}
	return request.Messages[len(request.Messages)-1]
}

func TestStepBudgetEndsWithAToolLessFinalTurn(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(append(toolSteps(3, "read"), text("Read three files; /continue reads the rest."))...)
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 3}

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopBudgetExhausted || outcome.Assistant.Content != "Read three files; /continue reads the rest." {
		t.Fatalf("outcome = %+v", outcome)
	}
	requests := model.Requests()
	if len(requests) != 4 || len(f.Tools.Calls) != 3 {
		t.Fatalf("requests = %d tool calls = %d, want 4 and 3", len(requests), len(f.Tools.Calls))
	}
	for i, request := range requests[:3] {
		if request.ToolChoice != providers.ToolChoiceAuto {
			t.Fatalf("request %d tool choice = %q", i, request.ToolChoice)
		}
	}
	final := requests[3]
	if final.ToolChoice != providers.ToolChoiceNone || len(final.Tools) == 0 {
		t.Fatalf("final turn tool choice = %q with %d tools, want none with tools still defined", final.ToolChoice, len(final.Tools))
	}
	if note := lastMessage(final); note.Role != "user" || !strings.Contains(note.Content, "reached its budget (3 steps)") {
		t.Fatalf("final turn instruction = %+v", note)
	}
	notes := engineNotes(f.Journal.Messages)
	if len(notes) != 1 || notes[0].Role != transcript.MessageRoleSystem || notes[0].RunID != agenttest.RunID {
		t.Fatalf("engine notes = %+v", notes)
	}
}

func TestWrapUpNoteArrivesOnceAtEightyPercent(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(append(toolSteps(10, "read"), text("Summary."))...)
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 10}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 11 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if strings.Contains(lastMessage(requests[7]).Content, "Budget note") {
		t.Fatal("wrap-up note arrived before 80% of the steps were used")
	}
	if note := lastMessage(requests[8]); note.Role != "user" || !strings.Contains(note.Content, "about 2 steps left") {
		t.Fatalf("request 9 ends with %+v, want the wrap-up note", note)
	}
	wrapUps := 0
	for _, note := range engineNotes(f.Journal.Messages) {
		if strings.Contains(note.Content, "Budget note") {
			wrapUps++
		}
	}
	if wrapUps != 1 {
		t.Fatalf("wrap-up notes = %d, want 1", wrapUps)
	}
}

func TestActiveTimeBudgetCountsTheRunsWorkingTime(t *testing.T) {
	f := agenttest.NewFixture()
	waits := 0
	f.Tools.Funcs["wait"] = func(tools.Call) tools.Result {
		waits++
		f.Clock = f.Clock.Add(10 * time.Minute)
		return tools.Result{Content: fmt.Sprintf("waited %d", waits)}
	}
	model := agenttest.NewScriptedModel(append(toolSteps(3, "wait"), text("Out of time."))...)
	task := f.Task(model)
	task.Budget = agent.Budget{ActiveTime: 25 * time.Minute}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 4 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if note := lastMessage(requests[2]); !strings.Contains(note.Content, "about 5 minutes left") {
		t.Fatalf("request 3 ends with %+v, want the wrap-up note", note)
	}
	if final := requests[3]; final.ToolChoice != providers.ToolChoiceNone || !strings.Contains(lastMessage(final).Content, "active time of 25 minutes") {
		t.Fatalf("final turn = %q / %+v", final.ToolChoice, lastMessage(final))
	}
}

func TestResumedCountersKeepTheBudget(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["wait"] = func(tools.Call) tools.Result {
		f.Clock = f.Clock.Add(10 * time.Minute)
		return tools.Result{Content: "waited"}
	}
	model := agenttest.NewScriptedModel(calls(call("w1", "wait")), text("Out of time."))
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 10, ActiveTime: 25 * time.Minute}
	task.Resume = agent.Counters{Steps: 4, Active: 20 * time.Minute}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 2 || requests[1].ToolChoice != providers.ToolChoiceNone {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	last := f.Journal.States[len(f.Journal.States)-1].Counters
	if last.Steps != 5 || last.Active != 30*time.Minute || !last.WrapUpSent {
		t.Fatalf("last checkpoint counters = %+v", last)
	}
}

func TestTokenBudgetCountsEveryGeneration(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	usage := providers.Usage{PromptTokens: 40, OutputTokens: 10}
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("r1", "read")}, Usage: usage}},
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("r2", "read")}, Usage: usage}},
		text("Token budget used."),
	)
	task := f.Task(model)
	task.Budget = agent.Budget{Tokens: 100}

	outcome := runTask(t, f, task)

	requests := model.Requests()
	if outcome.StopReason != agent.StopBudgetExhausted || len(requests) != 3 || !strings.Contains(lastMessage(requests[2]).Content, "reached its budget (100 tokens)") {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
}

func TestFinalTurnDropsToolCallsAndFallsBackToAStopNote(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(calls(call("r1", "read")), calls(call("r2", "read")))
	task := f.Task(model)
	task.Budget = agent.Budget{Steps: 1}

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopBudgetExhausted || len(f.Tools.Calls) != 1 {
		t.Fatalf("outcome = %+v tool calls = %d", outcome, len(f.Tools.Calls))
	}
	if !strings.HasPrefix(outcome.Assistant.Content, "Stopped: the run reached its budget.") {
		t.Fatalf("final reply = %q", outcome.Assistant.Content)
	}
}

func TestCheckpointsCarryTheRunCounters(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 10, OutputTokens: 5}}},
		text("Done."),
	)

	run(t, f, model)

	if got := phases(f.Journal.States); got != "model,tool:c1,model,model" {
		t.Fatalf("checkpoints = %s", got)
	}
	if tool := f.Journal.States[1].Counters; tool.Steps != 1 || tool.Tokens != 15 {
		t.Fatalf("tool checkpoint counters = %+v", tool)
	}
}
```

In `internal/agent/engine_test.go`:
- delete `TestThirtyTwoToolStepsFailTheRun` entirely (the step budget test replaces it);
- in `TestTextReplyCompletesWithFinalMessage` replace the first check with

```go
	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant == nil || outcome.AssistantSaved {
		t.Fatalf("outcome = %+v", outcome)
	}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/...`
Expected: build failure (`unknown field Budget in struct literal`, `undefined: agent.Counters`, `outcome.StopReason undefined`).

- [ ] **Step 3: Budget types, outcome stop reason and checkpoint counters**

In `internal/agent/task.go` add the import `"time"` and replace `Task` and `Outcome` (keep `Status` and the `StopReason` block from Task 4):

```go
// Task is one native run to execute. Resume holds the counters the run had
// reached before it was parked or interrupted; a new run starts from zero.
type Task struct {
	RunID       string
	SessionID   string
	Client      string
	ExternalKey string
	WorkingDir  string
	Model       Model
	Budget      Budget
	Resume      Counters
}

// Budget limits one run; a zero field is unlimited.
type Budget struct {
	Steps      int
	ActiveTime time.Duration
	Tokens     int64
}
```

```go
// Outcome is applied by core. Assistant is the final reply (completed), the reply to
// seal (canceled, interrupted) or the errored reply (failed with MarkErrored).
// Reached is what the last step produced before an interruption; StopReason is set
// whenever the run completed or reached completion.
type Outcome struct {
	Status         Status
	StopReason     StopReason
	Assistant      *transcript.Message
	AssistantSaved bool
	Err            error
	MarkErrored    bool
	Reached        Status
}
```

In `internal/agent/ports.go` replace `State`:

```go
// State is the checkpoint written before each model call and tool execution; its
// Counters let a parked or restarted run keep its budget.
type State struct {
	RunID      string
	Phase      Phase
	ToolCallID string
	ToolName   string
	Counters   Counters
}
```

- [ ] **Step 4: Engine notes** — create `internal/agent/notes.go`:

```go
package agent

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// appendEngineMessage journals a note from the engine: the model reads it as user
// text and clients show it as a system note.
func (r *run) appendEngineMessage(ctx context.Context, text string) error {
	now := r.Now()
	return r.history.append(ctx, transcript.Message{
		ID:        r.NewID("msg"),
		SessionID: r.task.SessionID,
		RunID:     r.task.RunID,
		Role:      transcript.MessageRoleSystem,
		Origin:    transcript.OriginEngine,
		Content:   text,
		Parts:     transcript.NormalizeMessageParts(text, nil),
		CreatedAt: now,
		UpdatedAt: now,
	})
}
```

- [ ] **Step 5: Budget logic** — create `internal/agent/budget.go`:

```go
package agent

import (
	"context"
	"fmt"
	"time"
)

// wrapUpShare is the used share of a limit at which the run is told to wrap up.
const wrapUpShare = 0.8

// Counters is what a run has used of its budget; it is checkpointed so a parked
// or restarted run continues from it.
type Counters struct {
	Steps      int           `json:"steps,omitempty"`
	Tokens     int64         `json:"tokens,omitempty"`
	Active     time.Duration `json:"active,omitempty"`
	WrapUpSent bool          `json:"wrap_up_sent,omitempty"`
}

// active is the run's working time: what it had used before plus this Run call.
func (r *run) active() time.Duration {
	return r.task.Resume.Active + r.Now().Sub(r.started)
}

// prepareStep journals the engine notes due before this step's model call and
// returns the stop reason when this step is the tool-less final turn.
func (r *run) prepareStep(ctx context.Context) (StopReason, error) {
	if reached := r.exhausted(); reached != "" {
		return StopBudgetExhausted, r.appendEngineMessage(ctx, budgetStopText(reached))
	}
	if !r.counters.WrapUpSent {
		if left := r.remaining(); left != "" {
			if err := r.appendEngineMessage(ctx, wrapUpText(left)); err != nil {
				return "", err
			}
			r.counters.WrapUpSent = true
		}
	}
	return "", nil
}

// exhausted describes the first budget limit the run has reached, or "".
func (r *run) exhausted() string {
	b := r.task.Budget
	switch {
	case b.Steps > 0 && r.counters.Steps >= b.Steps:
		return quantity(int64(b.Steps), "step")
	case b.ActiveTime > 0 && r.active() >= b.ActiveTime:
		return "active time of " + approxDuration(b.ActiveTime)
	case b.Tokens > 0 && r.counters.Tokens >= b.Tokens:
		return quantity(b.Tokens, "token")
	default:
		return ""
	}
}

// remaining describes what is left of the most used limit once 80% of it is
// used, or "".
func (r *run) remaining() string {
	b := r.task.Budget
	share, left := 0.0, ""
	consider := func(used float64, text string) {
		if used >= wrapUpShare && used > share {
			share, left = used, text
		}
	}
	if b.Steps > 0 {
		consider(float64(r.counters.Steps)/float64(b.Steps), quantity(int64(b.Steps-r.counters.Steps), "step"))
	}
	if b.ActiveTime > 0 {
		active := r.active()
		consider(float64(active)/float64(b.ActiveTime), approxDuration(b.ActiveTime-active))
	}
	if b.Tokens > 0 {
		consider(float64(r.counters.Tokens)/float64(b.Tokens), quantity(b.Tokens-r.counters.Tokens, "token"))
	}
	return left
}

// finalTurn completes the run with the final turn's text; tool calls a provider
// sent despite tool_choice none are dropped.
func finalTurn(gen generation, reason StopReason) stepResult {
	response := gen.response
	response.ToolCalls = nil
	if sanitizeAssistantOutput(response.Text) == "" {
		response.Text = finalFallback(reason)
	}
	return stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: response, stop: reason}
}

// finalFallback is the reply of a final turn that produced no text.
func finalFallback(reason StopReason) string {
	if reason == StopLoopDetected {
		return "Stopped: the run kept repeating the same step. Send /continue to go on."
	}
	return "Stopped: the run reached its budget. Send /continue to go on."
}

func budgetStopText(reached string) string {
	return "This run has reached its budget (" + reached + "). Do not call tools. Reply briefly: what is done, what remains, and how to continue."
}

func wrapUpText(left string) string {
	return "Budget note: about " + left + " left in this run. Wrap up: finish the current piece of work, verify it and summarise what remains instead of starting something new."
}

func quantity(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// approxDuration renders d to the minute.
func approxDuration(d time.Duration) string {
	minutes := int64(d.Round(time.Minute) / time.Minute)
	switch {
	case minutes < 1:
		return "under a minute"
	case minutes < 60:
		return quantity(minutes, "minute")
	default:
		return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
	}
}
```

- [ ] **Step 6: Engine loop** — replace `internal/agent/engine.go` with:

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
}

// Engine runs the native agent loop over its ports.
type Engine struct {
	cfg Config
}

// New returns an engine over the given ports.
func New(cfg Config) *Engine {
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	return &Engine{cfg: cfg}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
	r := &run{Config: e.cfg, task: task, history: newHistory(e.cfg.Journal, e.cfg.Sink, window), counters: task.Resume, started: e.cfg.Now()}
	for {
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
}

type run struct {
	Config
	task     Task
	history  *history
	counters Counters
	started  time.Time
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
	stop        StopReason
}

func (s stepResult) stopReason() StopReason {
	if s.stop == "" {
		return StopDone
	}
	return s.stop
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
	final, err := r.prepareStep(ctx)
	if err != nil {
		return failedStep(err)
	}
	if err := r.checkpoint(ctx, PhaseModel, "", ""); err != nil {
		return failedStep(err)
	}
	budget, err := r.budget(ctx)
	if err != nil {
		return failedStep(err)
	}
	compacted, err := r.autoCompact(ctx, budget)
	if err != nil {
		return failedStep(err)
	}
	request, err := r.buildRequest(ctx, final)
	if err != nil {
		return failedStep(err)
	}
	if !compacted && requestNeedsCompact(request, budget) {
		if compacted, err = r.compactHistory(ctx, r.history.all(), budget.base); err != nil {
			return failedStep(err)
		}
		if compacted {
			if request, err = r.buildRequest(ctx, final); err != nil {
				return failedStep(err)
			}
		}
	}
	r.counters.Steps++
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && agentcontext.IsContextLengthExceeded(err) {
		compacted, compactErr := r.compactHistory(ctx, r.history.all(), budget.base)
		if compactErr != nil {
			return failedStep(compactErr)
		}
		if compacted {
			retry, buildErr := r.buildRequest(ctx, final)
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
	if final != "" {
		return finalTurn(gen, final)
	}
	return r.handleResponse(ctx, gen)
}

func (r *run) handleResponse(ctx context.Context, gen generation) stepResult {
	response := gen.response
	response.Text = sanitizeAssistantOutput(response.Text)
	assistant := gen.assistant
	if len(response.ToolCalls) > 0 {
		if err := r.finishToolTurn(ctx, &assistant, gen.saved, response); err != nil {
			if ctx.Err() != nil {
				// Stopped before the tool turn was written: core seals the streamed preview.
				return stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, err: err}
			}
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
		return Outcome{Status: StatusCompleted, StopReason: result.stopReason(), Assistant: &reply, AssistantSaved: result.saved}, true, nil
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
		outcome.Assistant, outcome.Reached, outcome.StopReason = &reply, StatusCompleted, result.stopReason()
	case stepWaitingApproval:
		outcome.Reached = StatusWaitingApproval
	}
	return outcome
}

func (r *run) canceled(ctx context.Context) bool {
	canceled, err := r.Inbox.Canceled(ctx, r.task.RunID)
	return err == nil && canceled
}

// checkpoint records the durable phase together with the run's counters.
func (r *run) checkpoint(ctx context.Context, phase Phase, toolCallID string, toolName string) error {
	r.counters.Active = r.active()
	return r.Journal.Checkpoint(ctx, State{RunID: r.task.RunID, Phase: phase, ToolCallID: toolCallID, ToolName: toolName, Counters: r.counters})
}
```

- [ ] **Step 7: Count tokens and pass the final-turn tool choice**

In `internal/agent/generate.go` replace `recordStep`:

```go
// recordStep stores one successful generation as a run step and counts its tokens
// against the run's budget.
func (r *run) recordStep(ctx context.Context, response providers.Response, stopReason string, latency time.Duration) error {
	r.counters.Tokens += response.Usage.PromptTokens + response.Usage.OutputTokens
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
```

In `internal/agent/request.go` replace `buildRequest`:

```go
// buildRequest assembles the step's request; the final turn keeps the tools
// defined but forbids calling them, so the cached prefix survives.
func (r *run) buildRequest(ctx context.Context, final StopReason) (providers.Request, error) {
	summary, effective := agentcontext.LatestSummaryForRun(r.history.all(), r.task.RunID)
	system, custom := r.Prompts.System(ctx, summary, effective)
	request := providers.Request{
		RunID:              r.task.RunID,
		SessionID:          r.task.SessionID,
		SystemPrompt:       system,
		CustomInstructions: custom,
		CacheKey:           r.task.SessionID,
	}
	if final != "" {
		request.ToolChoice = providers.ToolChoiceNone
	}
	if !ToolUseAllowed(r.task.Model) {
		request.Messages = agentcontext.TextOnlyConversation(effective, r.task.RunID)
		request.Messages = providers.NormalizeMessages(request.Messages, providers.ToolUseDisabled)
		return request, nil
	}
	messages, err := agentcontext.Conversation(ctx, effective, r.Attachments, r.task.RunID, ImageInputAllowed(r.task.Model), modelIdentity(r.task.Model))
	if err != nil {
		return providers.Request{}, err
	}
	request.Messages = messages
	request.Tools = toolDefinitions(r.Tools.Specs(ctx))
	return request, nil
}
```

- [ ] **Step 8: Run the engine tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 9: Write the failing core tests** — delete `TestNativeRunFailsAfterThirtyTwoToolSteps` from `internal/core/native_run_characterization_test.go` (remove any import that becomes unused), then create `internal/core/run_budget_test.go`:

```go
package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/orchestration"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// changingTool reports new output on every call, so the loop guard never fires.
func changingTool(id string) funcTool {
	var mu sync.Mutex
	calls := 0
	return funcTool{spec: recoveryToolSpec(id, tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return tools.Result{Content: fmt.Sprintf("state %d", calls)}, nil
	}}
}

func TestNativeRunEndsWithAFinalTurnAtTheDefaultStepBudget(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(changingTool("inspect_state")))
	var choices []providers.ToolChoice
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		choices = append(choices, request.ToolChoice)
		if request.ToolChoice == providers.ToolChoiceNone {
			return providers.Response{Text: "Inspected 32 times; /continue finishes the job."}, nil
		}
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("step-%d", len(choices)), Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "step-budget", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != core.RunStatusCompleted || stored.StopReason != agent.StopBudgetExhausted {
		t.Fatalf("run = %s (%s) stop reason %q", stored.Status, stored.Error, stored.StopReason)
	}
	if len(choices) != 33 || choices[31] != providers.ToolChoiceAuto || choices[32] != providers.ToolChoiceNone {
		t.Fatalf("model calls = %d, want 32 with tools and a final one without", len(choices))
	}
	notes := 0
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Origin == transcript.OriginEngine && message.Role == transcript.MessageRoleSystem {
			notes++
		}
	}
	if notes != 2 {
		t.Fatalf("engine notes = %d, want the wrap-up and the final-turn note", notes)
	}
}

func TestBudgetCountersSurviveAnApprovalPark(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunBudgets(core.RunBudgets{User: agent.Budget{Steps: 2}})
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, changingTool("inspect_state")))
	app.WithRunStarter(&recordingRunStarter{})
	var requests []providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		switch len(requests) {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		case 2:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-inspect", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		default:
			return providers.Response{Text: "Mutated and inspected; /continue verifies."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "budget-park", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	checkpoint, err := db.GetRunCheckpoint(context.Background(), run.ID)
	if err != nil || !strings.Contains(string(checkpoint.EngineState), `"steps":1`) {
		t.Fatalf("parked checkpoint = %+v err = %v, want the step counter", checkpoint, err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("pending approvals = %+v err = %v", approvals, err)
	}
	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != core.RunStatusCompleted || stored.StopReason != agent.StopBudgetExhausted || mutations != 1 {
		t.Fatalf("run = %s stop reason %q mutations %d", stored.Status, stored.StopReason, mutations)
	}
	if len(requests) != 3 || requests[1].ToolChoice != providers.ToolChoiceAuto || requests[2].ToolChoice != providers.ToolChoiceNone {
		t.Fatalf("requests = %d, want the resumed run to spend its one remaining step before the final turn", len(requests))
	}
}

func TestRecoveredRunKeepsItsBudgetCounters(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunBudgets(core.RunBudgets{User: agent.Budget{Steps: 2}})
	runtime := &recoveryRuntime{text: "Stopped after the restart."}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	app.WithRunStarter(orchestration.NewStub(app))
	_, run := saveCrashRecoveryRun(t, db, "budget-recovery", core.RunStatusRunning, false)
	if err := db.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{
		RunID: run.ID, Phase: core.RunCheckpointPhaseModel, EngineState: json.RawMessage(`{"steps":2}`), UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	waitForRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)

	if runtime.requestCount() != 1 || runtime.lastRequest().ToolChoice != providers.ToolChoiceNone {
		t.Fatalf("requests = %d, last tool choice %q, want only the final turn", runtime.requestCount(), runtime.lastRequest().ToolChoice)
	}
	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil || stored.StopReason != agent.StopBudgetExhausted {
		t.Fatalf("run = %+v err = %v", stored, err)
	}
}

func TestRunBudgetFollowsTheTrigger(t *testing.T) {
	budgets := core.RunBudgets{User: agent.Budget{Steps: 3}, Subagent: agent.Budget{Steps: 2}, Automation: agent.Budget{Steps: 1}}
	for _, tc := range []struct {
		name string
		want int
	}{
		{"user", 3},
		{"subagent", 2},
		{"automation", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			app.WithRunBudgets(budgets)
			app.WithRunStarter(&recordingRunStarter{})
			app.WithTools(tools.NewRegistry(changingTool("inspect_state")))
			withTools := 0
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
				if request.ToolChoice == providers.ToolChoiceNone {
					return providers.Response{Text: "Out of budget."}, nil
				}
				withTools++
				return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("call-%d", withTools), Name: "inspect_state", Arguments: []byte(fmt.Sprintf(`{"n":%d}`, withTools))}}}, nil
			})})
			status := core.RunStatusAccepted
			if tc.name == "automation" {
				status = core.RunStatusCompleted
			}
			session, run := saveCrashRecoveryRun(t, db, "trigger-"+tc.name, status, tc.name == "subagent")
			runID := run.ID
			if tc.name == "automation" {
				result, err := app.AcceptTriggeredRun(context.Background(), core.HandleTriggeredRunInput{TriggerID: "job-budget", SessionID: session.ID, Text: "scheduled check"})
				if err != nil {
					t.Fatal(err)
				}
				runID = result.Run.ID
			}

			if err := app.ExecuteRun(context.Background(), runID); err != nil {
				t.Fatal(err)
			}

			assertRecoveryRunStatus(t, db, runID, core.RunStatusCompleted)
			if withTools != tc.want {
				t.Fatalf("model calls with tools = %d, want %d", withTools, tc.want)
			}
		})
	}
}
```

- [ ] **Step 10: Run them to verify they fail**

Run: `go test ./internal/core/ -run 'TestNativeRunEndsWithAFinalTurnAtTheDefaultStepBudget|TestBudgetCountersSurviveAnApprovalPark|TestRecoveredRunKeepsItsBudgetCounters|TestRunBudgetFollowsTheTrigger' -timeout 60s`
Expected: build failure `app.WithRunBudgets undefined` / `undefined: core.RunBudgets`.

- [ ] **Step 11: Budget defaults per trigger** — create `internal/core/run_budget.go`:

```go
package core

import (
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
)

// RunBudgets are the daemon's default run budgets per trigger.
type RunBudgets struct {
	User       agent.Budget
	Subagent   agent.Budget
	Automation agent.Budget
}

// DefaultRunBudgets apply when the daemon configuration names none.
func DefaultRunBudgets() RunBudgets {
	return RunBudgets{
		User:       agent.Budget{Steps: 32, ActiveTime: 4 * time.Hour},
		Subagent:   agent.Budget{Steps: 32, ActiveTime: time.Hour},
		Automation: agent.Budget{Steps: 32, ActiveTime: 30 * time.Minute},
	}
}

// WithRunBudgets sets the default run budgets per trigger.
func (c *Core) WithRunBudgets(budgets RunBudgets) *Core {
	c.budgets = budgets
	return c
}

// defaultRunBudget is the daemon default for what started the run.
func (c *Core) defaultRunBudget(run Run, session Session) agent.Budget {
	switch {
	case isSubagentSession(session):
		return c.budgets.Subagent
	case run.Trigger == RunTriggerAutomation:
		return c.budgets.Automation
	default:
		return c.budgets.User
	}
}
```

In `internal/core/core.go` add the field `budgets RunBudgets` to `Core` (after `lifetime`) and `budgets: DefaultRunBudgets(),` to the literal in `New`.

- [ ] **Step 12: Checkpoint counters** — in `internal/core/run_checkpoint.go` add the imports `"log"` and `"github.com/Suren878/matrixclaw/internal/agent"`, replace `saveRunCheckpoint` and add the three functions below it:

```go
func (c *Core) saveRunCheckpoint(ctx context.Context, runID string, phase RunCheckpointPhase, toolCallID string, toolName string) error {
	return c.updateRunCheckpoint(ctx, runID, func(checkpoint *RunCheckpoint) {
		checkpoint.Phase = phase
		checkpoint.ToolCallID = normalizeText(toolCallID)
		checkpoint.ToolName = normalizeText(toolName)
	})
}

// saveEngineCheckpoint stores the engine's phase together with its run counters.
func (c *Core) saveEngineCheckpoint(ctx context.Context, state agent.State) error {
	counters, err := json.Marshal(state.Counters)
	if err != nil {
		return err
	}
	return c.updateRunCheckpoint(ctx, state.RunID, func(checkpoint *RunCheckpoint) {
		checkpoint.Phase = RunCheckpointPhase(state.Phase)
		checkpoint.ToolCallID = normalizeText(state.ToolCallID)
		checkpoint.ToolName = normalizeText(state.ToolName)
		checkpoint.EngineState = counters
	})
}

// updateRunCheckpoint applies update to the run's checkpoint and keeps every field
// update leaves alone, such as the recovery count or the engine counters.
func (c *Core) updateRunCheckpoint(ctx context.Context, runID string, update func(*RunCheckpoint)) error {
	store, ok := c.store.(RunCheckpointStore)
	if !ok {
		return nil
	}
	runID = normalizeText(runID)
	if runID == "" {
		return nil
	}
	checkpoint, err := store.GetRunCheckpoint(ctx, runID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	checkpoint.RunID = runID
	update(&checkpoint)
	checkpoint.UpdatedAt = c.now().UTC()
	return store.SaveRunCheckpoint(ctx, checkpoint)
}

// resumeCounters reads the engine counters of the run's checkpoint; a checkpoint
// without readable counters starts the budget from zero.
func (c *Core) resumeCounters(ctx context.Context, runID string) (agent.Counters, error) {
	checkpoint, ok, err := c.runCheckpoint(ctx, runID)
	if err != nil || !ok || len(checkpoint.EngineState) == 0 {
		return agent.Counters{}, err
	}
	var counters agent.Counters
	if err := json.Unmarshal(checkpoint.EngineState, &counters); err != nil {
		log.Printf("core: run %q has unreadable budget counters, starting from zero: %v", runID, err)
		return agent.Counters{}, nil
	}
	return counters, nil
}
```

In `internal/core/agent_journal.go` replace `Checkpoint`:

```go
func (j coreJournal) Checkpoint(ctx context.Context, state agent.State) error {
	return j.c.saveEngineCheckpoint(ctx, state)
}
```

- [ ] **Step 13: Give the task its budget and counters** — in `internal/core/run_execute.go`, `ExecuteRun`, replace

```go
	task, engine := c.nativeEngine(run, session, runtime)
	outcome, err := engine.Run(runCtx, task)
```

with

```go
	task, engine, err := c.nativeEngine(ctx, run, session, runtime)
	if err != nil {
		return c.failRunByID(ctx, run, err)
	}
	outcome, err := engine.Run(runCtx, task)
```

and replace `nativeEngine`:

```go
// nativeEngine builds the engine and task of one native run; the task resumes the
// counters of the run's checkpoint.
func (c *Core) nativeEngine(ctx context.Context, run Run, session Session, runtime providers.Runtime) (agent.Task, *agent.Engine, error) {
	resume, err := c.resumeCounters(ctx, run.ID)
	if err != nil {
		return agent.Task{}, nil, err
	}
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
		Budget:      c.defaultRunBudget(run, session),
		Resume:      resume,
	}
	return task, engine, nil
}
```

- [ ] **Step 14: Store the stop reason** — in `internal/core/run_outcome.go` replace `applyOutcome` and `applyInterruptedOutcome`:

```go
// applyOutcome persists how the engine left a native run and reports whether
// the run was kept for recovery.
func (c *Core) applyOutcome(ctx context.Context, run Run, outcome agent.Outcome) (bool, error) {
	switch outcome.Status {
	case agent.StatusCompleted:
		if outcome.Assistant == nil {
			return false, nil
		}
		run.StopReason = outcome.StopReason
		return false, c.completeAssistantTurn(ctx, &run, run.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		return false, c.setRunStatus(ctx, &run, RunStatusWaitingApproval, "")
	case agent.StatusCanceled:
		return false, c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusFailed:
		if outcome.MarkErrored && outcome.Assistant != nil {
			return false, c.persistAssistantError(ctx, run, outcome.Assistant, outcome.AssistantSaved, outcome.Err)
		}
		return false, c.failRunByID(ctx, run, outcome.Err)
	case agent.StatusInterrupted:
		return c.applyInterruptedOutcome(run, outcome)
	default:
		return false, fmt.Errorf("core: unknown run outcome %q", outcome.Status)
	}
}

// applyInterruptedOutcome commits what the run reached before its context stopped,
// or keeps it running with a recovery checkpoint (reported as true).
func (c *Core) applyInterruptedOutcome(run Run, outcome agent.Outcome) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()
	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	if latest.Status == RunStatusCanceled {
		return false, c.finishCanceledAssistant(ctx, outcome.Assistant, outcome.AssistantSaved)
	}
	if subagentRunStatusTerminal(latest.Status) {
		return false, nil
	}
	switch outcome.Reached {
	case agent.StatusCompleted:
		latest.StopReason = outcome.StopReason
		return false, c.completeAssistantTurn(ctx, &latest, latest.SessionID, outcome.Assistant, outcome.AssistantSaved)
	case agent.StatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, latest.SessionID, latest.ID)
		if err != nil {
			return false, err
		}
		if pending {
			return false, c.setRunStatus(ctx, &latest, RunStatusWaitingApproval, "")
		}
	}
	if err := c.preserveRunForRecovery(ctx, latest, outcome.Assistant, outcome.AssistantSaved); err != nil {
		return false, err
	}
	current, err := c.store.GetRun(ctx, latest.ID)
	if err != nil {
		return false, err
	}
	return !subagentRunStatusTerminal(current.Status), nil
}
```

- [ ] **Step 15: Run the core tests**

Run: `go test ./internal/core/ -timeout 120s`
Expected: PASS.

- [ ] **Step 16: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/
git status --short
git add internal/agent/budget.go internal/agent/notes.go internal/agent/budget_test.go internal/agent/task.go internal/agent/ports.go internal/agent/engine.go internal/agent/generate.go internal/agent/request.go internal/agent/engine_test.go internal/core/run_budget.go internal/core/run_budget_test.go internal/core/core.go internal/core/run_checkpoint.go internal/core/agent_journal.go internal/core/run_execute.go internal/core/run_outcome.go internal/core/native_run_characterization_test.go
git commit -m "$(cat <<'EOF'
feat(agent): stop runs on their budget with a tool-less final turn

Steps, active time and tokens are counted in checkpointed counters, a
note asks the model to wrap up at 80%, and the run ends completed with
stop_reason budget_exhausted. Core picks the budget by trigger.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Loop guard

**Files:**
- Create: `internal/agent/loopguard.go`, `internal/agent/loopguard_test.go`
- Modify: `internal/agent/budget.go` (`Counters`, `prepareStep`)
- Modify: `internal/agent/tools.go` (`finishCall`, `rejectCall`)

- [ ] **Step 1: Write the failing tests** — create `internal/agent/loopguard_test.go`:

```go
package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func statusTool(tools.Call) tools.Result {
	return tools.Result{Content: "working tree clean"}
}

func TestRepeatingACallWithoutProgressWarnsThenStops(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["status"] = statusTool
	layouts := []string{`{"path":"a","deep":true}`, `{"deep":true, "path":"a"}`}
	turns := make([]agenttest.Turn, 0, 6)
	for i := 1; i <= 5; i++ {
		turns = append(turns, calls(providers.ToolCall{ID: fmt.Sprintf("s%d", i), Name: "status", Arguments: []byte(layouts[i%2])}))
	}
	model := agenttest.NewScriptedModel(append(turns, text("Nothing changes; stopping here."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopLoopDetected || len(requests) != 6 || len(f.Tools.Calls) != 5 {
		t.Fatalf("outcome = %+v requests = %d tool calls = %d", outcome, len(requests), len(f.Tools.Calls))
	}
	if note := lastMessage(requests[3]); note.Role != "user" || !strings.Contains(note.Content, "You are repeating status") {
		t.Fatalf("request 4 ends with %+v, want the loop warning", note)
	}
	if strings.Contains(lastMessage(requests[4]).Content, "You are repeating") {
		t.Fatal("loop warning repeated within one streak")
	}
	final := requests[5]
	if final.ToolChoice != providers.ToolChoiceNone || !strings.Contains(lastMessage(final).Content, "5 times in a row") {
		t.Fatalf("final turn = %q / %+v", final.ToolChoice, lastMessage(final))
	}
	if notes := engineNotes(f.Journal.Messages); len(notes) != 2 {
		t.Fatalf("engine notes = %d, want the warning and the final-turn note", len(notes))
	}
}

func TestPollingThatReturnsNewOutputIsNotALoop(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["task_output"] = counterTool()
	model := agenttest.NewScriptedModel(append(toolSteps(6, "task_output"), text("Build finished."))...)

	outcome := run(t, f, model)

	if outcome.StopReason != agent.StopDone || len(engineNotes(f.Journal.Messages)) != 0 {
		t.Fatalf("outcome = %+v notes = %+v", outcome, engineNotes(f.Journal.Messages))
	}
}

func TestLoopStreakIsCheckpointed(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["status"] = statusTool
	model := agenttest.NewScriptedModel(append(toolSteps(3, "status"), text("Clean."))...)

	run(t, f, model)

	last := f.Journal.States[len(f.Journal.States)-1].Counters
	if last.LoopTool != "status" || last.LoopRepeats != 3 || !last.LoopWarned {
		t.Fatalf("last checkpoint counters = %+v", last)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/ -run 'Loop|Polling|Repeating'`
Expected: build failure `last.LoopTool undefined`.

- [ ] **Step 3: Implement the detector** — create `internal/agent/loopguard.go`:

```go
package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// Identical consecutive (tool, arguments, result) triples mean no progress: the
// model is warned at loopWarnRepeats and the run stops at loopStopRepeats.
const (
	loopWarnRepeats = 3
	loopStopRepeats = 5
)

// observeCall records a finished call; a call whose name, arguments and result
// match the previous one extends the no-progress streak.
func (c *Counters) observeCall(name string, args []byte, result tools.Result) {
	hash := callHash(name, args, result)
	if hash == c.LoopHash {
		c.LoopRepeats++
		return
	}
	c.LoopHash, c.LoopTool, c.LoopRepeats, c.LoopWarned = hash, name, 1, false
}

func callHash(name string, args []byte, result tools.Result) string {
	sum := sha256.New()
	sum.Write([]byte(strings.TrimSpace(name)))
	sum.Write([]byte{0})
	sum.Write(canonicalArgs(args))
	sum.Write([]byte{0})
	sum.Write([]byte(strings.TrimSpace(result.Content)))
	if result.IsError {
		sum.Write([]byte{1})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// canonicalArgs re-encodes JSON arguments with sorted keys, so a different layout
// does not hide a repeated call.
func canonicalArgs(args []byte) []byte {
	args = bytes.TrimSpace(args)
	if len(args) == 0 {
		return []byte(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return args
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return args
	}
	return canonical
}

func loopWarningText(tool string) string {
	return "You are repeating " + tool + " with the same arguments and getting the same result. Change your approach instead of calling it again."
}

func loopStopText(tool string) string {
	return fmt.Sprintf("You called %s with the same arguments and got the same result %d times in a row, so this run stops here. Do not call tools. Reply briefly: what is done, what remains, and how to continue.", tool, loopStopRepeats)
}
```

- [ ] **Step 4: Keep the streak in the counters and act on it** — in `internal/agent/budget.go` replace `Counters` and `prepareStep`:

```go
// Counters is what a run has used of its budget and its no-progress streak; it is
// checkpointed so a parked or restarted run continues from it.
type Counters struct {
	Steps       int           `json:"steps,omitempty"`
	Tokens      int64         `json:"tokens,omitempty"`
	Active      time.Duration `json:"active,omitempty"`
	WrapUpSent  bool          `json:"wrap_up_sent,omitempty"`
	LoopHash    string        `json:"loop_hash,omitempty"`
	LoopTool    string        `json:"loop_tool,omitempty"`
	LoopRepeats int           `json:"loop_repeats,omitempty"`
	LoopWarned  bool          `json:"loop_warned,omitempty"`
}
```

```go
// prepareStep journals the engine notes due before this step's model call and
// returns the stop reason when this step is the tool-less final turn.
func (r *run) prepareStep(ctx context.Context) (StopReason, error) {
	if r.counters.LoopRepeats >= loopStopRepeats {
		return StopLoopDetected, r.appendEngineMessage(ctx, loopStopText(r.counters.LoopTool))
	}
	if reached := r.exhausted(); reached != "" {
		return StopBudgetExhausted, r.appendEngineMessage(ctx, budgetStopText(reached))
	}
	if r.counters.LoopRepeats >= loopWarnRepeats && !r.counters.LoopWarned {
		if err := r.appendEngineMessage(ctx, loopWarningText(r.counters.LoopTool)); err != nil {
			return "", err
		}
		r.counters.LoopWarned = true
	}
	if !r.counters.WrapUpSent {
		if left := r.remaining(); left != "" {
			if err := r.appendEngineMessage(ctx, wrapUpText(left)); err != nil {
				return "", err
			}
			r.counters.WrapUpSent = true
		}
	}
	return "", nil
}
```

- [ ] **Step 5: Observe every finished and rejected call** — in `internal/agent/tools.go` replace `rejectCall` and `finishCall`:

```go
// rejectCall journals a call that may not run with its error as the result, so the
// model can correct it.
func (r *run) rejectCall(ctx context.Context, req callRequest, reason string) error {
	if err := r.history.append(ctx, r.callMessage(req, true)); err != nil {
		return err
	}
	result := tools.Result{Content: reason, IsError: true}
	if _, err := r.appendResult(ctx, req, result); err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
	return nil
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
	r.counters.observeCall(req.name, req.args, result)
	r.Sink.Emit(Event{Kind: EventToolFinished, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name, ResultMessageID: message.ID, Result: result})
	if err := r.Tools.Finish(ctx, req.name, call, result, message); err != nil {
		return err
	}
	return r.checkpoint(ctx, PhaseModel, "", "")
}
```

- [ ] **Step 6: Run the engine tests**

Run: `go test ./internal/agent/...`
Expected: PASS (the budget tests use `counterTool`, so no streak builds up there).

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/loopguard.go internal/agent/loopguard_test.go internal/agent/budget.go internal/agent/tools.go
git commit -m "$(cat <<'EOF'
feat(agent): stop runs that repeat a call without progress

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Replies cut by the output limit; refusal and content filter

**Files:**
- Create: `internal/agent/output_limit.go`, `internal/agent/output_limit_test.go`
- Modify: `internal/agent/engine.go` (`handleResponse`; `finishToolTurn` becomes `finishTurn`; import `fmt`)
- Modify: `internal/agent/generate.go` (`generateWithRetry`; drop the `agentcontext` import)
- Modify: `internal/agent/request.go` (`buildRequest`)
- Modify: `internal/agent/budget.go` (`Counters`)
- Modify: `internal/agent/context/errors.go`, `internal/agent/context/compact.go` (`StopReasonError` becomes the unexported `stopReasonError`)
- Modify: `internal/core/provider_contract_test.go`

- [ ] **Step 1: Write the failing engine tests** — create `internal/agent/output_limit_test.go`:

```go
package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type limitedModel struct {
	*agenttest.ScriptedModel
	current, ceiling int64
}

func (m limitedModel) OutputLimits() (int64, int64) { return m.current, m.ceiling }

func cut(text string) agenttest.Turn {
	return agenttest.Turn{Response: providers.Response{Text: text, StopReason: providers.StopMaxTokens}}
}

func TestCutReplyIsKeptAndContinued(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(cut("First half"), text("second half."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != "second half." {
		t.Fatalf("outcome = %+v", outcome)
	}
	partial := f.Journal.Messages[1]
	if partial.Role != transcript.MessageRoleAssistant || partial.Content != "First half" || !hasFinish(partial, "max_tokens") {
		t.Fatalf("partial reply = %+v", partial)
	}
	if notes := engineNotes(f.Journal.Messages); len(notes) != 1 || !strings.Contains(notes[0].Content, "cut by the output limit") {
		t.Fatalf("engine notes = %+v", notes)
	}
	second := model.Requests()[1]
	n := len(second.Messages)
	if second.Messages[n-2].Role != "assistant" || second.Messages[n-2].Content != "First half" || !strings.Contains(second.Messages[n-1].Content, "Continue exactly where you stopped") {
		t.Fatalf("second request tail = %+v", second.Messages[n-2:])
	}
}

func TestFourthCutInARowFailsTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(cut("part 1"), cut("part 2"), cut("part 3"), cut("part 4"))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || !outcome.MarkErrored || outcome.Assistant == nil || outcome.Assistant.Content != "part 4" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.Err == nil || outcome.Err.Error() != "reply cut by the output limit 4 times in a row" {
		t.Fatalf("error = %v", outcome.Err)
	}
	if len(model.Requests()) != 4 || len(engineNotes(f.Journal.Messages)) != 3 {
		t.Fatalf("requests = %d notes = %d", len(model.Requests()), len(engineNotes(f.Journal.Messages)))
	}
}

func TestContinuationsResetAfterAToolStep(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(cut("a"), cut("b"), cut("c"), calls(call("r1", "read")), cut("d"), text("end."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "end." {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestToolCallsCutByTheLimitStillRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("r1", "read")}, StopReason: providers.StopMaxTokens}},
		text("Done."),
	)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Tools.Calls) != 1 {
		t.Fatalf("outcome = %+v tool calls = %d", outcome, len(f.Tools.Calls))
	}
}

func TestEmptyCutRaisesTheOutputLimitOnce(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ceiling, want int64
	}{
		{"capped by the model", 6000, 6000},
		{"doubled", 0, 8000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agenttest.NewFixture()
			model := limitedModel{ScriptedModel: agenttest.NewScriptedModel(cut(""), text("Done.")), current: 4000, ceiling: tc.ceiling}

			outcome := run(t, f, model)

			requests := model.Requests()
			if outcome.Status != agent.StatusCompleted || len(requests) != 2 {
				t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
			}
			if requests[0].MaxOutputTokens != 0 || int64(requests[1].MaxOutputTokens) != tc.want {
				t.Fatalf("output limits = %d then %d, want 0 then %d", requests[0].MaxOutputTokens, requests[1].MaxOutputTokens, tc.want)
			}
			if len(engineNotes(f.Journal.Messages)) != 0 {
				t.Fatal("a raised limit must not journal a note")
			}
		})
	}
}

func TestEmptyCutFailsWhenTheLimitCannotBeRaised(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model func() agent.Model
		want  int
	}{
		{"second empty cut", func() agent.Model {
			return limitedModel{ScriptedModel: agenttest.NewScriptedModel(cut(""), cut("")), current: 4000}
		}, 2},
		{"no output limit", func() agent.Model { return agenttest.NewScriptedModel(cut("")) }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := agenttest.NewFixture()
			model := tc.model()

			outcome := run(t, f, model)

			if outcome.Status != agent.StatusFailed || outcome.Err == nil || outcome.Err.Error() != "reply cut by the output limit before any text" {
				t.Fatalf("outcome = %+v", outcome)
			}
			requests := len(model.(interface{ Requests() []providers.Request }).Requests())
			if requests != tc.want {
				t.Fatalf("requests = %d, want %d", requests, tc.want)
			}
		})
	}
}

func TestProviderStopsCompleteTheRun(t *testing.T) {
	for _, tc := range []struct {
		reason providers.StopReason
		text   string
		want   string
	}{
		{providers.StopRefusal, "I can't help with that.", "I can't help with that."},
		{providers.StopContentFilter, "", fmt.Sprintf("The provider stopped this reply (%s).", providers.StopContentFilter)},
	} {
		t.Run(string(tc.reason), func(t *testing.T) {
			f := agenttest.NewFixture()
			model := agenttest.NewScriptedModel(agenttest.Turn{Response: providers.Response{Text: tc.text, StopReason: tc.reason}})

			outcome := run(t, f, model)

			if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != tc.want || len(model.Requests()) != 1 {
				t.Fatalf("outcome = %+v", outcome)
			}
		})
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/ -run 'Cut|Continuations|ProviderStops|OutputLimit'`
Expected: FAIL — cut replies fail with `generation stopped before completion (max_tokens)`.

- [ ] **Step 3: Continuation and raise logic** — create `internal/agent/output_limit.go`:

```go
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// maxContinuations is how many replies in a row the output limit may cut before
// the run fails.
const maxContinuations = 3

const continueText = "Your reply was cut by the output limit. Continue exactly where you stopped, without repeating what you already wrote."

// continueCutReply keeps a reply cut by the output limit and asks the model to go
// on. A cut before any text (the adapter dropped a truncated tool call, or
// reasoning used the limit) is retried once with a raised limit.
func (r *run) continueCutReply(ctx context.Context, gen generation, response providers.Response) stepResult {
	if response.Text == "" {
		if !r.raiseOutputLimit() {
			return failedStep(errors.New("reply cut by the output limit before any text"))
		}
		return stepResult{kind: stepContinue}
	}
	if r.counters.Continuations >= maxContinuations {
		reply := finalReply(gen.assistant, response)
		return stepResult{kind: stepDone, assistant: &reply, saved: gen.saved, response: response, err: fmt.Errorf("reply cut by the output limit %d times in a row", maxContinuations+1), markErrored: true}
	}
	r.counters.Continuations++
	assistant := gen.assistant
	if err := r.finishTurn(ctx, &assistant, gen.saved, response, string(providers.StopMaxTokens)); err != nil {
		return failedStep(err)
	}
	if err := r.appendEngineMessage(ctx, continueText); err != nil {
		return failedStep(err)
	}
	return stepResult{kind: stepContinue}
}

// raiseOutputLimit doubles the model's output limit once, capped by its ceiling;
// false when the limit is unknown, already raised or at the ceiling.
func (r *run) raiseOutputLimit() bool {
	if r.counters.OutputLimit > 0 {
		return false
	}
	limiter, ok := r.task.Model.(providers.OutputLimiter)
	if !ok {
		return false
	}
	current, ceiling := limiter.OutputLimits()
	raised := current * 2
	if ceiling > 0 && raised > ceiling {
		raised = ceiling
	}
	if raised <= current {
		return false
	}
	r.counters.OutputLimit = int(raised)
	return true
}
```

- [ ] **Step 4: Counters for continuations and the raised limit** — in `internal/agent/budget.go` replace `Counters`:

```go
// Counters is what a run has used of its budget, its no-progress streak and its
// output-limit state; it is checkpointed so a parked or restarted run continues
// from it.
type Counters struct {
	Steps         int           `json:"steps,omitempty"`
	Tokens        int64         `json:"tokens,omitempty"`
	Active        time.Duration `json:"active,omitempty"`
	WrapUpSent    bool          `json:"wrap_up_sent,omitempty"`
	LoopHash      string        `json:"loop_hash,omitempty"`
	LoopTool      string        `json:"loop_tool,omitempty"`
	LoopRepeats   int           `json:"loop_repeats,omitempty"`
	LoopWarned    bool          `json:"loop_warned,omitempty"`
	Continuations int           `json:"continuations,omitempty"`
	OutputLimit   int           `json:"output_limit,omitempty"`
}
```

- [ ] **Step 5: Branch on the stop reason** — in `internal/agent/engine.go` add `"fmt"` to the imports, replace `handleResponse`, and replace `finishToolTurn` with `finishTurn`:

```go
func (r *run) handleResponse(ctx context.Context, gen generation) stepResult {
	response := gen.response
	response.Text = sanitizeAssistantOutput(response.Text)
	assistant := gen.assistant
	if len(response.ToolCalls) > 0 {
		r.counters.Continuations = 0
		if err := r.finishTurn(ctx, &assistant, gen.saved, response, "tool_calls"); err != nil {
			if ctx.Err() != nil {
				// Stopped before the tool turn was written: core seals the streamed preview.
				return stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, err: err}
			}
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
	switch response.StopReason {
	case providers.StopMaxTokens:
		return r.continueCutReply(ctx, gen, response)
	case providers.StopRefusal, providers.StopContentFilter:
		if response.Text == "" {
			response.Text = fmt.Sprintf("The provider stopped this reply (%s).", response.StopReason)
		}
		return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response}
	}
	if strings.TrimSpace(response.Text) == "" {
		return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response, err: providers.ErrEmptyResponse, markErrored: true}
	}
	return stepResult{kind: stepDone, assistant: &assistant, saved: gen.saved, response: response}
}

// finishTurn writes a reply the run goes on after (a tool step, or a reply cut by
// the output limit) with its reasoning, usage and finish reason.
func (r *run) finishTurn(ctx context.Context, assistant *transcript.Message, saved bool, response providers.Response, reason string) error {
	assistant.Content = response.Text
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	assistant.Parts = append(reasoningParts(response), transcript.NormalizeMessageParts(assistant.Content, nil)...)
	finish := usageFinishPart(response.Usage)
	if finish == nil {
		finish = &transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{}}
	}
	finish.Finish.Reason = reason
	assistant.Parts = append(assistant.Parts, *finish)
	assistant.UpdatedAt = r.Now()
	if saved {
		return r.history.finish(ctx, *assistant)
	}
	assistant.CreatedAt = assistant.UpdatedAt
	return r.history.append(ctx, *assistant)
}
```

- [ ] **Step 6: Accept cut and filtered replies from the provider** — in `internal/agent/generate.go` remove the `agentcontext` import and replace `generateWithRetry`:

```go
// generateWithRetry retries only failures that happened before any output was shown;
// a partial answer stays visible as failed instead of being replayed.
func (r *run) generateWithRetry(ctx context.Context, request providers.Request) (generation, error) {
	backoffs := [...]time.Duration{200 * time.Millisecond, 750 * time.Millisecond}
	for attempt := 0; ; attempt++ {
		gen, err := r.generate(ctx, request)
		if err == nil && sanitizeAssistantOutput(gen.response.Text) == "" && len(gen.response.ToolCalls) == 0 && !gen.response.StopReason.AllowsEmptyReply() {
			err = providers.ErrEmptyResponse
		}
		if err == nil || gen.saved || gen.assistant.Content != "" || ctx.Err() != nil || attempt >= len(backoffs) || !providers.IsRetryableGenerationError(err) {
			return gen, err
		}
		if err := r.Sleep(ctx, backoffs[attempt]); err != nil {
			return gen, err
		}
	}
}
```

In `internal/agent/request.go`, `buildRequest`, add `MaxOutputTokens: r.counters.OutputLimit,` to the `providers.Request{...}` literal (after `CacheKey`).

In `internal/agent/context/errors.go` rename `StopReasonError` to `stopReasonError` with the doc comment

```go
// stopReasonError fails a summary cut by the output limit or a content filter.
```

and update its only caller in `internal/agent/context/compact.go` (`if err := stopReasonError(response); err != nil {`).

- [ ] **Step 7: Run the engine tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 8: Update the core provider-contract tests** — in `internal/core/provider_contract_test.go` add the imports `"fmt"` and `"github.com/Suren878/matrixclaw/internal/agent"`, delete `TestTruncatedOrFilteredReplyFailsTheTurnWithoutRetry`, replace `TestRunStepRecordsTheProviderStopReason`, and add the two tests:

```go
func TestReplyCutFourTimesInARowFailsTheRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		calls++
		text := fmt.Sprintf("Part %d", calls)
		if err := providers.StreamText(ctx, text); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{Text: text, Provider: "recovery-test", StopReason: providers.StopMaxTokens}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cut-four", core.RunStatusAccepted, false)

	err := app.ExecuteRun(context.Background(), run.ID)

	if err == nil || err.Error() != "reply cut by the output limit 4 times in a row" {
		t.Fatalf("error = %v", err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusFailed)
	if calls != 4 {
		t.Fatalf("model calls = %d, want 4", calls)
	}
	parts, notes := 0, 0
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleAssistant && strings.HasPrefix(message.Content, "Part ") {
			parts++
		}
		if message.Origin == transcript.OriginEngine {
			notes++
		}
	}
	if parts != 4 || notes != 3 {
		t.Fatalf("kept parts = %d continuation notes = %d, want 4 and 3", parts, notes)
	}
}

func TestFilteredReplyCompletesTheRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Provider: "recovery-test", StopReason: providers.StopContentFilter}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "filtered", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), run.ID)
	if err != nil || stored.Status != core.RunStatusCompleted || stored.StopReason != agent.StopDone {
		t.Fatalf("run = %+v err = %v", stored, err)
	}
	if !hasAssistantContent(sessionMessages(t, db, session.ID), run.ID, "The provider stopped this reply (content_filter).") {
		t.Fatal("filtered reply has no visible note")
	}
}

func TestRunStepRecordsTheProviderStopReason(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{Text: "Cut off", Provider: "recovery-test", StopReason: providers.StopMaxTokens}, nil
		}
		return providers.Response{Text: "and finished.", Provider: "recovery-test", StopReason: providers.StopEndTurn}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "run-step-stop", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	steps, err := db.ListRunSteps(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].StopReason != "max_tokens" || steps[1].StopReason != "end_turn" {
		t.Fatalf("run steps=%+v, want max_tokens then end_turn", steps)
	}
}
```

(`hasAssistantContent` and `sessionMessages` are existing helpers in the `core_test` package.)

- [ ] **Step 9: Run the core tests**

Run: `go test ./internal/core/ -timeout 120s`
Expected: PASS.

- [ ] **Step 10: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/output_limit.go internal/agent/output_limit_test.go internal/agent/engine.go internal/agent/generate.go internal/agent/request.go internal/agent/budget.go internal/agent/context/errors.go internal/agent/context/compact.go internal/core/provider_contract_test.go
git commit -m "$(cat <<'EOF'
feat(agent): continue replies cut by the output limit

A cut reply is kept and continued up to three times in a row; a cut
before any text raises the output limit once. Refusals and content
filters complete the run instead of failing it.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Drop "use the fewest tool calls" from the prompt

**Files:**
- Modify: `internal/agent/prompt/guidance.go` (`ToolUseDiscipline`)
- Test: `internal/agent/prompt/guidance_test.go` (new)

- [ ] **Step 1: Write the failing test** — create `internal/agent/prompt/guidance_test.go`:

```go
package prompt

import (
	"strings"
	"testing"
)

func TestToolUseDisciplineDoesNotAskToMinimiseToolCalls(t *testing.T) {
	if strings.Contains(ToolUseDiscipline(), "fewest tool calls") {
		t.Fatalf("tool-use discipline still asks to minimise tool calls:\n%s", ToolUseDiscipline())
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/agent/prompt/`
Expected: FAIL.

- [ ] **Step 3: Remove the line** — in `ToolUseDiscipline` delete the line
`- Use the fewest tool calls that can reliably answer the user's request.`
and keep every other line unchanged.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/prompt/guidance.go internal/agent/prompt/guidance_test.go
git commit -m "$(cat <<'EOF'
fix(prompt): stop telling the model to minimise tool calls

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Per-session budget overrides (store, core, API)

**Files:**
- Create: `internal/core/types_budget.go`, `internal/store/sqlite_session_budgets.go`, `internal/store/sqlite_session_budgets_test.go`, `internal/api/budget.go`, `internal/api/budget_test.go`
- Modify: `internal/core/ports.go` (new `SessionBudgetStore`, `Store`)
- Modify: `internal/core/run_budget.go` (new `runBudget`, `SessionBudget`, `UpdateSessionBudget`, `sessionBudgetReport`)
- Modify: `internal/core/run_execute.go` (`nativeEngine`)
- Modify: `internal/core/contracts.go` (new `SessionBudgetResponse`)
- Modify: `internal/store/schema.go` (`applyCanonicalSchema`)
- Modify: `internal/api/sessions.go` (`handleSessionByID` routes)
- Test: `internal/core/run_budget_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/store/sqlite_session_budgets_test.go`:

```go
package store_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionBudgetOverridesRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	steps, tokens := 7, int64(0)
	if err := st.SaveSessionBudget(ctx, "s1", core.SessionBudget{Steps: &steps, Tokens: &tokens}, testEpoch); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetSessionBudget(ctx, "s1")
	if err != nil || stored.Steps == nil || *stored.Steps != 7 || stored.ActiveSeconds != nil || stored.Tokens == nil || *stored.Tokens != 0 {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	if err := st.SaveSessionBudget(ctx, "s1", core.SessionBudget{}, testEpoch); err != nil {
		t.Fatal(err)
	}
	if cleared, err := st.GetSessionBudget(ctx, "s1"); err != nil || !cleared.IsZero() {
		t.Fatalf("cleared = %+v err = %v", cleared, err)
	}
}
```

Append to `internal/core/run_budget_test.go`:

```go
func TestSessionBudgetOverridesTheDefault(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(changingTool("inspect_state")))
	withTools := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if request.ToolChoice == providers.ToolChoiceNone {
			return providers.Response{Text: "Out of budget."}, nil
		}
		withTools++
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("call-%d", withTools), Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "session-budget", core.RunStatusAccepted, false)
	steps := 1

	report, err := app.UpdateSessionBudget(context.Background(), session.ID, core.SessionBudget{Steps: &steps})
	if err != nil || report.Steps != 1 || report.ActiveSeconds != 4*3600 || report.Tokens != 0 {
		t.Fatalf("report = %+v err = %v", report, err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if withTools != 1 {
		t.Fatalf("model calls with tools = %d, want the session's 1 step", withTools)
	}
	zero := 0
	if _, err := app.UpdateSessionBudget(context.Background(), session.ID, core.SessionBudget{Steps: &zero}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("zero steps error = %v, want ErrInvalidInput", err)
	}
}
```

(add `"errors"` to that file's imports).

`internal/api/budget_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionBudgetEndpointStoresOverrides(t *testing.T) {
	server, _ := newAPITestServer(t)
	put := httptest.NewRecorder()
	server.Handler().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/v1/sessions/s1/budget", strings.NewReader(`{"steps":7,"tokens":0}`)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/budget", nil))
	var response core.SessionBudgetResponse
	if err := json.Unmarshal(get.Body.Bytes(), &response); err != nil {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
	budget := response.Budget
	if budget.Steps != 7 || budget.Tokens != 0 || budget.ActiveSeconds != 4*3600 || budget.Override.Steps == nil || budget.Override.ActiveSeconds != nil {
		t.Fatalf("budget = %+v", budget)
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodPut, "/v1/sessions/s1/budget", strings.NewReader(`{"steps":0}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid PUT status=%d", invalid.Code)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/store/ ./internal/core/ ./internal/api/ -run 'SessionBudget' -timeout 120s`
Expected: build failure `undefined: core.SessionBudget`.

- [ ] **Step 3: Types** — create `internal/core/types_budget.go`:

```go
package core

import (
	"fmt"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
)

// SessionBudget overrides the daemon run budget for one session. A nil field
// inherits the default; Tokens 0 means unlimited.
type SessionBudget struct {
	Steps         *int   `json:"steps,omitempty"`
	ActiveSeconds *int64 `json:"active_seconds,omitempty"`
	Tokens        *int64 `json:"tokens,omitempty"`
}

// IsZero reports whether the session overrides nothing.
func (b SessionBudget) IsZero() bool {
	return b.Steps == nil && b.ActiveSeconds == nil && b.Tokens == nil
}

func (b SessionBudget) apply(budget agent.Budget) agent.Budget {
	if b.Steps != nil {
		budget.Steps = *b.Steps
	}
	if b.ActiveSeconds != nil {
		budget.ActiveTime = time.Duration(*b.ActiveSeconds) * time.Second
	}
	if b.Tokens != nil {
		budget.Tokens = *b.Tokens
	}
	return budget
}

func (b SessionBudget) validate() error {
	switch {
	case b.Steps != nil && *b.Steps < 1:
		return fmt.Errorf("%w: budget steps must be at least 1", ErrInvalidInput)
	case b.ActiveSeconds != nil && *b.ActiveSeconds < 60:
		return fmt.Errorf("%w: budget active time must be at least one minute", ErrInvalidInput)
	case b.Tokens != nil && *b.Tokens < 0:
		return fmt.Errorf("%w: budget tokens must not be negative", ErrInvalidInput)
	default:
		return nil
	}
}

// SessionBudgetReport is a session's overrides and the budget its next run started
// by a user message gets.
type SessionBudgetReport struct {
	SessionID     string        `json:"session_id"`
	Override      SessionBudget `json:"override"`
	Steps         int           `json:"steps"`
	ActiveSeconds int64         `json:"active_seconds"`
	Tokens        int64         `json:"tokens"`
}
```

In `internal/core/contracts.go` add:

```go
type SessionBudgetResponse struct {
	Budget SessionBudgetReport `json:"budget"`
}
```

In `internal/core/ports.go` add

```go
type SessionBudgetStore interface {
	GetSessionBudget(ctx context.Context, sessionID string) (SessionBudget, error)
	SaveSessionBudget(ctx context.Context, sessionID string, budget SessionBudget, updatedAt time.Time) error
}
```

and `SessionBudgetStore` to the embedded list of `Store` (after `UsageStore`).

- [ ] **Step 4: Store** — in `internal/store/schema.go`, `applyCanonicalSchema`, insert before the final `return migrateMessageSearch(db)`:

```go
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_budgets (
    session_id TEXT PRIMARY KEY,
    steps INTEGER,
    active_seconds INTEGER,
    tokens INTEGER,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session budgets table: %w", err)
	}
```

Create `internal/store/sqlite_session_budgets.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

// GetSessionBudget returns the session's budget overrides; none stored is the zero value.
func (s *SQLiteStore) GetSessionBudget(ctx context.Context, sessionID string) (core.SessionBudget, error) {
	var steps, activeSeconds, tokens sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT steps, active_seconds, tokens FROM session_budgets WHERE session_id = ?`, strings.TrimSpace(sessionID)).Scan(&steps, &activeSeconds, &tokens)
	if errors.Is(err, sql.ErrNoRows) {
		return core.SessionBudget{}, nil
	}
	if err != nil {
		return core.SessionBudget{}, fmt.Errorf("store: get session budget: %w", err)
	}
	return core.SessionBudget{Steps: scannedCount[int](steps), ActiveSeconds: scannedCount[int64](activeSeconds), Tokens: scannedCount[int64](tokens)}, nil
}

// SaveSessionBudget replaces the session's budget overrides; an empty budget removes them.
func (s *SQLiteStore) SaveSessionBudget(ctx context.Context, sessionID string, budget core.SessionBudget, updatedAt time.Time) error {
	sessionID = strings.TrimSpace(sessionID)
	if budget.IsZero() {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM session_budgets WHERE session_id = ?`, sessionID); err != nil {
			return fmt.Errorf("store: delete session budget: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_budgets(session_id, steps, active_seconds, tokens, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    steps = excluded.steps,
    active_seconds = excluded.active_seconds,
    tokens = excluded.tokens,
    updated_at = excluded.updated_at`,
		sessionID,
		nullableCount(budget.Steps),
		nullableCount(budget.ActiveSeconds),
		nullableCount(budget.Tokens),
		formatTime(updatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save session budget: %w", err)
	}
	return nil
}

func nullableCount[T int | int64](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func scannedCount[T int | int64](value sql.NullInt64) *T {
	if !value.Valid {
		return nil
	}
	count := T(value.Int64)
	return &count
}
```

- [ ] **Step 5: Core** — in `internal/core/run_budget.go` add `"context"` to the imports and append:

```go
// runBudget is the budget a run starts with: the default for what started it with
// the session's overrides applied.
func (c *Core) runBudget(ctx context.Context, run Run, session Session) (agent.Budget, error) {
	override, err := c.store.GetSessionBudget(ctx, session.ID)
	if err != nil {
		return agent.Budget{}, err
	}
	return override.apply(c.defaultRunBudget(run, session)), nil
}

// SessionBudget reports the session's overrides and the budget of its next run.
func (c *Core) SessionBudget(ctx context.Context, sessionID string) (SessionBudgetReport, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return SessionBudgetReport{}, err
	}
	override, err := c.store.GetSessionBudget(ctx, session.ID)
	if err != nil {
		return SessionBudgetReport{}, err
	}
	return c.sessionBudgetReport(session, override), nil
}

// UpdateSessionBudget replaces the session's overrides; an empty budget restores
// the defaults.
func (c *Core) UpdateSessionBudget(ctx context.Context, sessionID string, budget SessionBudget) (SessionBudgetReport, error) {
	if err := budget.validate(); err != nil {
		return SessionBudgetReport{}, err
	}
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return SessionBudgetReport{}, err
	}
	if err := c.store.SaveSessionBudget(ctx, session.ID, budget, c.now().UTC()); err != nil {
		return SessionBudgetReport{}, err
	}
	return c.sessionBudgetReport(session, budget), nil
}

func (c *Core) sessionBudgetReport(session Session, override SessionBudget) SessionBudgetReport {
	budget := override.apply(c.defaultRunBudget(Run{}, session))
	return SessionBudgetReport{
		SessionID:     session.ID,
		Override:      override,
		Steps:         budget.Steps,
		ActiveSeconds: int64(budget.ActiveTime / time.Second),
		Tokens:        budget.Tokens,
	}
}
```

In `internal/core/run_execute.go`, `nativeEngine`, directly after the `resumeCounters` block insert

```go
	budget, err := c.runBudget(ctx, run, session)
	if err != nil {
		return agent.Task{}, nil, err
	}
```

and in the `agent.Task` literal replace `Budget:      c.defaultRunBudget(run, session),` with `Budget:      budget,`.

- [ ] **Step 6: API** — create `internal/api/budget.go`:

```go
package api

import (
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionBudget(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		report, err := s.core.SessionBudget(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionBudgetResponse{Budget: report})
	case http.MethodPut:
		var budget core.SessionBudget
		if !decodeJSONBody(w, r, &budget) {
			return
		}
		report, err := s.core.UpdateSessionBudget(r.Context(), sessionID, budget)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionBudgetResponse{Budget: report})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}
```

In `internal/api/sessions.go`, `handleSessionByID`, add `{suffix: "/budget", handle: s.handleSessionBudget},` to `childRoutes` (after the `/usage` entry).

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/store/ ./internal/core/ ./internal/api/ -timeout 120s`
Expected: PASS.

- [ ] **Step 8: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/types_budget.go internal/core/ports.go internal/core/run_budget.go internal/core/run_budget_test.go internal/core/run_execute.go internal/core/contracts.go internal/store/schema.go internal/store/sqlite_session_budgets.go internal/store/sqlite_session_budgets_test.go internal/api/budget.go internal/api/budget_test.go internal/api/sessions.go
git commit -m "$(cat <<'EOF'
feat(core): per-session run budget overrides

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: Budget defaults from the daemon configuration

**Files:**
- Modify: `internal/setup/types.go` (`DaemonConfig`, new `RunBudgetsConfig`, `RunBudgetConfig`)
- Create: `internal/daemoncmd/run_budgets.go`, `internal/daemoncmd/run_budgets_test.go`
- Modify: `internal/daemoncmd/bootstrap.go` (`bootstrapConfig`, `loadBootstrap`)
- Modify: `internal/daemoncmd/run.go` (core construction)

- [ ] **Step 1: Write the failing test** — create `internal/daemoncmd/run_budgets_test.go`:

```go
package daemoncmd

import (
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestRunBudgetsFromConfigOverrideOnlyTheGivenFields(t *testing.T) {
	budgets, err := runBudgetsFromConfig(setup.RunBudgetsConfig{
		User:       setup.RunBudgetConfig{Steps: 300, ActiveTime: "2h"},
		Automation: setup.RunBudgetConfig{Tokens: 50000},
	})
	if err != nil {
		t.Fatal(err)
	}
	defaults := core.DefaultRunBudgets()
	if budgets.User.Steps != 300 || budgets.User.ActiveTime != 2*time.Hour || budgets.User.Tokens != 0 {
		t.Fatalf("user budget = %+v", budgets.User)
	}
	if budgets.Subagent != defaults.Subagent {
		t.Fatalf("subagent budget = %+v, want the default %+v", budgets.Subagent, defaults.Subagent)
	}
	if budgets.Automation.Tokens != 50000 || budgets.Automation.Steps != defaults.Automation.Steps || budgets.Automation.ActiveTime != defaults.Automation.ActiveTime {
		t.Fatalf("automation budget = %+v", budgets.Automation)
	}
}

func TestRunBudgetsFromConfigRejectsBadValues(t *testing.T) {
	for _, cfg := range []setup.RunBudgetsConfig{
		{User: setup.RunBudgetConfig{ActiveTime: "soon"}},
		{Subagent: setup.RunBudgetConfig{ActiveTime: "10s"}},
		{Automation: setup.RunBudgetConfig{Steps: -1}},
	} {
		if _, err := runBudgetsFromConfig(cfg); err == nil {
			t.Errorf("config %+v accepted", cfg)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/daemoncmd/ -run RunBudgets`
Expected: build failure `undefined: setup.RunBudgetsConfig`.

- [ ] **Step 3: Configuration types** — in `internal/setup/types.go` replace `DaemonConfig` and add the two types after it:

```go
type DaemonConfig struct {
	HTTPAddr        string           `json:"http_addr"`
	DBPath          string           `json:"db_path"`
	Timezone        string           `json:"timezone,omitempty"`
	APIToken        string           `json:"api_token,omitempty"`
	AutostartOnBoot bool             `json:"autostart_on_boot"`
	Budgets         RunBudgetsConfig `json:"budgets,omitzero"`
}

// RunBudgetsConfig overrides the built-in run budget per trigger.
type RunBudgetsConfig struct {
	User       RunBudgetConfig `json:"user,omitzero"`
	Subagent   RunBudgetConfig `json:"subagent,omitzero"`
	Automation RunBudgetConfig `json:"automation,omitzero"`
}

// RunBudgetConfig: zero fields keep the default; ActiveTime is a Go duration such
// as "4h". Tokens 0 means unlimited.
type RunBudgetConfig struct {
	Steps      int    `json:"steps,omitempty"`
	ActiveTime string `json:"active_time,omitempty"`
	Tokens     int64  `json:"tokens,omitempty"`
}
```

- [ ] **Step 4: Conversion** — create `internal/daemoncmd/run_budgets.go`:

```go
package daemoncmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// runBudgetsFromConfig applies the configured budget fields over the built-in defaults.
func runBudgetsFromConfig(cfg setup.RunBudgetsConfig) (core.RunBudgets, error) {
	budgets := core.DefaultRunBudgets()
	var err error
	if budgets.User, err = applyRunBudgetConfig("user", budgets.User, cfg.User); err != nil {
		return core.RunBudgets{}, err
	}
	if budgets.Subagent, err = applyRunBudgetConfig("subagent", budgets.Subagent, cfg.Subagent); err != nil {
		return core.RunBudgets{}, err
	}
	if budgets.Automation, err = applyRunBudgetConfig("automation", budgets.Automation, cfg.Automation); err != nil {
		return core.RunBudgets{}, err
	}
	return budgets, nil
}

func applyRunBudgetConfig(name string, budget agent.Budget, cfg setup.RunBudgetConfig) (agent.Budget, error) {
	if cfg.Steps < 0 || cfg.Tokens < 0 {
		return budget, fmt.Errorf("daemon.budgets.%s: steps and tokens must not be negative", name)
	}
	if cfg.Steps > 0 {
		budget.Steps = cfg.Steps
	}
	if cfg.Tokens > 0 {
		budget.Tokens = cfg.Tokens
	}
	if value := strings.TrimSpace(cfg.ActiveTime); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration < time.Minute {
			return budget, fmt.Errorf("daemon.budgets.%s.active_time: want a duration of at least 1m, got %q", name, value)
		}
		budget.ActiveTime = duration
	}
	return budget, nil
}
```

- [ ] **Step 5: Wire it** — in `internal/daemoncmd/bootstrap.go`:
- add `Budgets core.RunBudgets` to `bootstrapConfig`;
- in `loadBootstrap` initialise it in the first literal: `cfg := bootstrapConfig{Addr: defaultDaemonAddr, DBPath: setup.DefaultDBPath(), Budgets: core.DefaultRunBudgets()}`;
- in the `default:` branch, right after `cfg.APIToken = strings.TrimSpace(setupCfg.Daemon.APIToken)`, insert

```go
		budgets, err := runBudgetsFromConfig(setupCfg.Daemon.Budgets)
		if err != nil {
			return bootstrapConfig{}, fmt.Errorf("load setup config %s: %w", service.Path(), err)
		}
		cfg.Budgets = budgets
```

In `internal/daemoncmd/run.go` add `WithRunBudgets(bootstrap.Budgets).` to the `core.New(sqliteStore).` chain (after `WithSessionLLMs(bootstrap.SessionLLMs).`).

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/daemoncmd/ ./internal/setup/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/setup/types.go internal/daemoncmd/run_budgets.go internal/daemoncmd/run_budgets_test.go internal/daemoncmd/bootstrap.go internal/daemoncmd/run.go
git commit -m "$(cat <<'EOF'
feat(daemon): read run budget defaults from the setup config

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 11: `/continue`

**Files:**
- Create: `internal/core/run_continue.go`, `internal/core/run_continue_test.go`, `internal/controlplane/continue.go`, `internal/controlplane/continue_test.go`
- Modify: `internal/core/types_run.go` (`HandleMessageInput`)
- Modify: `internal/core/runs.go` (`AcceptRun`)
- Modify: `internal/core/session_inputs.go` (`createAcceptedRun`, `consumeSessionInputAsRun`)
- Modify: `internal/core/ports.go` (`RunStore`)
- Modify: `internal/store/sqlite_messages.go` (new `GetLatestRunBySession`)
- Modify: `internal/daemonclient/sessions.go` (new `ContinueSession`)
- Modify: `internal/clientruntime/controlplane_runtime.go` (new `ContinueSession`)
- Modify: `internal/commandcatalog/catalog.go`, `internal/controlplane/catalog.go`, `internal/controlplane/dispatcher.go`
- Test: `internal/store/sqlite_runs_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/sqlite_runs_test.go` (add `"errors"` and `"github.com/Suren878/matrixclaw/internal/transcript"` to its imports):

```go
func TestLatestRunFollowsTheUserMessageOrder(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	if _, err := st.GetLatestRunBySession(ctx, "s1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("empty session error = %v, want ErrNotFound", err)
	}
	for _, id := range []string{"r1", "r2"} {
		message := transcript.Message{ID: "m_" + id, SessionID: "s1", RunID: id, Role: transcript.MessageRoleUser, Content: id, CreatedAt: testEpoch}
		run := core.Run{ID: id, SessionID: "s1", UserMessageID: message.ID, Status: core.RunStatusCompleted, StartedAt: testEpoch, UpdatedAt: testEpoch}
		if err := st.AcceptMessage(ctx, message, run); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := st.GetLatestRunBySession(ctx, "s1")
	if err != nil || latest.ID != "r2" {
		t.Fatalf("latest = %+v err = %v", latest, err)
	}
}
```

Create `internal/core/run_continue_test.go`:

```go
package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestContinueStartsARunThatContinuesTheLatestOne(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	session, previous := saveCrashRecoveryRun(t, db, "continue", core.RunStatusCompleted, false)

	result, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Continue: true})

	if err != nil {
		t.Fatal(err)
	}
	if result.Run.ContinuesRunID != previous.ID || result.UserMessage.Role != transcript.MessageRoleUser || result.UserMessage.Content != "Continue" {
		t.Fatalf("result = %+v", result)
	}
	stored, err := db.GetRun(context.Background(), result.Run.ID)
	if err != nil || stored.ContinuesRunID != previous.ID {
		t.Fatalf("stored run = %+v err = %v", stored, err)
	}
	if got := starter.count(result.Run.ID); got != 1 {
		t.Fatalf("starts = %d, want 1", got)
	}
}

func TestContinueIsRejectedWhileARunIsActive(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	session, _ := saveCrashRecoveryRun(t, db, "continue-busy", core.RunStatusRunning, false)

	_, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Continue: true})

	if !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("error = %v, want ErrRunActive", err)
	}
}

func TestContinueNeedsAnEarlierRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	now := runRecoveryTestTime()
	session := core.Session{ID: "session_empty", Title: "empty", Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw, Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}

	_, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Continue: true})

	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
```

Create `internal/controlplane/continue_test.go`:

```go
package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type continueRuntime struct {
	tokenReportRuntime
	continued []string
}

func (r *continueRuntime) ContinueSession(_ context.Context, externalKey string, sessionID string) (core.AcceptRunResult, error) {
	r.continued = append(r.continued, externalKey+"/"+sessionID)
	return core.AcceptRunResult{SessionID: sessionID}, nil
}

func TestContinueCommandContinuesTheCurrentSession(t *testing.T) {
	runtime := &continueRuntime{}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/continue")

	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Continuing the last run." || len(runtime.continued) != 1 || runtime.continued[0] != "key/s1" {
		t.Fatalf("result = %+v continued = %v", result, runtime.continued)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/store/ ./internal/core/ ./internal/controlplane/ -run 'Latest|Continue' -timeout 120s`
Expected: build failure (`GetLatestRunBySession undefined`, `unknown field Continue`).

- [ ] **Step 3: Store** — in `internal/store/sqlite_messages.go` add after `GetActiveRunBySession`:

```go
// GetLatestRunBySession returns the session's newest run, ordered by its user message.
func (s *SQLiteStore) GetLatestRunBySession(ctx context.Context, sessionID string) (core.Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE session_id = ?
ORDER BY (SELECT seq FROM messages WHERE messages.id = runs.user_message_id) DESC
LIMIT 1`, strings.TrimSpace(sessionID))

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Run{}, core.ErrNotFound
		}
		return core.Run{}, fmt.Errorf("store: get latest run by session: %w", err)
	}
	return run, nil
}
```

In `internal/core/ports.go` add `GetLatestRunBySession(ctx context.Context, sessionID string) (Run, error)` to `RunStore` (after `GetActiveRunBySession`).

- [ ] **Step 4: Core** — in `internal/core/types_run.go` add to `HandleMessageInput` (after `AllowAutoBindOne`):

```go
	// Continue starts a run that continues the session's latest run; Text is ignored.
	Continue bool `json:"continue,omitempty"`
```

In `internal/core/runs.go`, `AcceptRun`, make these the first lines of the function body:

```go
	if input.Continue {
		return c.acceptContinueRun(ctx, input)
	}
```

In `internal/core/session_inputs.go` replace `createAcceptedRun` (new last parameter `continuesRunID`):

```go
func (c *Core) createAcceptedRun(ctx context.Context, session Session, text string, parts []transcript.MessagePart, client string, externalKey string, capabilities ClientCapabilities, deliveryAddress json.RawMessage, continuesRunID string) (AcceptRunResult, error) {
	autoTitle := c.firstMessageAutoTitle(ctx, session, text)
	now := c.now().UTC()
	runID := c.newID("run")
	messageID := c.newID("msg")

	message := transcript.Message{
		ID:        messageID,
		SessionID: session.ID,
		RunID:     runID,
		Role:      transcript.MessageRoleUser,
		Content:   text,
		Parts:     parts,
		CreatedAt: now,
		UpdatedAt: now,
	}
	run := Run{
		ID:                 runID,
		SessionID:          session.ID,
		UserMessageID:      messageID,
		Client:             normalizeText(client),
		ExternalKey:        normalizeText(externalKey),
		ClientCapabilities: capabilities,
		ContinuesRunID:     normalizeText(continuesRunID),
		Status:             RunStatusAccepted,
		StartedAt:          now,
		UpdatedAt:          now,
	}
	delivery, hasDelivery, err := c.prepareSessionRunDelivery(run, text, parts, client, externalKey, deliveryAddress)
	if err != nil {
		return AcceptRunResult{}, err
	}
	var deliveries []ClientDelivery
	if hasDelivery {
		deliveries = append(deliveries, delivery)
	}
	if err := c.store.AcceptMessage(ctx, message, run, deliveries...); err != nil {
		return AcceptRunResult{}, err
	}
	c.applyAutoSessionTitle(ctx, session, autoTitle)
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: session.ID, RunID: run.ID, Payload: message})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: session.ID, RunID: run.ID, Payload: run})
	return AcceptRunResult{
		SessionID:   session.ID,
		Status:      AcceptRunStatusStarted,
		UserMessage: message,
		Run:         run,
	}, nil
}
```

and pass `""` as the new last argument at its two existing call sites: `consumeSessionInputAsRun` (`session_inputs.go`) and `AcceptRun` (`runs.go`).

Create `internal/core/run_continue.go`:

```go
package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// continueRunText is the user message of a run started by /continue.
const continueRunText = "Continue"

// acceptContinueRun starts a run that continues the session's latest run over the
// same history with a fresh budget.
func (c *Core) acceptContinueRun(ctx context.Context, input HandleMessageInput) (AcceptRunResult, error) {
	session, err := c.resolveSession(ctx, input)
	if err != nil {
		return AcceptRunResult{}, err
	}
	gate := c.sessionGate(session.ID)
	gate.Lock()
	result, err := c.createContinueRun(ctx, session, input)
	gate.Unlock()
	if err != nil {
		return AcceptRunResult{}, err
	}
	if err := c.startRun(ctx, result.Run.ID); err != nil {
		return c.failAcceptedRun(ctx, result, err)
	}
	return result, nil
}

func (c *Core) createContinueRun(ctx context.Context, session Session, input HandleMessageInput) (AcceptRunResult, error) {
	if _, err := c.store.GetActiveRunBySession(ctx, session.ID); err == nil {
		return AcceptRunResult{}, fmt.Errorf("%w: wait for the current run to finish before continuing", ErrRunActive)
	} else if !errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, err
	}
	latest, err := c.store.GetLatestRunBySession(ctx, session.ID)
	if errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, fmt.Errorf("%w: the session has no run to continue", ErrInvalidInput)
	}
	if err != nil {
		return AcceptRunResult{}, err
	}
	parts := transcript.NormalizeMessageParts(continueRunText, nil)
	return c.createAcceptedRun(ctx, session, continueRunText, parts, input.Client, input.ExternalKey, input.ClientCapabilities, input.DeliveryAddress, latest.ID)
}
```

- [ ] **Step 5: Client plumbing** — in `internal/daemonclient/sessions.go` add after `SendMessagePartsModeWithDelivery`:

```go
// ContinueSession starts a run that continues the session's latest run with a
// fresh budget.
func (c *Client) ContinueSession(ctx context.Context, sessionID string, workingDir string) (core.AcceptRunResult, error) {
	var response core.AcceptRunResult
	request := core.HandleMessageInput{
		Client:             c.ClientName,
		ExternalKey:        c.ExternalKey,
		ClientCapabilities: c.Capabilities,
		SessionID:          strings.TrimSpace(sessionID),
		WorkingDir:         strings.TrimSpace(workingDir),
		AllowAutoBindOne:   true,
		Continue:           true,
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/messages", request, &response); err != nil {
		return core.AcceptRunResult{}, err
	}
	return response, nil
}
```

In `internal/clientruntime/controlplane_runtime.go` add after `SendMessage`:

```go
// ContinueSession continues the session's latest run for the client behind
// externalKey, so its results are delivered there.
func (r ControlplaneRuntime) ContinueSession(ctx context.Context, externalKey string, sessionID string) (core.AcceptRunResult, error) {
	client, err := r.client(externalKey)
	if err != nil {
		return core.AcceptRunResult{}, err
	}
	return client.ContinueSession(ctx, sessionID, r.WorkingDir)
}
```

- [ ] **Step 6: Command** — in `internal/commandcatalog/catalog.go` add `CommandContinue CommandID = "continue"` to the constants and

```go
		{ID: CommandContinue, Command: "/continue", Description: "Continue the last run", Menu: false, Public: true},
```

to `Catalog()` after the `/usage` entry. In `internal/controlplane/catalog.go` add `CommandContinue = commandcatalog.CommandContinue` to the constant block.

In `internal/controlplane/dispatcher.go`:
- add the interface after `SessionSendRuntime`:

```go
type ContinueRuntime interface {
	ContinueSession(ctx context.Context, externalKey string, sessionID string) (core.AcceptRunResult, error)
}
```

- add the field `continuer ContinueRuntime` to `Dispatcher` (after `sender`);
- in `New` add `d.continuer, _ = runtime.(ContinueRuntime)` (after the `d.sender` line);
- in `Handle` add, after the `CommandUsage` case:

```go
	case CommandContinue:
		return d.handleContinue(ctx, externalKey)
```

Create `internal/controlplane/continue.go`:

```go
package controlplane

import "context"

func (d *Dispatcher) handleContinue(ctx context.Context, externalKey string) (Result, error) {
	if d.continuer == nil {
		return unsupportedRuntime("continue"), nil
	}
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if sessionID == "" {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if _, err := d.continuer.ContinueSession(ctx, externalKey, sessionID); err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "Continuing the last run."}, nil
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/store/ ./internal/core/ ./internal/controlplane/ ./internal/clientruntime/ ./internal/daemonclient/ -timeout 120s`
Expected: PASS.

- [ ] **Step 8: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/run_continue.go internal/core/run_continue_test.go internal/core/types_run.go internal/core/runs.go internal/core/session_inputs.go internal/core/ports.go internal/store/sqlite_messages.go internal/store/sqlite_runs_test.go internal/daemonclient/sessions.go internal/clientruntime/controlplane_runtime.go internal/commandcatalog/catalog.go internal/controlplane/catalog.go internal/controlplane/dispatcher.go internal/controlplane/continue.go internal/controlplane/continue_test.go
git commit -m "$(cat <<'EOF'
feat(core): /continue starts a fresh-budget run after the latest one

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: `/budget`

**Files:**
- Create: `internal/controlplane/budget.go`, `internal/controlplane/budget_test.go`
- Modify: `internal/daemonclient/sessions.go` (new `SessionBudget`, `UpdateSessionBudget`)
- Modify: `internal/clientruntime/controlplane_runtime.go` (new `SessionBudget`, `UpdateSessionBudget`)
- Modify: `internal/commandcatalog/catalog.go`, `internal/controlplane/catalog.go`, `internal/controlplane/dispatcher.go`

- [ ] **Step 1: Write the failing tests** — create `internal/controlplane/budget_test.go`:

```go
package controlplane

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type budgetRuntime struct {
	tokenReportRuntime
	report core.SessionBudgetReport
	saved  []core.SessionBudget
}

func (r *budgetRuntime) SessionBudget(context.Context, string) (core.SessionBudgetReport, error) {
	return r.report, nil
}

func (r *budgetRuntime) UpdateSessionBudget(_ context.Context, _ string, budget core.SessionBudget) (core.SessionBudgetReport, error) {
	r.saved = append(r.saved, budget)
	r.report.Override = budget
	if budget.Steps != nil {
		r.report.Steps = *budget.Steps
	}
	return r.report, nil
}

func TestBudgetCommandShowsTheEffectiveBudget(t *testing.T) {
	runtime := &budgetRuntime{report: core.SessionBudgetReport{SessionID: "s1", Steps: 32, ActiveSeconds: 4 * 3600}}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/budget")

	if err != nil {
		t.Fatal(err)
	}
	want := "Steps: 32 (default)\nActive time: 4h (default)\nTokens: unlimited (default)"
	if result.Info == nil || !strings.HasPrefix(result.Info.Text, want) || len(runtime.saved) != 0 {
		t.Fatalf("info = %+v, want text starting with:\n%s", result.Info, want)
	}
}

func TestBudgetCommandChangesOneLimit(t *testing.T) {
	steps := 50
	for _, tc := range []struct {
		command string
		check   func(core.SessionBudget) bool
	}{
		{"/budget steps 50", func(b core.SessionBudget) bool { return b.Steps != nil && *b.Steps == 50 }},
		{"/budget time 90m", func(b core.SessionBudget) bool {
			return b.ActiveSeconds != nil && *b.ActiveSeconds == 5400 && b.Steps != nil
		}},
		{"/budget tokens off", func(b core.SessionBudget) bool { return b.Tokens != nil && *b.Tokens == 0 }},
		{"/budget reset", func(b core.SessionBudget) bool { return b.IsZero() }},
	} {
		runtime := &budgetRuntime{report: core.SessionBudgetReport{SessionID: "s1", Override: core.SessionBudget{Steps: &steps}}}

		result, err := New(runtime, "").Handle(context.Background(), "key", tc.command)

		if err != nil || result.Info == nil || len(runtime.saved) != 1 || !tc.check(runtime.saved[0]) {
			t.Errorf("%s: result = %+v saved = %+v err = %v", tc.command, result, runtime.saved, err)
		}
	}
}

func TestBudgetCommandRejectsBadInput(t *testing.T) {
	for _, command := range []string{"/budget steps 0", "/budget time soon", "/budget speed 3"} {
		runtime := &budgetRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || result.Text != budgetUsage || len(runtime.saved) != 0 {
			t.Errorf("%s: result = %+v saved = %+v err = %v", command, result, runtime.saved, err)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/controlplane/ -run Budget`
Expected: build failure `undefined: budgetUsage`.

- [ ] **Step 3: Command** — in `internal/commandcatalog/catalog.go` add `CommandBudget CommandID = "budget"` and

```go
		{ID: CommandBudget, Command: "/budget", Description: "Run budget", Menu: false, Public: true},
```

after the `/continue` entry. In `internal/controlplane/catalog.go` add `CommandBudget = commandcatalog.CommandBudget`.

In `internal/controlplane/dispatcher.go`:
- add after `UsageRuntime`:

```go
type BudgetRuntime interface {
	SessionBudget(ctx context.Context, sessionID string) (core.SessionBudgetReport, error)
	UpdateSessionBudget(ctx context.Context, sessionID string, budget core.SessionBudget) (core.SessionBudgetReport, error)
}
```

- add the field `budget BudgetRuntime` to `Dispatcher` (after `usage`), `d.budget, _ = runtime.(BudgetRuntime)` to `New` (after the `d.usage` line), and in `Handle` after the `CommandContinue` case:

```go
	case CommandBudget:
		return d.handleBudget(ctx, externalKey, args)
```

Create `internal/controlplane/budget.go`:

```go
package controlplane

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

const budgetUsage = "Usage: /budget [steps N | time 2h | tokens N | tokens off | reset]"

func (d *Dispatcher) handleBudget(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.budget == nil {
		return unsupportedRuntime("budget"), nil
	}
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if sessionID == "" {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	report, err := d.budget.SessionBudget(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	if fields := strings.Fields(strings.ToLower(args)); len(fields) > 0 {
		override, ok := budgetOverride(report.Override, fields)
		if !ok {
			return Result{Handled: true, Text: budgetUsage}, nil
		}
		if report, err = d.budget.UpdateSessionBudget(ctx, sessionID, override); err != nil {
			return Result{}, err
		}
	}
	info := budgetInfoData(report)
	return Result{Handled: true, Info: &info}, nil
}

// budgetOverride applies one /budget change to the session's overrides.
func budgetOverride(current core.SessionBudget, fields []string) (core.SessionBudget, bool) {
	if len(fields) == 1 && fields[0] == "reset" {
		return core.SessionBudget{}, true
	}
	if len(fields) != 2 {
		return current, false
	}
	switch fields[0] {
	case "steps":
		steps, err := strconv.Atoi(fields[1])
		if err != nil || steps < 1 {
			return current, false
		}
		current.Steps = &steps
	case "time":
		duration, err := time.ParseDuration(fields[1])
		if err != nil || duration < time.Minute {
			return current, false
		}
		seconds := int64(duration / time.Second)
		current.ActiveSeconds = &seconds
	case "tokens":
		var tokens int64
		if fields[1] != "off" {
			parsed, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || parsed < 1 {
				return current, false
			}
			tokens = parsed
		}
		current.Tokens = &tokens
	default:
		return current, false
	}
	return current, true
}

func budgetInfoData(report core.SessionBudgetReport) InfoData {
	rows := []InfoRow{
		{Label: "Steps", Value: budgetValue(strconv.Itoa(report.Steps), report.Override.Steps != nil)},
		{Label: "Active time", Value: budgetValue(budgetDuration(time.Duration(report.ActiveSeconds)*time.Second), report.Override.ActiveSeconds != nil)},
		{Label: "Tokens", Value: budgetValue(budgetTokens(report.Tokens), report.Override.Tokens != nil)},
	}
	lines := make([]string, 0, len(rows)+2)
	for _, row := range rows {
		lines = append(lines, row.Label+": "+row.Value)
	}
	lines = append(lines, "", budgetUsage)
	return InfoData{Title: "Run Budget", Text: strings.Join(lines, "\n"), Rows: rows}
}

func budgetValue(value string, session bool) string {
	if session {
		return value + " (session)"
	}
	return value + " (default)"
}

func budgetTokens(tokens int64) string {
	if tokens == 0 {
		return "unlimited"
	}
	return formatShortNumber(int(tokens))
}

// budgetDuration renders whole hours and minutes, such as 4h or 1h30m.
func budgetDuration(d time.Duration) string {
	if d <= 0 {
		return "unlimited"
	}
	text := strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}
```

- [ ] **Step 4: Client plumbing** — in `internal/daemonclient/sessions.go` add after `SessionUsage`:

```go
func (c *Client) SessionBudget(ctx context.Context, sessionID string) (core.SessionBudgetReport, error) {
	var response core.SessionBudgetResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/budget"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.SessionBudgetReport{}, err
	}
	return response.Budget, nil
}

func (c *Client) UpdateSessionBudget(ctx context.Context, sessionID string, budget core.SessionBudget) (core.SessionBudgetReport, error) {
	var response core.SessionBudgetResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/budget"
	if err := c.doJSON(ctx, http.MethodPut, path, budget, &response); err != nil {
		return core.SessionBudgetReport{}, err
	}
	return response.Budget, nil
}
```

In `internal/clientruntime/controlplane_runtime.go` add after `SessionUsage`:

```go
func (r ControlplaneRuntime) SessionBudget(ctx context.Context, sessionID string) (core.SessionBudgetReport, error) {
	client, err := r.client("")
	if err != nil {
		return core.SessionBudgetReport{}, err
	}
	return client.SessionBudget(ctx, sessionID)
}

func (r ControlplaneRuntime) UpdateSessionBudget(ctx context.Context, sessionID string, budget core.SessionBudget) (core.SessionBudgetReport, error) {
	client, err := r.client("")
	if err != nil {
		return core.SessionBudgetReport{}, err
	}
	return client.UpdateSessionBudget(ctx, sessionID, budget)
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/controlplane/ ./internal/clientruntime/ ./internal/daemonclient/`
Expected: PASS.

- [ ] **Step 6: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/controlplane/budget.go internal/controlplane/budget_test.go internal/controlplane/dispatcher.go internal/controlplane/catalog.go internal/commandcatalog/catalog.go internal/daemonclient/sessions.go internal/clientruntime/controlplane_runtime.go
git commit -m "$(cat <<'EOF'
feat(controlplane): /budget shows and sets the session's run budget

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 13: TUI — engine notes and the continue hint

Engine notes already arrive as `role: system` rows, which the TUI renders; this task renders them as plain notes and shows a `/continue` hint when a run stops early.

**Files:**
- Create: `internal/controlplane/run_stop.go` (`StopNotice`, shared with Telegram)
- Modify: `clients/terminal/chat/viewmodel/surface_adapter.go` (`ToSurfaceMessage`)
- Create: `clients/terminal/chat/viewmodel/surface_adapter_test.go`
- Create: `clients/terminal/chat/runtime/run_stop_notice.go`, `clients/terminal/chat/runtime/run_stop_notice_test.go`
- Modify: `clients/terminal/chat/runtime/app_runtime_events.go` (`handleRunUpdatedEvent`)

- [ ] **Step 1: Write the failing tests**

`clients/terminal/chat/viewmodel/surface_adapter_test.go`:

```go
package viewmodel

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestEngineNotesRenderAsPlainSystemNotes(t *testing.T) {
	text := "Budget note: about 2 steps left."
	note := ToSurfaceMessage(transcript.Message{ID: "n1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: text, Parts: transcript.NormalizeMessageParts(text, nil)})
	if note.Role != surfacemessage.System || !note.IsSummaryMessage || note.Content().Text != text {
		t.Fatalf("engine note = %+v", note)
	}
	plain := ToSurfaceMessage(transcript.Message{ID: "s1", Role: transcript.MessageRoleSystem, Content: "system text"})
	if plain.IsSummaryMessage {
		t.Fatalf("plain system message = %+v", plain)
	}
}
```

`clients/terminal/chat/runtime/run_stop_notice_test.go`:

```go
package runtime

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestStoppedRunShowsHowToContinueUntilTheNextRunStarts(t *testing.T) {
	m := &appModel{}

	m.showRunStopNotice(core.Run{ID: "r1", Status: core.RunStatusCompleted, StopReason: agent.StopDone})
	if len(m.transientMessages) != 0 {
		t.Fatalf("finished run left a notice: %+v", m.transientMessages)
	}
	m.showRunStopNotice(core.Run{ID: "r1", Status: core.RunStatusCompleted, StopReason: agent.StopBudgetExhausted})
	if len(m.transientMessages) != 1 || !strings.Contains(m.transientMessages[0].Content().Text, "/continue") {
		t.Fatalf("notice = %+v", m.transientMessages)
	}
	m.showRunStopNotice(core.Run{ID: "r2", Status: core.RunStatusRunning})
	if len(m.transientMessages) != 0 {
		t.Fatalf("notice kept while a run is active: %+v", m.transientMessages)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./clients/terminal/chat/...`
Expected: FAIL / build failure (`m.showRunStopNotice undefined`; engine note not a summary message).

- [ ] **Step 3: Shared stop notice** — create `internal/controlplane/run_stop.go`:

```go
package controlplane

import "github.com/Suren878/matrixclaw/internal/agent"

// StopNotice explains why a completed run stopped before its work was done; ""
// when it finished normally.
func StopNotice(reason agent.StopReason) string {
	switch reason {
	case agent.StopBudgetExhausted:
		return "The run stopped at its budget."
	case agent.StopLoopDetected:
		return "The run stopped because it kept repeating the same step."
	default:
		return ""
	}
}
```

- [ ] **Step 4: Plain notes** — in `clients/terminal/chat/viewmodel/surface_adapter.go`, `ToSurfaceMessage`, add `IsSummaryMessage: message.Origin == transcript.OriginEngine,` to the `out := surfacemessage.Message{...}` literal (after `UpdatedAt`).

- [ ] **Step 5: The hint** — create `clients/terminal/chat/runtime/run_stop_notice.go`:

```go
package runtime

import (
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

const runStopNoticeMessageID = "run-stop-notice"

// showRunStopNotice tells how to continue a run that stopped early and drops the
// hint once a run is active again.
func (m *appModel) showRunStopNotice(run core.Run) {
	if runIsActive(&run) {
		m.removeTransientMessage(runStopNoticeMessageID)
		return
	}
	notice := controlplane.StopNotice(run.StopReason)
	if notice == "" {
		return
	}
	now := time.Now().Unix()
	m.upsertTransientMessage(surfacemessage.Message{
		ID:               runStopNoticeMessageID,
		Role:             surfacemessage.System,
		Parts:            []surfacemessage.ContentPart{surfacemessage.TextContent{Text: notice + " Send /continue to go on."}},
		CreatedAt:        now,
		UpdatedAt:        now,
		IsSummaryMessage: true,
	})
}
```

In `clients/terminal/chat/runtime/app_runtime_events.go`, `handleRunUpdatedEvent`, add `m.showRunStopNotice(run)` directly after `m.setBusy(runIsActive(&run))`.

- [ ] **Step 6: Run the tests**

Run: `go test ./clients/terminal/... ./internal/controlplane/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/controlplane/run_stop.go clients/terminal/chat/viewmodel/surface_adapter.go clients/terminal/chat/viewmodel/surface_adapter_test.go clients/terminal/chat/runtime/run_stop_notice.go clients/terminal/chat/runtime/run_stop_notice_test.go clients/terminal/chat/runtime/app_runtime_events.go
git commit -m "$(cat <<'EOF'
feat(tui): show engine notes and a /continue hint after an early stop

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 14: Telegram — engine notes and the Continue button

**Files:**
- Create: `clients/telegram/run_notes.go`, `clients/telegram/run_notes_test.go`
- Modify: `clients/telegram/worker_types.go` (`runDeliveryState`)
- Modify: `clients/telegram/delivery.go` (`newRunDeliveryState`, `deliverChatRunDelivery`, `deliverActiveRunProgress`)

The button sends `/continue` through the existing picker-command callback, so the controlplane handler from Task 11 runs it and the button message is edited to "Continuing the last run.".

- [ ] **Step 1: Write the failing tests** — create `clients/telegram/run_notes_test.go`:

```go
package telegram

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestEngineNotesAreSentOnceAndSilently(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := &Worker{api: api}
	target := chatTarget{chatID: 7, externalKey: "7"}
	state := newRunDeliveryState()
	messages := []transcript.Message{
		{ID: "n1", RunID: "run-1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note: about 2 steps left."},
		{ID: "n2", RunID: "run-2", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "another run"},
		{ID: "s1", RunID: "run-1", Role: transcript.MessageRoleSystem, Content: "not an engine note"},
	}

	for i := 0; i < 2; i++ {
		if err := worker.renderEngineNotes(context.Background(), target, messages, "run-1", state); err != nil {
			t.Fatal(err)
		}
	}

	if api.sendCount() != 1 || api.messages[0].Text != "Note: Budget note: about 2 steps left." || !api.messages[0].DisableNotification {
		t.Fatalf("sent = %+v", api.messages)
	}
}

func TestRunStoppedEarlyOffersAContinueButtonOnce(t *testing.T) {
	api := &runRenderBotAPI{}
	worker := &Worker{api: api}
	target := chatTarget{chatID: 7, externalKey: "7"}
	state := newRunDeliveryState()
	stopped := core.Run{ID: "run-1", Status: core.RunStatusCompleted, StopReason: agent.StopBudgetExhausted}

	for i := 0; i < 2; i++ {
		if err := worker.offerContinue(context.Background(), target, stopped, state); err != nil {
			t.Fatal(err)
		}
	}
	finished := core.Run{ID: "run-2", Status: core.RunStatusCompleted, StopReason: agent.StopDone}
	if err := worker.offerContinue(context.Background(), target, finished, newRunDeliveryState()); err != nil {
		t.Fatal(err)
	}

	if api.sendCount() != 1 {
		t.Fatalf("sent = %+v, want one offer", api.messages)
	}
	markup, ok := api.messages[0].ReplyMarkup.(*InlineKeyboardMarkup)
	if !ok || len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 1 || markup.InlineKeyboard[0][0].CallbackData != commandCallbackData("/continue") {
		t.Fatalf("reply markup = %#v", api.messages[0].ReplyMarkup)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./clients/telegram/ -run 'EngineNotes|ContinueButton'`
Expected: build failure `worker.renderEngineNotes undefined`.

- [ ] **Step 3: State** — in `clients/telegram/worker_types.go` replace `runDeliveryState`:

```go
type runDeliveryState struct {
	statusSent        bool
	continueOffered   bool
	assistant         map[string]sentAssistantMessage
	approvals         map[string]int64
	toolCalls         map[string]sentToolCallStatus
	voiceResults      map[string]int64
	voiceFingerprints map[string]int64
	notes             map[string]struct{}
}
```

In `clients/telegram/delivery.go` replace `newRunDeliveryState`:

```go
func newRunDeliveryState() *runDeliveryState {
	return &runDeliveryState{
		assistant:         map[string]sentAssistantMessage{},
		approvals:         map[string]int64{},
		toolCalls:         map[string]sentToolCallStatus{},
		voiceResults:      map[string]int64{},
		voiceFingerprints: map[string]int64{},
		notes:             map[string]struct{}{},
	}
}
```

- [ ] **Step 4: Rendering** — create `clients/telegram/run_notes.go`:

```go
package telegram

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/commandcatalog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// renderEngineNotes sends each engine note of the run once, without a notification.
func (w *Worker) renderEngineNotes(ctx context.Context, target chatTarget, messages []transcript.Message, runID string, state *runDeliveryState) error {
	runID = strings.TrimSpace(runID)
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != runID || message.Origin != transcript.OriginEngine {
			continue
		}
		if _, sent := state.notes[message.ID]; sent {
			continue
		}
		text := strings.TrimSpace(message.Content)
		if text == "" {
			continue
		}
		if _, err := w.sendTelegramMessage(silentTelegramDelivery(ctx), SendMessageRequest{ChatID: target.chatID, Text: clipTelegramText("Note: " + text)}); err != nil {
			return err
		}
		state.notes[message.ID] = struct{}{}
	}
	return nil
}

// offerContinue sends the stop notice of a run that stopped before its work was
// done, with a Continue button.
func (w *Worker) offerContinue(ctx context.Context, target chatTarget, run core.Run, state *runDeliveryState) error {
	notice := controlplane.StopNotice(run.StopReason)
	if notice == "" || state.continueOffered {
		return nil
	}
	markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{commandButton("Continue", catalogCommand(commandcatalog.CommandContinue, ""))}}}
	if _, err := w.sendTelegramMessage(ctx, SendMessageRequest{ChatID: target.chatID, Text: notice, ReplyMarkup: markup}); err != nil {
		return err
	}
	state.continueOffered = true
	return nil
}
```

- [ ] **Step 5: Hook them into run delivery** — in `clients/telegram/delivery.go` replace `deliverChatRunDelivery` and `deliverActiveRunProgress`:

```go
func (w *Worker) deliverChatRunDelivery(ctx context.Context, target chatTarget, sessionID string, runID string, deliveryID string) error {
	daemon := w.daemon(target.externalKey)
	run, err := daemon.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	w.updateRunTypingIndicator(ctx, target, &run)
	switch run.Status {
	case core.RunStatusWaitingApproval:
		return w.deliverRunApprovals(ctx, target, daemon, sessionID, runID)
	case core.RunStatusAccepted, core.RunStatusRunning:
		return w.deliverActiveRunProgress(ctx, target, daemon, sessionID, runID)
	case core.RunStatusCompleted, core.RunStatusFailed, core.RunStatusCanceled:
	default:
		return nil
	}

	messages, err := daemon.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return err
	}
	state := w.runRenderState(target.externalKey, runID)
	if err := w.renderToolCallUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderVoiceToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderEngineNotes(ctx, target, messages, runID, state); err != nil {
		return err
	}
	assistantCtx := ctx
	if run.Status != core.RunStatusCompleted {
		assistantCtx = silentTelegramDelivery(ctx)
	}
	if err := w.renderAssistantUpdates(assistantCtx, target, messages, runID, state); err != nil {
		return err
	}
	if run.Status == core.RunStatusCompleted {
		if err := w.offerContinue(ctx, target, run, state); err != nil {
			return err
		}
	}
	if run.Status != core.RunStatusCompleted && !state.statusSent {
		if err := w.sendText(ctx, target, renderRunStatus(run)); err != nil {
			return err
		}
		state.statusSent = true
	}
	if err := w.acknowledgeSentDelivery(ctx, daemon, deliveryID); err != nil {
		return err
	}
	w.clearRunRenderState(target.externalKey, runID)
	return nil
}

func (w *Worker) deliverActiveRunProgress(ctx context.Context, target chatTarget, daemon *daemonclient.Client, sessionID string, runID string) error {
	messages, err := daemon.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return err
	}
	state := w.runRenderState(target.externalKey, runID)
	if err := w.renderAssistantProgressUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderToolCallUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderVoiceToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderToolResultUpdates(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderEngineNotes(ctx, target, messages, runID, state); err != nil {
		return err
	}
	if err := w.renderAssistantStreamUpdate(ctx, target, messages, runID, state); err != nil {
		if IsRetryable(err) {
			return err
		}
		log.Printf("telegram: assistant stream update failed chat=%d run=%s: %v", target.chatID, runID, err)
	}
	return nil
}
```

(If the other session changed either function since `6e6c660`, keep its changes and add only the `renderEngineNotes` / `offerContinue` blocks.)

- [ ] **Step 6: Run the tests**

Run: `go test ./clients/telegram/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add clients/telegram/run_notes.go clients/telegram/run_notes_test.go clients/telegram/worker_types.go clients/telegram/delivery.go
git commit -m "$(cat <<'EOF'
feat(telegram): show engine notes and offer Continue after an early stop

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 15: iOS models — stop reason, continuation, trigger, origin

**Files:**
- Modify: `clients/ios/Sources/MatrixclawClient/Models.swift` (`Run`, `Message`)
- Test: `clients/ios/Tests/MatrixclawClientTests/ModelDecodingTests.swift`

New fields are plain optional strings, so values added later never break decoding.

- [ ] **Step 1: Write the failing test** — add to `ModelDecodingTests`:

```swift
    func testRunStopReasonAndEngineOriginDecode() throws {
        let run = try decode(Run.self, #"{"id":"r2","session_id":"s1","user_message_id":"m2","status":"completed","stop_reason":"budget_exhausted","continues_run_id":"r1","trigger":"automation","started_at":"2026-09-24T10:00:00Z","updated_at":"2026-09-24T10:05:00Z"}"#)
        XCTAssertEqual(run.stopReason, "budget_exhausted")
        XCTAssertEqual(run.continuesRunId, "r1")
        XCTAssertEqual(run.trigger, "automation")
        let note = try decode(Message.self, #"{"id":"n1","session_id":"s1","run_id":"r2","role":"system","origin":"engine","content":"Budget note","created_at":"2026-09-24T10:00:00Z","updated_at":"2026-09-24T10:00:00Z"}"#)
        XCTAssertEqual(note.origin, "engine")
    }
```

- [ ] **Step 2: Add the fields** — in `Models.swift` replace `Run` and `Message`:

```swift
public struct Run: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var sessionId: String
    public var userMessageId: String
    public var client: String?
    public var externalKey: String?
    public var trigger: String?
    public var continuesRunId: String?
    public var status: RunStatus
    public var stopReason: String?
    public var error: String?
    public var startedAt: Date
    public var finishedAt: Date?
    public var updatedAt: Date
}
```

```swift
public struct Message: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var seq: Int64?
    public var sessionId: String
    public var runId: String
    public var role: MessageRole
    public var origin: String?
    public var content: String
    public var parts: [MessagePart]?
    public var model: String?
    public var provider: String?
    public var createdAt: Date
    public var updatedAt: Date
}
```

- [ ] **Step 3: Run the Swift tests where a toolchain exists**

Run: `cd clients/ios && swift test` (this host has no Swift toolchain; if `swift` is missing, skip and say so in the report).
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add clients/ios/Sources/MatrixclawClient/Models.swift clients/ios/Tests/MatrixclawClientTests/ModelDecodingTests.swift
git commit -m "$(cat <<'EOF'
feat(ios): decode run stop reason, continuation, trigger and message origin

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 16: Stage verification

- [ ] **Step 1: Static checks and the whole suite**

```bash
gofmt -l ./internal ./clients ./cmd
go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/ ./clients/telegram/
```

Expected: `gofmt -l` prints nothing; everything else PASS.

- [ ] **Step 2: Leftovers** — each of these must print nothing:

```bash
grep -rn "maxSteps\|tool loop exceeded\|StopReasonError\|finishToolTurn\|fewest tool calls" --include=*.go internal clients
```

- [ ] **Step 3: Manual check (owner, on the test stand — never against the production daemon on this host)**
- TUI: `/budget steps 3`, give a multi-step task: a "Budget note" appears once, the run ends with a short summary and the hint "The run stopped at its budget. Send /continue to go on."; `/continue` starts a new run that picks up the work; `/budget reset` restores the defaults.
- Telegram: the same task shows "Note: …" rows silently and a **Continue** button under the stop notice; pressing it edits the button message to "Continuing the last run." and delivers the continued run.
- API: `GET /v1/runs/<id>` shows `"stop_reason":"budget_exhausted"`, the continued run shows `"continues_run_id"`.

---

## Self-review

**Spec coverage (§2 Loop, Stages row 2b, §6):**
- Budget steps / active wall-clock (parks excluded) / tokens from normalised usage → Task 5 (`budget.go`, `recordStep`, `active()`), with per-trigger daemon defaults (Task 5 `DefaultRunBudgets`, Task 10 config) and per-session overrides (Task 9). Default step limit stays 32 (Decision 3).
- 80% wrap-up engine message → Task 5 `prepareStep` / `remaining`.
- Final turn with `tool_choice: none`, tools still defined → Task 5 `buildRequest(final)` and test `TestStepBudgetEndsWithAToolLessFinalTurn`.
- `completed` + `stop_reason` (`done | budget_exhausted | loop_detected`) stored on runs and exposed via API / iOS → Tasks 4, 5, 15 (`core.Run` is the API payload).
- Loop guard 3 → warning, 5 → final turn → Task 6.
- Completion check → deferred to Stage 5 with justification (Decision 2); `continues_run_id` stored for it (Tasks 4, 11).
- Engine messages with `origin: engine`, rendered as system notes in TUI and Telegram → Tasks 1, 2, 5 (`notes.go`), 13, 14.
- `/continue` (controlplane → TUI & Telegram), user message "Continue", `continues_run_id`, fresh budget → Task 11 (fresh budget: a new run has no checkpoint); Telegram button by `stop_reason` → Task 14.
- `/budget` view/set → Tasks 9 and 12.
- `max_tokens`: tool calls run, text is continued up to 3 times then `failed`, empty/truncated-call replies raise `MaxOutputTokens` once ×2 capped by the model, Codex has no limiter → Tasks 3 and 7; `refusal` / `content_filter` complete → Task 7.
- "Minimise tool calls" removed → Task 8.
- Counters survive approval parks and crash recovery → Task 5 (`saveEngineCheckpoint` / `updateRunCheckpoint` / `resumeCounters`, tests `TestBudgetCountersSurviveAnApprovalPark`, `TestRecoveredRunKeepsItsBudgetCounters`); loop-guard and output-limit state live in the same `Counters` (Tasks 6, 7, `TestLoopStreakIsCheckpointed`).
- Explicitly later stages: auto-wake chain limit (6b), child usage reported to parent (6c), context message (3).

**Placeholder scan:** every code step carries complete code or an exact one-line insertion with its anchor; no "TBD", "similar to", or undefined helpers. Helpers used across tasks are defined in earlier tasks: `runTask`, `counterTool`, `toolSteps`, `engineNotes`, `lastMessage` (Task 5, `budget_test.go`); `changingTool` (Task 5, `run_budget_test.go`); `statusTool` (Task 6); existing helpers `run`, `text`, `calls`, `call`, `readTool`, `phases`, `hasFinish`, `newCrashRecoveryCore`, `saveCrashRecoveryRun`, `approvalTools`, `funcTool`, `recoveryLLMs`, `recoveryRuntime`, `recordingRunStarter`, `sessionMessages`, `hasAssistantContent`, `assertRecoveryRunStatus`, `waitForRecoveryRunStatus`, `tokenReportRuntime`, `runRenderBotAPI`, `newTestStore`, `createTestSession`, `createTestRun`, `saveTestMessage`, `newAPITestServer`.

**Type consistency:** `agent.StopReason` / `StopDone` / `StopBudgetExhausted` / `StopLoopDetected` / `Continuable` (Task 4) are used unchanged in Tasks 5–7, 13, 14. `agent.Budget{Steps int, ActiveTime time.Duration, Tokens int64}` and `agent.Counters` fields (`Steps`, `Tokens`, `Active`, `WrapUpSent`, then `LoopHash`, `LoopTool`, `LoopRepeats`, `LoopWarned`, then `Continuations`, `OutputLimit`) match every test. `buildRequest(ctx, final StopReason)` is called with `final` at all three sites. `finishToolTurn` is renamed `finishTurn` in Task 7 and its only caller is updated there. `createAcceptedRun` gains `continuesRunID` in Task 11 and both existing callers get `""`. `ContinueSession` has the signature `(ctx, externalKey, sessionID)` in `controlplane.ContinueRuntime` and `clientruntime`, and `(ctx, sessionID, workingDir)` in `daemonclient`. `SessionBudget` / `SessionBudgetReport` / `SessionBudgetResponse` are defined in Task 9 before Task 12 uses them. `RunCheckpoint.EngineState` is `json.RawMessage` in core and stored as TEXT.
