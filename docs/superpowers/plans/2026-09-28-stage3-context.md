# Stage 3 — Context Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A native run survives hundreds of steps without overflowing its model window: history is cut at a seq-based boundary with a cache-friendly summary, old bulky tool results are elided, huge outputs live in files, token accounting follows the provider's reported usage, the system prompt stays byte-stable for the whole run with changing state journaled as context messages, prompt caching is marked for Anthropic and OpenRouter-Claude, and the default budgets rise to 300 / 100 / 50 steps.

**Architecture:** The boundary becomes a structured `transcript.Compaction` on a system message (`messages.compaction_json`); `Journal.Load` returns the newest boundary plus the messages with `seq > covers_through_seq`, and the engine's in-memory history exposes the same window. `internal/agent/context` (package `agentcontext`) owns the pure parts: script-aware estimates, effective window and thresholds, tail cut, kept texts, elision, chunked summaries, head/tail truncation. `internal/agent` owns the loop parts: usage anchor, `fitRequest` (elide at 60%, summarise at 80%, context exhaustion), prefix-reusing summaries, overflow recovery, the context message. Core serves the ports: boundary load, `/clear` and `/compact`, large-output files, the stable prompt and the context text.

**Tech Stack:** Go 1.26, SQLite (modernc, `ensureColumn` migrations), bubbletea TUI, Anthropic Messages API, OpenAI-compatible chat completions (OpenRouter).

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session may commit in parallel: **locate code by function name, not line number**, run `git status --short` before each commit and stage only the paths listed in the task with explicit `git add <paths>` (never `-A`, `-u` or directories).
- Run `gofmt -w` on every Go file you touch (code blocks below are not guaranteed to be column-aligned). Every commit must pass `go build ./... && go vet ./... && go test ./...` — run the full suite before committing.
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger. When a pre-existing test fails only because an expectation this plan changes on purpose (for example an extra `engine_model` context note in a message count), update that expectation and name it in your report.

## Decisions taken in this plan (owner should know)

1. **The boundary is a message field, not a message part.** `transcript.Message.Compaction *Compaction` (`{summary, kept, covers_through_seq, run_id, tokens_before, tokens_after, cleared}`), stored in a new column `messages.compaction_json`. The shipped iOS package decodes `MessagePartKind` strictly, so a new part kind would break every session list with a boundary; an unknown top-level key is ignored. The boundary message stays `role: system`, `run_id: ""`, with a readable label as content (`Context compacted: ~120k -> ~31k tokens.` / `Context cleared.`); nobody parses that text.
2. **Old text markers are migrated once, at store open.** System messages starting with `🧠 Context compacted` / `🧹 Context cleared` get a `compaction_json` with `covers_through_seq = own seq − 1` (the old marker covered everything written before it), stats and summary parsed from the old text. That migration is the only place left that knows the emoji prefixes; the engine, core, controlplane and TUI read the structured field only. The old "keep every message of the current run" exception is gone.
3. **`/clear` has its own endpoint** `POST /v1/sessions/{id}/clear` → `core.ClearContext`, writing a boundary with `cleared: true` that covers the session's newest seq. Clients no longer post a magic system-message text.
4. **Tail and kept texts.** A summary covers everything before the tail; the tail is the newest whole turns within 25% of the effective window (12.5% after a provider context-length error), at least the newest turn, never splitting a model reply from its calls or a call from its result. The current run's user messages and every steer are kept verbatim in `Compaction.Kept`; steers are recorded structurally in a new `ToolResultPart.Guidance` (they are still appended to the result text as before). Kept texts carry over to the next boundary only within the same run. The summary and kept texts reach the model as the **first user message** of the request, never in the system prompt.
5. **Token accounting.** The authority is the last main response's `PromptTokens`, anchored at the highest seq its request contained, plus an estimate of the messages written after it. The anchor is dropped (estimate authoritative) after an elision change, a summary, or at run start. Estimate: runes/4 below U+0250 (Latin), runes/2.5 above (Cyrillic etc.), 1500 per image. Effective window = window − output limit − 5% of window, never below half the window; unknown window → 128k. The provider fallback window drops from 256k to 128k; the 80k compaction floor is deleted.
6. **Thresholds.** Elision at 60% of the effective window, summary at 80% (after elision). The elision watermark (`Counters.ElidedResults`, `Counters.ElidedImages`, both seqs) moves only when the 60% threshold is reached, so the request prefix changes rarely; a tool round is an assistant reply with finish reason `tool_calls`; results larger than ~1k tokens older than the last 5 rounds and images older than the last 3 replies are elided in the request only.
7. **Summaries.** When the step's request fits the effective window, the summary request **is** that request plus a trailing user instruction with `tool_choice: none` (same system prompt, tools and messages → cache hit). Otherwise — request over the window, a provider context-length error, the summary request itself overflowing, or a configured `compact_model` — the covered history is summarised on its own in chunks of half the effective window and the partial summaries are merged. Summaries are recorded as `compact` run steps, count toward the token budget, not toward steps.
8. **Low-yield backoff and exhaustion.** `Counters.LowYield` counts consecutive summaries whose `tokens_after` is more than 90% of `tokens_before`. At 2, a step still at ≥80% runs a context-exhausted final turn (`tool_choice: none`, engine note) and the run ends `failed` with `stop_reason = context_exhausted` (not continuable). A context-length error summarises with half the tail and retries once; a second overflow, or nothing left to summarise, also ends `failed` / `context_exhausted`.
9. **Large tool outputs.** A result over 8k estimated tokens is written to `<dir of the DB>/sessions/<session id>/tool-output/<16 hex of sha256>.txt` (dir 0700, file 0600); the model gets `Output is ~Nk tokens; the full output is in <path> …` plus head and tail. Naming by content hash keeps the loop guard working for repeated identical outputs. The spill sits in core's `executeToolWithGrant`, so engine runs, run-less `ExecuteTool` (API, voice, MCP server) and recovery replays all get it. `DeleteSession` removes the session's directory. The provider view keeps a head/tail guard at the same 8k tokens for rows written before this stage; the 12k-rune truncation and the 5k browser-snapshot special case are deleted. `ToolResultPart.OutputPath` records the file so elision notes can point at it.
10. **Stable prompt.** Computed once per `Engine.Run`: identity, rules and tool guidance, project root, web research and subagent guidance, memory as of run start, skills as of run start; tool definitions are also computed once per run. Everything that changes — recovery notice, runtime/module status, memory changed during the run, the session plan (until Stage 5) — is core's `Prompts.Context` text, journaled as an `engine_model` message whenever its hash differs from `Counters.ContextHash`, and re-sent after every summary. The hash is checkpointed, so a restart does not repeat it.
11. **Caching.** Anthropic: `cache_control` on the last tool, the system block, and the last block of the two newest user turns (4 breakpoints). OpenRouter with a Claude model (`anthropic/…` or `…claude…` on `openrouter.ai`): the system message and the two newest user/tool messages become content-part arrays with `cache_control` on their last text part. Whether OpenRouter honours breakpoints on `tool` messages must be checked on the stand (cache read in `/usage`). `prompt_cache_key` stays as it is.
12. **`compact_model`** is a daemon setting `daemon.compact_model: {provider, model}`. It writes in-run summaries (always chunked, since it cannot reuse the run's cache) and manual `/compact`; if it cannot be resolved the run's model is used and a log line says so.
13. **Cache hit** is shown in `/usage` as `Cache hit: 75%` (cache read ÷ prompt). `/status` stays the server-status command.
14. **Budgets** rise to 300 steps / 4 h (user), 100 / 1 h (subagent), 50 / 30 min (automation) in the last functional task, after in-run compaction, elision and the long-run regression tests are in.
15. The Stage 2a note about `transcript.RunReply` joining cut replies with a space is already solved by `e5c9a4f`; nothing to do.

Out of scope (later stages / deferred): todo re-sent by the context message (Stage 5 adds todo to `Prompts.Context`), deferred MCP tool loading, fallback model, cost budgets, file checkpoints.

## File structure

Created:
- `internal/agent/context/boundary.go` — `TailPercent`, `UnknownWindowTokens`, `EffectiveWindow`, `TailStart`, `Kept`, `SummaryText`, `SummaryMessages`, `BoundaryLabel`.
- `internal/agent/context/summarize.go` — chunked `Summarize`, summary prompt, `SummaryInstruction`, `SummaryReply` (replaces `compact.go`).
- `internal/agent/context/elide.go` — `Elision`, `NextElision`, `Elide`.
- `internal/agent/context/{estimate,boundary,summarize,elide}_test.go`.
- `internal/agent/compact.go` — `compactHistory`, `writeBoundary`, summaries, low-yield counter.
- `internal/agent/fit.go` — usage anchor, `contextLimit`, `promptTokens`, `fitRequest`, elision state.
- `internal/agent/context_note.go` — `syncContext`.
- `internal/agent/{compact,fit,context_note,long_run}_test.go`.
- `internal/store/schema_compaction.go` (+ `schema_compaction_test.go`).
- `internal/core/context_boundary.go` — `ClearContext`, `appendBoundary`.
- `internal/core/tool_output.go` — `WithSessionFiles`, large-output files.
- `internal/core/compact_model.go` — `WithCompactModel`, `compactRuntime`.
- `internal/core/{context_boundary,tool_output,context_note,compact_model,long_run}_test.go`.
- `internal/api/context_test.go`, `internal/controlplane/context_test.go`, `internal/providers/context_window_test.go`.
- `clients/terminal/ui/surface/chat/compact_summary_test.go`, `clients/terminal/chat/runtime/app_usage_test.go`.

Deleted: `internal/agent/context/markers.go`, `internal/agent/context/compact.go`.

Modified: `internal/transcript/message.go`, `internal/tools/types.go`, `internal/providers/{context_window,openai_chat_options}.go`, `internal/providers/ai/anthropiccompat/{encode,wire}.go`, `internal/providers/ai/openaicompat/{chat,types,config}.go`, `internal/agent/{ports,task,engine,journal,request,generate,budget,messages,tools}.go`, `internal/agent/agenttest/agenttest.go`, `internal/agent/context/{estimate,conversation}.go`, `internal/store/{schema,sqlite_messages}.go`, `internal/core/{ports,agent_journal,agent_prompts,run_execute,run_outcome,run_budget,session_context,system_messages,sessions,tool_call_run,plan_tools}.go`, `internal/api/{context,sessions}.go`, `internal/daemonclient/sessions.go`, `internal/clientruntime/controlplane_runtime.go`, `internal/controlplane/{context,dispatcher,usage}.go`, `internal/setup/types.go`, `internal/daemoncmd/{bootstrap,run,run_helpers}.go`, TUI `surface/message/message.go`, `surface/chat/compact_summary.go`, `viewmodel/surface_adapter.go`, `runtime/app_usage.go`, and the tests named per task.

---

### Task 1: Script-aware token estimate and a 128k fallback window

**Files:**
- Modify: `internal/agent/context/estimate.go` (`EstimateTextTokens`)
- Modify: `internal/providers/context_window.go` (`DefaultFallbackContextWindowTokens`)
- Modify: `clients/terminal/chat/runtime/app_usage.go` (drop the local `estimateTokens`)
- Create: `internal/agent/context/estimate_test.go`, `internal/providers/context_window_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/agent/context/estimate_test.go`:

```go
package agentcontext

import (
	"strings"
	"testing"
)

func TestEstimateCountsOtherScriptsDenserThanLatin(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{strings.Repeat("a", 400), 100},
		{strings.Repeat("я", 400), 160},
		{"abяя", 2},
		{"é", 1},
	} {
		if got := EstimateTextTokens(tc.text); got != tc.want {
			t.Errorf("EstimateTextTokens(%d runes of %q...) = %d, want %d", len([]rune(tc.text)), firstRune(tc.text), got, tc.want)
		}
	}
}

func firstRune(text string) string {
	for _, r := range text {
		return string(r)
	}
	return ""
}
```

`internal/providers/context_window_test.go`:

```go
package providers

import "testing"

func TestUnknownModelsGetA128kWindow(t *testing.T) {
	if got := ResolveContextWindowTokens("custom-gateway", "", "mystery-model-x"); got != 128_000 {
		t.Fatalf("window of an unknown model = %d, want 128000", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/context/ -run TestEstimateCountsOtherScriptsDenserThanLatin; go test ./internal/providers/ -run TestUnknownModelsGetA128kWindow`
Expected: FAIL — Cyrillic estimates 100 instead of 160; the window is 256000.

- [ ] **Step 3: Implement** — in `internal/agent/context/estimate.go` replace `EstimateTextTokens` with the version below, add the constant after it and drop the now unused `unicode/utf8` import:

```go
// EstimateTextTokens estimates runes/4 for Latin script and runes/2.5 for other
// scripts such as Cyrillic, which tokenizers split more finely.
func EstimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	latin, other := 0, 0
	for _, r := range text {
		if r < latinScriptEnd {
			latin++
		} else {
			other++
		}
	}
	return max(1, (latin*10+other*16+39)/40)
}

// latinScriptEnd is the first rune after Latin Extended-B.
const latinScriptEnd = 0x0250
```

In `internal/providers/context_window.go` replace the constant line with:

```go
// DefaultFallbackContextWindowTokens is assumed for a model whose window is unknown.
const DefaultFallbackContextWindowTokens = 128_000
```

In `clients/terminal/chat/runtime/app_usage.go` delete the function `estimateTokens` (and the `unicode/utf8` import it used) and replace each of its five calls `estimateTokens(x)` with `agentcontext.EstimateTextTokens(x)`, so the TUI header estimates like the engine.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/agent/context/ ./internal/providers/ ./clients/terminal/...`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/context/estimate.go internal/agent/context/estimate_test.go internal/providers/context_window.go internal/providers/context_window_test.go clients/terminal/chat/runtime/app_usage.go
git commit -m "feat(agent): estimate tokens by script and assume 128k for unknown windows

Latin text counts runes/4, other scripts such as Cyrillic runes/2.5, so
Russian sessions are no longer underestimated by a third. Models with an
unknown window are assumed to have 128k instead of 256k, and the TUI header
uses the same estimate.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Steer guidance recorded on its tool result

**Files:**
- Modify: `internal/transcript/message.go` (`ToolResultPart`)
- Modify: `internal/agent/tools.go` (`appendUserGuidanceToToolResult`)
- Test: `internal/agent/engine_test.go` (`TestSteerIsAppendedToTheNextToolResult`)

- [ ] **Step 1: Write the failing test** — replace `TestSteerIsAppendedToTheNextToolResult` in `internal/agent/engine_test.go` with:

```go
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
	if guidance := result.Parts[0].ToolResult.Guidance; len(guidance) != 1 || guidance[0] != "check the logs" {
		t.Fatalf("recorded guidance = %q", guidance)
	}
	if got := toolContent(model.Requests()[1], "r1"); !strings.Contains(got, "User guidance: check the logs") {
		t.Fatalf("request tool content = %q", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/agent/ -run TestSteerIsAppendedToTheNextToolResult`
Expected: build failure `ToolResult.Guidance undefined`.

- [ ] **Step 3: Implement** — in `internal/transcript/message.go` replace `ToolResultPart` with:

```go
type ToolResultPart struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Content    string          `json:"content"`
	MIMEType   string          `json:"mime_type,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Status     string          `json:"status,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	// Guidance is user steering delivered with this result; Content carries it too.
	Guidance []string `json:"guidance,omitempty"`
}
```

In `internal/agent/tools.go` replace `appendUserGuidanceToToolResult` with:

```go
func appendUserGuidanceToToolResult(message *transcript.Message, text string) {
	text = strings.TrimSpace(text)
	if message == nil || text == "" {
		return
	}
	for i := range message.Parts {
		result := message.Parts[i].ToolResult
		if result == nil {
			continue
		}
		result.Content = appendGuidanceBlock(result.Content, "User guidance: "+text)
		result.Guidance = append(result.Guidance, text)
		message.Content = normalizeToolContent(result.Content)
		return
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/agent/... ./internal/transcript/...`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/transcript/message.go internal/agent/tools.go internal/agent/engine_test.go
git commit -m "feat(agent): record steer guidance on the tool result that carries it

The guidance a user steered into a run is kept as a structured list on the
tool result besides the text appended to it, so a later summary can keep
it verbatim.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `Compaction` on messages, stored and migrated

**Files:**
- Modify: `internal/transcript/message.go` (`Message`, new `Compaction`)
- Create: `internal/store/schema_compaction.go`, `internal/store/schema_compaction_test.go`
- Modify: `internal/store/schema.go` (`applyCanonicalSchema`)
- Modify: `internal/store/sqlite_messages.go` (`messageColumns`, `insertMessage`, `scanMessage`, new `LatestCompaction`)
- Test: `internal/store/sqlite_messages_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/sqlite_messages_test.go` (add `"reflect"` to its imports):

```go
func TestLatestCompactionIsTheNewestBoundaryOfTheSession(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	if _, err := st.LatestCompaction(ctx, "s1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("LatestCompaction without a boundary: err = %v, want ErrNotFound", err)
	}
	system := transcript.MessageRoleSystem
	saveTestMessage(t, st, transcript.Message{ID: "m1", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "b1", SessionID: "s1", Role: system, Content: "Context compacted.", Compaction: &transcript.Compaction{Summary: "first", CoversThroughSeq: 1}, CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "m2", SessionID: "s1", CreatedAt: testEpoch})
	want := transcript.Compaction{Summary: "second", Kept: []string{"User: task"}, CoversThroughSeq: 3, RunID: "r1", TokensBefore: 900, TokensAfter: 300}
	saveTestMessage(t, st, transcript.Message{ID: "b2", SessionID: "s1", Role: system, Content: "Context compacted.", Compaction: &want, CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "other", SessionID: "s2", Role: system, Content: "Context cleared.", Compaction: &transcript.Compaction{Cleared: true}, CreatedAt: testEpoch})

	latest, err := st.LatestCompaction(ctx, "s1")
	if err != nil || latest.ID != "b2" || latest.Compaction == nil || !reflect.DeepEqual(*latest.Compaction, want) {
		t.Fatalf("latest = %+v err = %v", latest, err)
	}
	plain, err := st.GetMessage(ctx, "m2")
	if err != nil || plain.Compaction != nil {
		t.Fatalf("plain message = %+v err = %v", plain, err)
	}
}
```

`internal/store/schema_compaction_test.go`:

```go
package store_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestLegacyContextMarkersBecomeBoundaries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	system := transcript.MessageRoleSystem
	saveTestMessage(t, st, transcript.Message{ID: "old", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "compact", SessionID: "s1", Role: system, Content: "🧠 Context compacted: ~12k -> ~3.4k tokens\n\nEarlier work summarised.", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "clear", SessionID: "s1", Role: system, Content: "🧹 Context cleared\n\nContext cleared by user.", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "notice", SessionID: "s1", Role: system, Content: "Model changed.", CreatedAt: testEpoch})
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	compact, err := reopened.GetMessage(ctx, "compact")
	want := transcript.Compaction{Summary: "Earlier work summarised.", CoversThroughSeq: compact.Seq - 1, TokensBefore: 12_000, TokensAfter: 3_400}
	if err != nil || compact.Compaction == nil || !reflect.DeepEqual(*compact.Compaction, want) {
		t.Fatalf("migrated compaction = %+v err = %v", compact.Compaction, err)
	}
	latest, err := reopened.LatestCompaction(ctx, "s1")
	if err != nil || latest.ID != "clear" || !latest.Compaction.Cleared || latest.Compaction.CoversThroughSeq != latest.Seq-1 || latest.Compaction.Summary != "" {
		t.Fatalf("latest boundary = %+v err = %v", latest, err)
	}
	if notice, err := reopened.GetMessage(ctx, "notice"); err != nil || notice.Compaction != nil {
		t.Fatalf("plain notice = %+v err = %v", notice, err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/store/ -run 'TestLatestCompaction|TestLegacyContextMarkers'`
Expected: build failure `unknown field Compaction` / `st.LatestCompaction undefined`.

- [ ] **Step 3: Add the type** — in `internal/transcript/message.go` replace the `Message` struct and add `Compaction` after it:

```go
type Message struct {
	ID         string        `json:"id"`
	Seq        int64         `json:"seq,omitempty"`
	SessionID  string        `json:"session_id"`
	RunID      string        `json:"run_id"`
	Role       MessageRole   `json:"role"`
	Origin     Origin        `json:"origin,omitempty"`
	Content    string        `json:"content"`
	Parts      []MessagePart `json:"parts,omitempty"`
	Compaction *Compaction   `json:"compaction,omitempty"`
	Model      string        `json:"model,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// Compaction makes a message a context boundary: the model sees Summary and Kept
// instead of the session's messages up to CoversThroughSeq. Cleared marks a
// /clear, which keeps nothing. Tokens are estimates of the prompt around it.
type Compaction struct {
	Summary          string   `json:"summary,omitempty"`
	Kept             []string `json:"kept,omitempty"`
	CoversThroughSeq int64    `json:"covers_through_seq"`
	RunID            string   `json:"run_id,omitempty"`
	TokensBefore     int      `json:"tokens_before,omitempty"`
	TokensAfter      int      `json:"tokens_after,omitempty"`
	Cleared          bool     `json:"cleared,omitempty"`
}
```

- [ ] **Step 4: Column and migration** — create `internal/store/schema_compaction.go`:

```go
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// Text prefixes older versions gave the system messages that marked a
// compaction or a /clear.
const (
	legacyCompactPrefix = "🧠 Context compacted"
	legacyClearPrefix   = "🧹 Context cleared"
)

var legacyCompactStats = regexp.MustCompile(`~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s*->\s*~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s+tokens`)

// migrateCompactionMarkers adds messages.compaction_json and turns the text
// markers older versions wrote into boundaries; such a marker covered every
// message of its session written before it.
func migrateCompactionMarkers(db *sql.DB) error {
	if err := ensureColumn(db, "messages", "compaction_json", `ALTER TABLE messages ADD COLUMN compaction_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT id, seq, content FROM messages WHERE role = 'system' AND compaction_json = '' AND (content LIKE ? OR content LIKE ?)`, legacyCompactPrefix+"%", legacyClearPrefix+"%")
	if err != nil {
		return fmt.Errorf("store: list legacy context markers: %w", err)
	}
	updates := map[string]string{}
	for rows.Next() {
		var id, content string
		var seq int64
		if err := rows.Scan(&id, &seq, &content); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan legacy context marker: %w", err)
		}
		if compaction, ok := legacyCompaction(seq, content); ok {
			updates[id] = marshalCompaction(&compaction)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("store: iterate legacy context markers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("store: close legacy context markers: %w", err)
	}
	for id, body := range updates {
		if _, err := db.Exec(`UPDATE messages SET compaction_json = ? WHERE id = ?`, body, id); err != nil {
			return fmt.Errorf("store: migrate legacy context marker: %w", err)
		}
	}
	return nil
}

func legacyCompaction(seq int64, content string) (transcript.Compaction, bool) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, legacyClearPrefix) {
		return transcript.Compaction{CoversThroughSeq: seq - 1, Cleared: true}, true
	}
	rest, ok := strings.CutPrefix(content, legacyCompactPrefix)
	if !ok {
		return transcript.Compaction{}, false
	}
	compaction := transcript.Compaction{CoversThroughSeq: seq - 1}
	rest = strings.TrimSpace(rest)
	if header, summary, found := strings.Cut(rest, "\n\n"); found && strings.HasPrefix(header, ":") {
		if match := legacyCompactStats.FindStringSubmatch(header); match != nil {
			compaction.TokensBefore = legacyTokenCount(match[1])
			compaction.TokensAfter = legacyTokenCount(match[2])
		}
		rest = summary
	}
	compaction.Summary = strings.TrimSpace(rest)
	return compaction, true
}

func legacyTokenCount(value string) int {
	value = strings.ToLower(strings.TrimSpace(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "k"):
		multiplier, value = 1_000, strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "m"):
		multiplier, value = 1_000_000, strings.TrimSuffix(value, "m")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return int(math.Round(parsed * multiplier))
}
```

In `internal/store/schema.go`, `applyCanonicalSchema`, directly after the `ensureColumn(db, "messages", "origin", …)` block insert:

```go
	if err := migrateCompactionMarkers(db); err != nil {
		return err
	}
```

(It must run before `migrateMessageSearch`, which scans rows with `messageColumns`.)

- [ ] **Step 5: Read and write the column** — in `internal/store/sqlite_messages.go`:

Replace `messageColumns`:

```go
const messageColumns = `id, session_id, run_id, role, origin, content, parts_json, model, provider, created_at, updated_at, seq, compaction_json`
```

Replace `insertMessage` and `scanMessage`:

```go
// insertMessage assigns the next database-wide seq in the same statement and returns it.
func insertMessage(ctx context.Context, queryer sqlRowQueryer, message transcript.Message) (int64, error) {
	var seq int64
	err := queryer.QueryRowContext(ctx, `
INSERT INTO messages(id, session_id, run_id, role, origin, content, parts_json, model, provider, created_at, updated_at, compaction_json, seq)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages))
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
		marshalCompaction(message.Compaction),
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
	var compactionJSON string
	if err := scanner.Scan(&message.ID, &message.SessionID, &message.RunID, &role, &origin, &message.Content, &partsJSON, &model, &provider, &createdAt, &updatedAt, &message.Seq, &compactionJSON); err != nil {
		return transcript.Message{}, fmt.Errorf("store: scan message: %w", err)
	}
	message.Role = transcript.MessageRole(role)
	message.Origin = transcript.Origin(origin)
	message.Parts = unmarshalMessageParts(partsJSON)
	message.Compaction = unmarshalCompaction(compactionJSON)
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

Add next to `marshalMessageParts`:

```go
func marshalCompaction(compaction *transcript.Compaction) string {
	if compaction == nil {
		return ""
	}
	body, err := json.Marshal(compaction)
	if err != nil {
		return ""
	}
	return string(body)
}

func unmarshalCompaction(raw string) *transcript.Compaction {
	if raw == "" {
		return nil
	}
	var compaction transcript.Compaction
	if err := json.Unmarshal([]byte(raw), &compaction); err != nil {
		return nil
	}
	return &compaction
}
```

Add after `ListMessagesAfter`:

```go
// LatestCompaction returns the session's newest context boundary.
func (s *SQLiteStore) LatestCompaction(ctx context.Context, sessionID string) (transcript.Message, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+messageColumns+`
FROM messages
WHERE session_id = ? AND compaction_json <> ''
ORDER BY seq DESC
LIMIT 1`, sessionID)
	message, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return transcript.Message{}, core.ErrNotFound
	}
	return message, err
}
```

`updateMessageRow` stays as is: a boundary is never rewritten.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/store/ ./internal/transcript/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/transcript/message.go internal/store/schema.go internal/store/schema_compaction.go internal/store/schema_compaction_test.go internal/store/sqlite_messages.go internal/store/sqlite_messages_test.go
git commit -m "feat(store): keep context boundaries as structured compactions

A message can carry a compaction: the summary and kept texts that replace
the session's messages up to a seq, with the estimated tokens around it.
It is stored in messages.compaction_json; the store finds a session's
newest boundary. The text markers older versions wrote are migrated into
boundaries once, when the store opens.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `/clear` writes a structured boundary

The engine still reads text markers until Task 5, so the clear boundary keeps the legacy text as its content for one task; Task 5 replaces it with a plain label.

**Files:**
- Create: `internal/core/context_boundary.go`, `internal/core/context_boundary_test.go`
- Modify: `internal/api/context.go`, `internal/api/sessions.go` (`handleSessionByID` routes)
- Modify: `internal/daemonclient/sessions.go`, `internal/clientruntime/controlplane_runtime.go`
- Modify: `internal/controlplane/dispatcher.go` (`ContextRuntime`), `internal/controlplane/context.go` (`handleContext`)
- Create: `internal/api/context_test.go`, `internal/controlplane/context_test.go`
- Modify: `internal/core/context_guard_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/core/context_boundary_test.go`:

```go
package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestClearContextCoversEveryMessageSoFar(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	session, _ := saveCrashRecoveryRun(t, db, "clear", core.RunStatusCompleted, false)
	messages := sessionMessages(t, db, session.ID)

	cleared, err := app.ClearContext(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if c := cleared.Compaction; c == nil || !c.Cleared || c.CoversThroughSeq != messages[len(messages)-1].Seq || cleared.RunID != "" {
		t.Fatalf("clear boundary = %+v", cleared)
	}
	latest, err := db.LatestCompaction(context.Background(), session.ID)
	if err != nil || latest.ID != cleared.ID || latest.Seq <= cleared.Compaction.CoversThroughSeq {
		t.Fatalf("stored boundary = %+v err = %v", latest, err)
	}
}
```

`internal/api/context_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestClearEndpointWritesABoundary(t *testing.T) {
	server, st := newAPITestServer(t)
	if err := st.SaveMessage(context.Background(), transcript.Message{ID: "m1", SessionID: "s1", Role: transcript.MessageRoleUser, Content: "hi", CreatedAt: apiTestEpoch}); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/sessions/s1/clear", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response core.MessageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if c := response.Message.Compaction; c == nil || !c.Cleared || c.CoversThroughSeq != 1 {
		t.Fatalf("boundary = %+v", response.Message)
	}
}
```

`internal/controlplane/context_test.go`:

```go
package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (r tokenReportRuntime) ClearContext(context.Context, string) (transcript.Message, error) {
	return transcript.Message{ID: "boundary", Compaction: &transcript.Compaction{Cleared: true}}, nil
}

func TestContextClearConfirmClearsThroughTheContextRuntime(t *testing.T) {
	result, err := New(tokenReportRuntime{}, "").Handle(context.Background(), "key", "/context clear confirm")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Context cleared." || !result.ReloadSnapshot {
		t.Fatalf("result = %+v", result)
	}
}
```

In `internal/core/context_guard_test.go` replace the block

```go
	if _, err := app.CreateSystemMessage(ctx, session.ID, agentcontext.ClearedMarkerContent()); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("clear marker error = %v, want ErrRunActive", err)
	}
```

with

```go
	if _, err := app.ClearContext(ctx, session.ID); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("ClearContext error = %v, want ErrRunActive", err)
	}
```

and drop the `agentcontext` import there.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/core/ -run 'TestClearContext|TestContextMarkersAreRejected' ; go test ./internal/api/ -run TestClearEndpoint ; go test ./internal/controlplane/ -run TestContextClearConfirm`
Expected: build failure `app.ClearContext undefined`; the API returns 404; the controlplane answers "unsupported" (the runtime has no system-message method).

- [ ] **Step 3: Core** — create `internal/core/context_boundary.go`:

```go
package core

import (
	"context"
	"fmt"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ClearContext starts the session's model context afresh with a boundary that
// covers every message so far; the transcript keeps them.
func (c *Core) ClearContext(ctx context.Context, sessionID string) (transcript.Message, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return transcript.Message{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	}
	if executing, err := c.sessionRunExecuting(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	} else if executing {
		return transcript.Message{}, fmt.Errorf("%w: wait for the current run to finish before clearing context", ErrRunActive)
	}
	latest, err := c.store.ListMessages(ctx, sessionID, 1)
	if err != nil {
		return transcript.Message{}, err
	}
	compaction := transcript.Compaction{Cleared: true}
	if len(latest) > 0 {
		compaction.CoversThroughSeq = latest[0].Seq
	}
	return c.appendBoundary(ctx, sessionID, agentcontext.ClearedMarkerContent(), compaction)
}

// appendBoundary journals a context boundary of the session and announces it.
func (c *Core) appendBoundary(ctx context.Context, sessionID string, content string, compaction transcript.Compaction) (transcript.Message, error) {
	now := c.now().UTC()
	message := transcript.Message{
		ID:         c.newID("msg"),
		SessionID:  sessionID,
		Role:       transcript.MessageRoleSystem,
		Content:    content,
		Parts:      transcript.NormalizeMessageParts(content, nil),
		Compaction: &compaction,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	seq, err := c.store.AppendMessage(ctx, message)
	if err != nil {
		return transcript.Message{}, err
	}
	message.Seq = seq
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
	return message, nil
}
```

- [ ] **Step 4: API** — in `internal/api/sessions.go`, `handleSessionByID`, add to `childRoutes` right after the `/compact` entry:

```go
		{suffix: "/clear", handle: s.handleSessionClear},
```

Append to `internal/api/context.go`:

```go
func (s *Server) handleSessionClear(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	message, err := s.core.ClearContext(r.Context(), sessionID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.MessageResponse{Message: message})
}
```

- [ ] **Step 5: Client chain** — append to `internal/daemonclient/sessions.go` (after `CompactSession`):

```go
func (c *Client) ClearContext(ctx context.Context, sessionID string) (transcript.Message, error) {
	var response core.MessageResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/clear"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return transcript.Message{}, err
	}
	return response.Message, nil
}
```

Append to `internal/clientruntime/controlplane_runtime.go` (after `CompactSession`):

```go
func (r ControlplaneRuntime) ClearContext(ctx context.Context, sessionID string) (transcript.Message, error) {
	client, err := r.client("")
	if err != nil {
		return transcript.Message{}, err
	}
	return client.ClearContext(ctx, sessionID)
}
```

In `internal/controlplane/dispatcher.go` replace `ContextRuntime` with:

```go
type ContextRuntime interface {
	SessionContext(ctx context.Context, sessionID string) (core.ContextReport, error)
	CompactSession(ctx context.Context, sessionID string) (core.CompactSessionResult, error)
	ClearContext(ctx context.Context, sessionID string) (transcript.Message, error)
}
```

In `internal/controlplane/context.go`, `handleContext`, replace the whole `case "clear confirm":` branch with:

```go
	case "clear confirm":
		if _, err := d.contextRuntime.ClearContext(ctx, session.ID); err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Context cleared.", ReloadSnapshot: true}, nil
```

The `agentcontext` import of `context.go` stays (it still uses `FormatShortNumber`).

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/core/ ./internal/api/ ./internal/controlplane/ ./internal/daemonclient/ ./internal/clientruntime/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/context_boundary.go internal/core/context_boundary_test.go internal/core/context_guard_test.go internal/api/context.go internal/api/sessions.go internal/api/context_test.go internal/daemonclient/sessions.go internal/clientruntime/controlplane_runtime.go internal/controlplane/dispatcher.go internal/controlplane/context.go internal/controlplane/context_test.go
git commit -m "feat(core): /clear writes a structured context boundary

Clearing the context goes through POST /v1/sessions/{id}/clear and
core.ClearContext, which journals a boundary covering the session's newest
message instead of a magic system-message text.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Seq boundary in the engine and core; text markers gone

The engine and core switch to the structured boundary: `Journal.Load` returns the newest boundary plus the messages after it, a summary covers everything before a whole-turn tail and keeps the run's assignment and steers, and the summary reaches the model as the first message. The compaction thresholds stay the old ones (`Threshold`, `Recommendation`, `Prompts.Budget`) until Task 7.

**Files:**
- Create: `internal/agent/context/boundary.go`, `internal/agent/context/summarize.go`, `internal/agent/context/boundary_test.go`, `internal/agent/context/summarize_test.go`
- Delete: `internal/agent/context/markers.go`, `internal/agent/context/compact.go`
- Modify: `internal/agent/context/estimate.go` (`SessionTokens`)
- Modify: `internal/agent/ports.go` (`Window`, `Prompts`), `internal/agent/journal.go` (`history`), `internal/agent/budget.go` (`Counters`), `internal/agent/request.go` (`buildRequest`, `autoCompact`; `compactHistory` moves out), `internal/agent/engine.go` (`step`)
- Create: `internal/agent/compact.go`, `internal/agent/compact_test.go`
- Modify: `internal/agent/agenttest/agenttest.go` (`Journal.Load`, `Prompts`, new `Fixture.WithHistory`)
- Modify: `internal/agent/engine_test.go` (three compaction tests move to `compact_test.go`)
- Modify: `internal/core/ports.go` (`MessageStore`), `internal/core/agent_journal.go`, `internal/core/agent_prompts.go`, `internal/core/session_context.go`, `internal/core/system_messages.go`, `internal/core/context_boundary.go`, `internal/core/plan_tools.go`
- Test: `internal/core/native_run_characterization_test.go`

- [ ] **Step 1: Write the failing agentcontext tests**

`internal/agent/context/boundary_test.go`:

```go
package agentcontext

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func textMessage(seq int64, role transcript.MessageRole, runID, text string) transcript.Message {
	return transcript.Message{ID: fmt.Sprintf("m%d", seq), Seq: seq, RunID: runID, Role: role, Content: text, Parts: transcript.NormalizeMessageParts(text, nil)}
}

// toolStep is one model response that called read once: reply, call, result.
func toolStep(seq int64, callID string, output string) []transcript.Message {
	reply := transcript.Message{ID: fmt.Sprintf("m%d", seq), Seq: seq, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}}}}
	call := transcript.Message{ID: callID, Seq: seq + 1, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: callID, Name: "read", Input: "{}"}}}}
	result := transcript.Message{ID: callID + "_result", Seq: seq + 2, Role: transcript.MessageRoleTool, Content: output, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: callID, Name: "read", Content: output}}}}
	return []transcript.Message{reply, call, result}
}

func TestTailStartKeepsWholeTurnsWithinTheBudget(t *testing.T) {
	output := strings.Repeat("a", 4_000)
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task")}
	messages = append(messages, toolStep(2, "c1", output)...)
	messages = append(messages, toolStep(5, "c2", output)...)
	messages = append(messages, toolStep(8, "c3", output)...)

	if got := TailStart(messages, 2_100); got != 4 {
		t.Fatalf("TailStart = %d, want 4 (the last two tool steps)", got)
	}
	if got := TailStart(messages[:1], 2_100); got != 0 {
		t.Fatalf("TailStart of a single message = %d, want 0", got)
	}
}

func TestTailStartNeverSplitsACallFromItsResult(t *testing.T) {
	output := strings.Repeat("a", 4_000)
	reply := toolStep(2, "a", output)[0]
	callA, resultA := toolStep(2, "a", output)[1], toolStep(2, "a", output)[2]
	callB, resultB := toolStep(5, "b", output)[1], toolStep(5, "b", output)[2]
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task"), reply, callA, callB, resultA, resultB, textMessage(9, transcript.MessageRoleUser, "r1", "next")}

	if got := TailStart(messages, 1_500); got != 6 {
		t.Fatalf("TailStart = %d, want 6: no cut between parallel calls and their results", got)
	}
	if got := TailStart(messages, 1_000_000); got != 1 {
		t.Fatalf("TailStart with room for all = %d, want 1", got)
	}
}

func TestTailStartKeepsAtLeastTheNewestTurn(t *testing.T) {
	messages := []transcript.Message{
		textMessage(1, transcript.MessageRoleUser, "r1", "task"),
		textMessage(2, transcript.MessageRoleAssistant, "r1", strings.Repeat("b", 20_000)),
	}
	if got := TailStart(messages, 100); got != 1 {
		t.Fatalf("TailStart = %d, want 1", got)
	}
}

func TestKeptHoldsTheRunsUserTextAndSteersVerbatim(t *testing.T) {
	result := toolStep(3, "c1", "output")[2]
	result.RunID = "r1"
	result.Parts[0].ToolResult.Guidance = []string{"look at the logs"}
	covered := []transcript.Message{
		textMessage(1, transcript.MessageRoleUser, "r1", "second ask"),
		textMessage(2, transcript.MessageRoleUser, "r0", "another run"),
		result,
	}

	got := Kept(&transcript.Compaction{RunID: "r1", Kept: []string{"User: first ask"}}, covered, "r1")
	want := []string{"User: first ask", "User: second ask", "User guidance: look at the logs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Kept = %q, want %q", got, want)
	}
	if got := Kept(&transcript.Compaction{RunID: "r0", Kept: []string{"User: old"}}, covered[:1], "r1"); !reflect.DeepEqual(got, []string{"User: second ask"}) {
		t.Fatalf("Kept across runs = %q", got)
	}
}

func TestEffectiveWindowLeavesRoomForTheReplyAndAReserve(t *testing.T) {
	for _, tc := range []struct{ window, output, want int }{
		{200_000, 16_384, 173_616},
		{0, 16_384, 105_216},
		{20_000, 16_384, 10_000},
	} {
		if got := EffectiveWindow(tc.window, tc.output); got != tc.want {
			t.Errorf("EffectiveWindow(%d, %d) = %d, want %d", tc.window, tc.output, got, tc.want)
		}
	}
}

func TestSummaryTextCarriesTheSummaryAndTheKeptTexts(t *testing.T) {
	text := SummaryText(&transcript.Compaction{Summary: "Goal: ship.", Kept: []string{"User: do it"}})
	if !strings.Contains(text, "Goal: ship.") || !strings.Contains(text, "User: do it") {
		t.Fatalf("summary text = %q", text)
	}
	if SummaryText(&transcript.Compaction{Cleared: true}) != "" || SummaryText(nil) != "" {
		t.Fatal("a cleared or missing boundary has no summary text")
	}
}
```

`internal/agent/context/summarize_test.go`:

```go
package agentcontext

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type recordingGenerator struct {
	requests []providers.Request
}

func (g *recordingGenerator) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	g.requests = append(g.requests, request)
	return providers.Response{Text: fmt.Sprintf("summary %d", len(g.requests))}, nil
}

func TestSummarizeChunksLongHistoryAndMergesThePartials(t *testing.T) {
	var messages []transcript.Message
	for i := 1; i <= 6; i++ {
		messages = append(messages, textMessage(int64(i), transcript.MessageRoleUser, "r1", strings.Repeat("w", 12_000)))
	}
	generator := &recordingGenerator{}

	summary, err := Summarize(context.Background(), generator, SummaryInput{SessionID: "s1", Previous: "EARLIER", Messages: messages, ChunkTokens: 4_000})

	if err != nil || summary != "summary 7" || len(generator.requests) != 7 {
		t.Fatalf("summary = %q err = %v requests = %d, want six chunks and one merge", summary, err, len(generator.requests))
	}
	for _, request := range generator.requests {
		if !strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			t.Fatalf("system prompt = %q", request.SystemPrompt)
		}
	}
	if first := generator.requests[0].Messages[0].Content; !strings.Contains(first, "EARLIER") {
		t.Fatalf("first chunk = %.80q, want the previous summary", first)
	}
	merge := generator.requests[6].Messages[0].Content
	if !strings.HasPrefix(merge, "Merge these partial summaries") || !strings.Contains(merge, "summary 1") || !strings.Contains(merge, "summary 6") {
		t.Fatalf("merge request = %.200q", merge)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/context/`
Expected: build failure `undefined: TailStart / Kept / EffectiveWindow / Summarize / SummaryInput / SummaryText`.

- [ ] **Step 3: Boundary helpers** — create `internal/agent/context/boundary.go`:

```go
package agentcontext

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// TailPercent is the share of the usable window the newest turns keep verbatim
// after a summary.
const TailPercent = 25

// UnknownWindowTokens stands in for a model whose window is unknown.
const UnknownWindowTokens = 128_000

// EffectiveWindow is the prompt room of a model: its window without the output
// limit and a 5% reserve, never less than half the window.
func EffectiveWindow(windowTokens int, maxOutputTokens int) int {
	if windowTokens <= 0 {
		windowTokens = UnknownWindowTokens
	}
	return max(windowTokens-maxOutputTokens-windowTokens/20, windowTokens/2)
}

// TailStart is the index where the kept tail of messages begins: the newest
// whole turns within budget tokens, at least the newest one. A tail never starts
// at a call or a result, nor while a call before it waits for its result.
// 0 means nothing before the tail can be summarised.
func TailStart(messages []transcript.Message, budget int) int {
	cuts := cutPoints(messages)
	start, used := 0, 0
	for i := len(messages) - 1; i > 0; i-- {
		used += EstimateMessageTokens(messages[i : i+1])
		if !cuts[i] {
			continue
		}
		if used > budget && start > 0 {
			break
		}
		start = i
		if used > budget {
			break
		}
	}
	return start
}

// cutPoints marks where a tail may start; a call whose result never arrived
// does not hold a cut back.
func cutPoints(messages []transcript.Message) []bool {
	answered := map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil {
				answered[strings.TrimSpace(part.ToolResult.ToolCallID)] = true
			}
		}
	}
	open := map[string]bool{}
	cuts := make([]bool, len(messages))
	for i, message := range messages {
		cuts[i] = len(open) == 0 && message.Role != transcript.MessageRoleTool && len(messageToolCallIDs(message)) == 0
		for _, part := range message.Parts {
			if part.ToolCall != nil && answered[strings.TrimSpace(part.ToolCall.ID)] {
				open[strings.TrimSpace(part.ToolCall.ID)] = true
			}
			if part.ToolResult != nil {
				delete(open, strings.TrimSpace(part.ToolResult.ToolCallID))
			}
		}
	}
	return cuts
}

// Kept is what a new boundary keeps verbatim: the run's user messages and the
// guidance steered into its tool results, after what the boundary it replaces
// kept for the same run.
func Kept(previous *transcript.Compaction, covered []transcript.Message, runID string) []string {
	runID = strings.TrimSpace(runID)
	var kept []string
	if previous != nil && previous.RunID == runID {
		kept = append(kept, previous.Kept...)
	}
	for _, message := range covered {
		if strings.TrimSpace(message.RunID) != runID {
			continue
		}
		if message.Role == transcript.MessageRoleUser {
			if text := strings.TrimSpace(message.Content); text != "" {
				kept = append(kept, "User: "+text)
			}
			continue
		}
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			for _, guidance := range part.ToolResult.Guidance {
				kept = append(kept, "User guidance: "+guidance)
			}
		}
	}
	return kept
}

// SummaryText is the user text that stands for the history a boundary covers;
// empty for a /clear or no boundary.
func SummaryText(compaction *transcript.Compaction) string {
	if compaction == nil {
		return ""
	}
	summary := strings.TrimSpace(compaction.Summary)
	if summary == "" && len(compaction.Kept) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Summary of the earlier conversation, which it replaces:\n\n")
	b.WriteString(summary)
	if len(compaction.Kept) > 0 {
		b.WriteString("\n\nKept verbatim from that part:")
		for _, text := range compaction.Kept {
			b.WriteString("\n\n")
			b.WriteString(text)
		}
	}
	return b.String()
}

// SummaryMessages is the boundary as the first message of a request, when it
// has anything to say.
func SummaryMessages(compaction *transcript.Compaction) []providers.Message {
	text := SummaryText(compaction)
	if text == "" {
		return nil
	}
	return []providers.Message{{Role: string(transcript.MessageRoleUser), Content: text}}
}

// BoundaryLabel is the content clients show for a boundary message.
func BoundaryLabel(compaction transcript.Compaction) string {
	if compaction.Cleared {
		return "Context cleared."
	}
	return fmt.Sprintf("Context compacted: ~%s -> ~%s tokens.", FormatShortNumber(compaction.TokensBefore), FormatShortNumber(compaction.TokensAfter))
}
```

- [ ] **Step 4: Chunked summaries** — delete `internal/agent/context/compact.go` and `internal/agent/context/markers.go`; create `internal/agent/context/summarize.go`:

```go
package agentcontext

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	minSummaryChunkTokens = 4_000
	summaryTextRunes      = 8_000
	summaryToolRunes      = 4_000
)

// Generator produces one model reply; agent.Model and providers.Runtime satisfy it.
type Generator interface {
	Generate(ctx context.Context, req providers.Request) (providers.Response, error)
}

// SummaryInput is history summarised on its own, outside the conversation's own
// request; Previous is the summary it continues.
type SummaryInput struct {
	SessionID   string
	Previous    string
	Messages    []transcript.Message
	ChunkTokens int
}

// Summarize summarises the input in chunks of about ChunkTokens and merges the
// partial summaries into one.
func Summarize(ctx context.Context, generator Generator, in SummaryInput) (string, error) {
	chunks := summaryChunks(in.Previous, in.Messages, max(in.ChunkTokens, minSummaryChunkTokens))
	if len(chunks) == 0 {
		return "", errors.New("nothing to summarise")
	}
	partials := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		summary, err := generateSummary(ctx, generator, in.SessionID, "Summarise this part of a conversation:\n\n"+chunk)
		if err != nil {
			return "", err
		}
		partials = append(partials, summary)
	}
	if len(partials) == 1 {
		return partials[0], nil
	}
	return generateSummary(ctx, generator, in.SessionID, "Merge these partial summaries of one conversation, oldest first, into a single summary:\n\n"+strings.Join(partials, "\n\n---\n\n"))
}

func generateSummary(ctx context.Context, generator Generator, sessionID string, content string) (string, error) {
	response, err := generator.Generate(ctx, providers.Request{
		SessionID:    sessionID,
		SystemPrompt: summarySystemPrompt(),
		Messages:     []providers.Message{{Role: string(transcript.MessageRoleUser), Content: content}},
	})
	if err != nil {
		return "", err
	}
	return summaryReply(response)
}

// summaryReply is the text of a summary reply; a cut, filtered or empty one fails.
func summaryReply(response providers.Response) (string, error) {
	if err := stopReasonError(response); err != nil {
		return "", err
	}
	text := strings.TrimSpace(response.Text)
	if text == "" {
		return "", errors.New("compact summary is empty")
	}
	return text, nil
}

func summarySystemPrompt() string {
	return strings.TrimSpace(`You compact matrixclaw chat histories into durable working context for future assistant turns.

Write a concise, factual summary with these sections:
Goal: what the user wants and the constraints they gave.
Decisions: choices already made and why.
Files changed: files, modules, commands and services changed or investigated.
Errors and fixes: failures seen and how they were resolved, or why they remain.
Current state: what is done and verified, what is in progress.
Next step: the immediate next action.

Leave out greetings, speculation, duplicated logs, raw tool dumps, secrets, API keys, OAuth tokens and long code or output blocks. Summarise a tool call together with its result. Reply in English.`)
}

// summaryChunks renders the previous summary and the message groups as text and
// packs them into chunks of about chunkTokens.
func summaryChunks(previous string, messages []transcript.Message, chunkTokens int) []string {
	var pieces []string
	if previous = strings.TrimSpace(previous); previous != "" {
		pieces = append(pieces, previous)
	}
	for _, group := range messageGroups(messages) {
		if text := groupText(group); text != "" {
			pieces = append(pieces, text)
		}
	}
	var chunks []string
	var current strings.Builder
	used := 0
	for _, piece := range pieces {
		piece = trimToTokens(piece, chunkTokens)
		tokens := EstimateTextTokens(piece)
		if used > 0 && used+tokens > chunkTokens {
			chunks = append(chunks, current.String())
			current.Reset()
			used = 0
		}
		if used > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(piece)
		used += tokens
	}
	if used > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// trimToTokens keeps the start of text within about maxTokens.
func trimToTokens(text string, maxTokens int) string {
	if EstimateTextTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	return string(runes[:min(len(runes), maxTokens*5/2)]) + "\n[truncated]"
}

// messageGroup is a message and the results of the calls it made.
type messageGroup struct {
	messages []transcript.Message
}

// messageGroups groups each message with the results of its calls; system rows
// (engine notes, boundaries) and plan runner prompts are left out.
func messageGroups(messages []transcript.Message) []messageGroup {
	filtered := make([]transcript.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == transcript.MessageRoleSystem || prompt.IsPlanRunPrompt(message) {
			continue
		}
		filtered = append(filtered, message)
	}
	groups := make([]messageGroup, 0, len(filtered))
	for i := 0; i < len(filtered); i++ {
		group := messageGroup{messages: []transcript.Message{filtered[i]}}
		ids := messageToolCallIDs(filtered[i])
		for len(ids) > 0 && i+1 < len(filtered) && messageIsToolResultFor(filtered[i+1], ids) {
			i++
			group.messages = append(group.messages, filtered[i])
		}
		groups = append(groups, group)
	}
	return groups
}

func groupText(group messageGroup) string {
	parts := make([]string, 0, len(group.messages))
	for _, message := range group.messages {
		role := strings.TrimSpace(string(message.Role))
		text := messageSummaryText(message)
		if role == "" || text == "" {
			continue
		}
		parts = append(parts, role+": "+text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func messageSummaryText(message transcript.Message) string {
	if len(message.Parts) == 0 {
		return trimRunesEnd(message.Content, summaryTextRunes)
	}
	values := make([]string, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch {
		case part.Text != nil:
			values = append(values, trimRunesEnd(part.Text.Text, summaryTextRunes))
		case part.Image != nil:
			values = append(values, "image: "+imagePartLabel(*part.Image))
		case part.ToolCall != nil:
			values = append(values, "tool call: "+part.ToolCall.Name+" "+trimRunesEnd(part.ToolCall.Input, summaryToolRunes))
		case part.ToolResult != nil:
			values = append(values, "tool result: "+part.ToolResult.Name+" "+trimRunesEnd(part.ToolResult.Content, summaryToolRunes))
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n"))
}

func messageToolCallIDs(message transcript.Message) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, part := range message.Parts {
		if part.ToolCall == nil {
			continue
		}
		if id := strings.TrimSpace(part.ToolCall.ID); id != "" {
			ids[id] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func messageIsToolResultFor(message transcript.Message, ids map[string]struct{}) bool {
	if len(ids) == 0 || message.Role != transcript.MessageRoleTool {
		return false
	}
	for _, part := range message.Parts {
		if part.ToolResult == nil {
			continue
		}
		if _, ok := ids[strings.TrimSpace(part.ToolResult.ToolCallID)]; ok {
			return true
		}
	}
	return false
}

func trimRunesEnd(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	return string([]rune(value)[:maxRunes]) + "\n[truncated]"
}
```

In `internal/agent/context/estimate.go` replace `SessionTokens` with:

```go
// SessionTokens estimates a session's context: fixed parts, the boundary's
// summary and the messages after it.
func SessionTokens(baseTokens int, compaction *transcript.Compaction, messages []transcript.Message) int {
	return baseTokens + EstimateTextTokens(SummaryText(compaction)) + EstimateMessageTokens(messages)
}
```

Also update the package comment at the top of `internal/agent/context/attachments.go` to: `// Package agentcontext builds the model's view of a session: provider conversation, context boundaries and summaries, and token estimates.`

- [ ] **Step 5: Run the agentcontext tests**

Run: `go test ./internal/agent/context/`
Expected: PASS (the `agent` and `core` packages do not build yet; that is Step 7–9).

- [ ] **Step 6: Write the failing engine tests** — in `internal/agent/engine_test.go` delete the var `markerPattern` (and the `regexp` import if nothing else uses it) and delete `TestAutoCompactionAddsMarkerBeforeTheModelCall`, `TestContextLengthErrorCompactsAndRetriesOnce` and `TestOversizedRequestIsCompactedBeforeSending`. Create `internal/agent/compact_test.go`:

```go
package agent_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// pastTurn is an earlier run of the session: a question and a long answer.
func pastTurn(answerRunes int) []transcript.Message {
	answer := strings.Repeat("a", answerRunes)
	return []transcript.Message{
		{ID: "past_q", SessionID: agenttest.SessionID, RunID: "run_0", Role: transcript.MessageRoleUser, Content: "earlier question", Parts: transcript.NormalizeMessageParts("earlier question", nil)},
		{ID: "past_a", SessionID: agenttest.SessionID, RunID: "run_0", Role: transcript.MessageRoleAssistant, Content: answer, Parts: transcript.NormalizeMessageParts(answer, nil)},
	}
}

func boundaries(messages []transcript.Message) []transcript.Message {
	var out []transcript.Message
	for _, message := range messages {
		if message.Compaction != nil {
			out = append(out, message)
		}
	}
	return out
}

func isSummaryRequest(request providers.Request) bool {
	return strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories")
}

func summaryCount(requests []providers.Request) int {
	count := 0
	for _, request := range requests {
		if isSummaryRequest(request) {
			count++
		}
	}
	return count
}

func TestSummaryReplacesOlderHistoryBeforeTheModelCall(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(400_000)...)
	f.Prompts.WindowTokens = 100_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 2 || !isSummaryRequest(requests[0]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	main := requests[1]
	if len(main.Messages) != 2 || main.Messages[0].Role != "user" || !strings.Contains(main.Messages[0].Content, "SUMMARY") || main.Messages[1].Content != "do the task" || strings.Contains(main.SystemPrompt, "SUMMARY") {
		t.Fatalf("main request = %q / %+v", main.SystemPrompt, main.Messages)
	}
	marks := boundaries(f.Journal.Messages)
	answer, _ := f.Journal.Message("past_a")
	if len(marks) != 1 || marks[0].Role != transcript.MessageRoleSystem || marks[0].RunID != "" {
		t.Fatalf("boundaries = %+v", marks)
	}
	if c := marks[0].Compaction; c.Summary != "SUMMARY" || c.CoversThroughSeq != answer.Seq || c.RunID != agenttest.RunID || c.TokensBefore <= c.TokensAfter || len(c.Kept) != 0 {
		t.Fatalf("compaction = %+v", c)
	}
	if len(f.Journal.Steps) != 2 || f.Journal.Steps[0].StopReason != "compact" {
		t.Fatalf("steps = %+v, want the summary recorded as compact first", f.Journal.Steps)
	}
}

func TestRequestOverTheThresholdIsSummarisedBeforeSending(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(100_000)...)
	f.Prompts.Text = strings.Repeat("s", 240_000)
	f.Prompts.WindowTokens = 100_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 2 || !isSummaryRequest(requests[0]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if first := requests[1].Messages[0]; !strings.Contains(first.Content, "SUMMARY") {
		t.Fatalf("main request starts with %+v", first)
	}
}

func TestContextLengthErrorSummarisesAndRetriesOnce(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(80_000)...)
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("context_length_exceeded")}, text("SUMMARY"), text("Recovered."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Recovered." || len(requests) != 3 || !isSummaryRequest(requests[1]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	if first := requests[2].Messages[0]; !strings.Contains(first.Content, "SUMMARY") {
		t.Fatalf("retry starts with %+v", first)
	}
	if len(boundaries(f.Journal.Messages)) != 1 {
		t.Fatalf("boundaries = %+v", boundaries(f.Journal.Messages))
	}
}

func TestSummaryKeepsTheAssignmentStepsAndWholeToolSteps(t *testing.T) {
	f := agenttest.NewFixture()
	// A 20k window keeps a tail of ~2.5k tokens; the base puts the fourth step,
	// with three ~2.5k results, over the 80k threshold.
	f.Prompts.WindowTokens = 20_000
	f.Prompts.BaseTokens = 74_000
	big := strings.Repeat("b", 10_000)
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result { return tools.Result{Content: call.ToolCallID + big} }
	f.Inbox.Steers = []string{"focus on the parser"}
	model := agenttest.NewScriptedModel(calls(call("r1", "read")), calls(call("r2", "read")), calls(call("r3", "read")), text("SUMMARY"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 5 || !isSummaryRequest(requests[3]) {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(requests))
	}
	marks := boundaries(f.Journal.Messages)
	r2, _ := f.Journal.Result("r2")
	if len(marks) != 1 || marks[0].Compaction.CoversThroughSeq != r2.Seq {
		t.Fatalf("boundaries = %+v", marks)
	}
	if kept := marks[0].Compaction.Kept; len(kept) != 2 || kept[0] != "User: do the task" || kept[1] != "User guidance: focus on the parser" {
		t.Fatalf("kept = %q", kept)
	}
	assertBoundaryAndLastToolStep(t, requests[4])

	again := agenttest.NewScriptedModel(text("Again."))
	run(t, f, again)
	assertBoundaryAndLastToolStep(t, again.Requests()[0])
}

// assertBoundaryAndLastToolStep checks a request that starts from the boundary:
// the summary with the kept texts, then the whole r3 step.
func assertBoundaryAndLastToolStep(t *testing.T, request providers.Request) {
	t.Helper()
	messages := request.Messages
	if len(messages) != 3 || !strings.Contains(messages[0].Content, "User: do the task") || !strings.Contains(messages[0].Content, "User guidance: focus on the parser") {
		t.Fatalf("request messages = %+v", messages)
	}
	if len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].ID != "r3" || messages[2].ToolCallID != "r3" {
		t.Fatalf("tail = %+v", messages[1:])
	}
}

func TestTwoLowYieldSummariesStopSummarising(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.BaseTokens = 90_000
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), text("SUM1"), calls(call("c2", "read")), text("SUM2"), calls(call("c3", "read")), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Done." || len(requests) != 6 || summaryCount(requests) != 2 {
		t.Fatalf("outcome = %+v requests = %d summaries = %d", outcome, len(requests), summaryCount(requests))
	}
	marks := boundaries(f.Journal.Messages)
	if len(marks) != 2 || len(marks[1].Compaction.Kept) != 1 || marks[1].Compaction.Kept[0] != "User: do the task" {
		t.Fatalf("boundaries = %+v", marks)
	}
}
```

- [ ] **Step 7: Engine** — `internal/agent/ports.go`: replace `Window` and `Prompts` with:

```go
// Window is the transcript a run starts from: the session's newest context
// boundary, if any, and the messages after what it covers, ordered by seq.
type Window struct {
	Boundary *transcript.Message
	Messages []transcript.Message
}

// Compaction is the boundary's compaction; nil without a boundary.
func (w Window) Compaction() *transcript.Compaction {
	if w.Boundary == nil {
		return nil
	}
	return w.Boundary.Compaction
}
```

```go
// Prompts supplies the core-owned parts of every request: the system prompt and
// custom instructions, and the context budget.
type Prompts interface {
	System(ctx context.Context, history []transcript.Message) (system, custom string)
	Budget(ctx context.Context) (baseTokens, windowTokens int, err error)
}
```

`internal/agent/journal.go`: replace the `history` struct, `newHistory` and `add`, and add `window`:

```go
// history is the run's in-memory transcript; every write goes through the Journal
// port first and is then announced on the Sink.
type history struct {
	port     Journal
	sink     Sink
	messages []transcript.Message
	boundary *transcript.Message
	index    map[string]int
	calls    map[string]int
	results  map[string]struct{}
}

func newHistory(port Journal, sink Sink, window Window) *history {
	h := &history{port: port, sink: sink, boundary: window.Boundary, index: map[string]int{}, calls: map[string]int{}, results: map[string]struct{}{}}
	for _, message := range window.Messages {
		h.add(message)
	}
	return h
}

// window is what the model sees: the newest boundary and the messages after
// what it covers.
func (h *history) window() (*transcript.Compaction, []transcript.Message) {
	var compaction *transcript.Compaction
	if h.boundary != nil {
		compaction = h.boundary.Compaction
	}
	messages := make([]transcript.Message, 0, len(h.messages))
	for _, message := range h.messages {
		if message.Compaction != nil || compaction != nil && message.Seq <= compaction.CoversThroughSeq {
			continue
		}
		messages = append(messages, message)
	}
	return compaction, messages
}

func (h *history) add(message transcript.Message) {
	if message.Compaction != nil {
		boundary := message
		h.boundary = &boundary
	}
	h.index[message.ID] = len(h.messages)
	h.messages = append(h.messages, message)
	h.indexParts(len(h.messages) - 1)
}
```

`internal/agent/budget.go`: add to `Counters`, after `OutputLimit`:

```go
	LowYield      int           `json:"low_yield,omitempty"`
```

and extend its doc comment's first sentence to "…its output-limit state and its run of low-yield summaries;".

Create `internal/agent/compact.go`:

```go
package agent

import (
	"context"
	"fmt"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// lowYieldLimit is how many summaries in a row may each save under a tenth of
// the prompt before the run stops summarising.
const lowYieldLimit = 2

// compactHistory summarises the history before the newest turns that fit in
// tailPercent of the window into a boundary; false means nothing could be
// summarised.
func (r *run) compactHistory(ctx context.Context, b contextBudget, before int, tailPercent int) (bool, error) {
	previous, messages := r.history.window()
	limit := agentcontext.EffectiveWindow(b.window, int(providers.DefaultMaxOutputTokens))
	cut := agentcontext.TailStart(messages, limit*tailPercent/100)
	if cut == 0 {
		return false, nil
	}
	covered := messages[:cut]
	summary, err := agentcontext.Summarize(ctx, summaryModel{r: r}, agentcontext.SummaryInput{
		SessionID:   r.task.SessionID,
		Previous:    agentcontext.SummaryText(previous),
		Messages:    covered,
		ChunkTokens: limit / 2,
	})
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, r.writeBoundary(ctx, previous, covered, summary, before)
}

// writeBoundary journals the boundary whose summary replaces covered and the
// boundary before it.
func (r *run) writeBoundary(ctx context.Context, previous *transcript.Compaction, covered []transcript.Message, summary string, before int) error {
	compaction := transcript.Compaction{
		Summary:          strings.TrimSpace(summary),
		Kept:             agentcontext.Kept(previous, covered, r.task.RunID),
		CoversThroughSeq: covered[len(covered)-1].Seq,
		RunID:            r.task.RunID,
		TokensBefore:     before,
	}
	compaction.TokensAfter = max(0, before-agentcontext.EstimateMessageTokens(covered)-agentcontext.EstimateTextTokens(agentcontext.SummaryText(previous))+agentcontext.EstimateTextTokens(agentcontext.SummaryText(&compaction)))
	r.counters.observeSummary(compaction.TokensBefore, compaction.TokensAfter)
	content := agentcontext.BoundaryLabel(compaction)
	now := r.Now()
	err := r.history.append(ctx, transcript.Message{
		ID:         r.NewID("msg"),
		SessionID:  r.task.SessionID,
		Role:       transcript.MessageRoleSystem,
		Content:    content,
		Parts:      transcript.NormalizeMessageParts(content, nil),
		Compaction: &compaction,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return fmt.Errorf("auto compact session: %w", err)
	}
	return nil
}

// observeSummary counts the summaries in a row that saved under a tenth of the prompt.
func (c *Counters) observeSummary(before, after int) {
	if before > 0 && (before-after)*10 < before {
		c.LowYield++
		return
	}
	c.LowYield = 0
}
```

`internal/agent/request.go`: delete `compactHistory` (it moved); replace `buildRequest` and `autoCompact` with:

```go
// buildRequest assembles the step's request: the boundary's summary first, then
// the history after it; the final turn keeps the tools defined but forbids
// calling them, so the cached prefix survives.
func (r *run) buildRequest(ctx context.Context, final StopReason) (providers.Request, error) {
	compaction, messages := r.history.window()
	system, custom := r.Prompts.System(ctx, messages)
	request := providers.Request{
		RunID:              r.task.RunID,
		SessionID:          r.task.SessionID,
		SystemPrompt:       system,
		CustomInstructions: custom,
		CacheKey:           r.task.SessionID,
		MaxOutputTokens:    r.counters.OutputLimit,
	}
	if final != "" {
		request.ToolChoice = providers.ToolChoiceNone
	}
	summary := agentcontext.SummaryMessages(compaction)
	if !ToolUseAllowed(r.task.Model) {
		request.Messages = append(summary, agentcontext.TextOnlyConversation(messages, r.task.RunID)...)
		request.Messages = providers.NormalizeMessages(request.Messages, providers.ToolUseDisabled)
		return request, nil
	}
	conversation, err := agentcontext.Conversation(ctx, messages, r.Attachments, r.task.RunID, ImageInputAllowed(r.task.Model), modelIdentity(r.task.Model))
	if err != nil {
		return providers.Request{}, err
	}
	request.Messages = append(summary, conversation...)
	request.Tools = toolDefinitions(r.Tools.Specs(ctx))
	return request, nil
}
```

```go
func (r *run) autoCompact(ctx context.Context, b contextBudget) (bool, error) {
	compaction, messages := r.history.window()
	tokens := agentcontext.SessionTokens(b.base, compaction, messages)
	recommended, _ := agentcontext.Recommendation(tokens, b.window)
	if !recommended || r.counters.LowYield >= lowYieldLimit {
		return false, nil
	}
	return r.compactHistory(ctx, b, tokens, agentcontext.TailPercent)
}
```

Remove the imports `request.go` no longer needs (`fmt`, `strings`, `transcript`) — let the compiler guide you.

`internal/agent/engine.go`: in `step`, replace the block from `compacted, err := r.autoCompact(ctx, budget)` up to (not including) `if final != "" && errors.Is(err, providers.ErrEmptyResponse)` with:

```go
	compacted, err := r.autoCompact(ctx, budget)
	if err != nil {
		return failedStep(err)
	}
	request, err := r.buildRequest(ctx, final)
	if err != nil {
		return failedStep(err)
	}
	if !compacted && r.counters.LowYield < lowYieldLimit && requestNeedsCompact(request, budget) {
		if compacted, err = r.compactHistory(ctx, budget, agentcontext.EstimateRequestTokens(request), agentcontext.TailPercent); err != nil {
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
		compacted, compactErr := r.compactHistory(ctx, budget, agentcontext.EstimateRequestTokens(request), agentcontext.TailPercent/2)
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
```

`internal/agent/agenttest/agenttest.go`:
- Replace `Journal.Load` with (and change the `Journal` doc comment's first sentence to "Journal keeps the transcript in memory, loads it like core — the newest boundary and the messages after it — and counts streaming writes."):

```go
func (j *Journal) Load(ctx context.Context, _ string) (agent.Window, error) {
	if err := ctx.Err(); err != nil {
		return agent.Window{}, err
	}
	if j.LoadErr != nil {
		return agent.Window{}, j.LoadErr
	}
	var window agent.Window
	for i := len(j.Messages) - 1; i >= 0; i-- {
		if j.Messages[i].Compaction != nil {
			boundary := j.Messages[i]
			window.Boundary = &boundary
			break
		}
	}
	var covers int64
	if compaction := window.Compaction(); compaction != nil {
		covers = compaction.CoversThroughSeq
	}
	for _, message := range j.Messages {
		if message.Compaction == nil && message.Seq > covers {
			window.Messages = append(window.Messages, message)
		}
	}
	return window, nil
}
```

- Replace the `Prompts` doc comment, `System` and delete `PlanSnapshot`:

```go
// Prompts returns Text as the system prompt and fixed context budget numbers.
type Prompts struct {
	Text         string
	BaseTokens   int
	WindowTokens int
	BudgetReads  int
}

func (p *Prompts) System(context.Context, []transcript.Message) (string, string) {
	return p.Text, ""
}
```

- Add after `NewFixture`:

```go
// WithHistory puts messages before the run's user message, as an earlier part
// of the session.
func (f *Fixture) WithHistory(messages ...transcript.Message) {
	user := f.Journal.Messages[len(f.Journal.Messages)-1]
	f.Journal.Messages, f.Journal.seq = nil, 0
	f.Journal.Seed(append(messages, user)...)
}
```

- [ ] **Step 8: Run the engine tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 9: Core** — `internal/core/ports.go`, `MessageStore`: add

```go
	LatestCompaction(ctx context.Context, sessionID string) (transcript.Message, error)
```

`internal/core/agent_journal.go`: replace `Load` and add `contextWindow` (import `errors`):

```go
func (j coreJournal) Load(ctx context.Context, sessionID string) (agent.Window, error) {
	return j.c.contextWindow(ctx, sessionID)
}

// contextWindow is what the model sees of a session: its newest context boundary
// and the messages after what the boundary covers.
func (c *Core) contextWindow(ctx context.Context, sessionID string) (agent.Window, error) {
	boundary, err := c.store.LatestCompaction(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		messages, err := c.store.ListMessages(ctx, sessionID, 0)
		return agent.Window{Messages: messages}, err
	}
	if err != nil {
		return agent.Window{}, err
	}
	messages, err := c.store.ListMessagesAfter(ctx, sessionID, boundary.Compaction.CoversThroughSeq, 0)
	if err != nil {
		return agent.Window{}, err
	}
	window := agent.Window{Boundary: &boundary, Messages: make([]transcript.Message, 0, len(messages))}
	for _, message := range messages {
		if message.Compaction == nil {
			window.Messages = append(window.Messages, message)
		}
	}
	return window, nil
}
```

`internal/core/agent_prompts.go`: replace `System`, delete `PlanSnapshot`, and in `nativeSystemPrompt` drop the `compactSummary` parameter and its `"Session context summary:\n"` section:

```go
func (p corePrompts) System(ctx context.Context, history []transcript.Message) (string, string) {
	assistant := p.c.assistantProfile()
	return p.c.nativeSystemPrompt(ctx, p.turn, assistant, history), assistant.CustomInstructions
}
```

The new signature is `func (c *Core) nativeSystemPrompt(ctx context.Context, turn nativeTurn, assistant AssistantProfile, history []transcript.Message) string`; its body is unchanged apart from deleting

```go
	if compactSummary != "" {
		sections = append(sections, "Session context summary:\n"+compactSummary)
	}
```

`internal/core/plan_tools.go`: delete `compactSessionPlanSnapshot` (no caller is left).

`internal/core/context_boundary.go`: in `ClearContext` replace `agentcontext.ClearedMarkerContent()` with `agentcontext.BoundaryLabel(compaction)`.

`internal/core/system_messages.go`: replace `CreateSystemMessage` with the version below and drop the `agentcontext` and `fmt` imports:

```go
func (c *Core) CreateSystemMessage(ctx context.Context, sessionID string, content string) (transcript.Message, error) {
	sessionID = normalizeText(sessionID)
	content = strings.TrimSpace(content)
	if sessionID == "" {
		return transcript.Message{}, ErrSessionRequired
	}
	if content == "" {
		return transcript.Message{}, ErrInvalidInput
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return transcript.Message{}, err
	}
	now := c.now().UTC()
	message := transcript.Message{
		ID:        c.newID("msg"),
		SessionID: sessionID,
		Role:      transcript.MessageRoleSystem,
		Content:   content,
		Parts: []transcript.MessagePart{{
			Kind: transcript.MessagePartKindText,
			Text: &transcript.TextPart{Text: content},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := c.store.SaveMessage(ctx, message); err != nil {
		return transcript.Message{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: sessionID, Payload: message})
	return message, nil
}
```

`internal/core/session_context.go` (add imports `agent` and `transcript` is already there): replace `SessionContext`, `CompactSession`, `contextReportForSession` and `contextReport` with:

```go
func (c *Core) SessionContext(ctx context.Context, sessionID string) (ContextReport, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return ContextReport{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return ContextReport{}, err
	}
	session = c.decorateSessionLLM(session)
	window, err := c.contextWindow(ctx, sessionID)
	if err != nil {
		return ContextReport{}, err
	}
	return c.contextReportForSession(session, window), nil
}

// CompactSession summarises what the model sees of the session into a boundary
// that covers all of it.
func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return CompactSessionResult{}, ErrSessionRequired
	}
	if executing, err := c.sessionRunExecuting(ctx, sessionID); err != nil {
		return CompactSessionResult{}, err
	} else if executing {
		return CompactSessionResult{}, fmt.Errorf("%w: wait for the current run to finish before compacting", ErrRunActive)
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	session = c.decorateSessionLLM(session)
	window, err := c.contextWindow(ctx, sessionID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	if len(window.Messages) == 0 {
		return CompactSessionResult{}, ErrInvalidInput
	}
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return CompactSessionResult{}, err
	}
	previous := window.Compaction()
	base := c.contextBaseTokens()
	limit := agentcontext.EffectiveWindow(c.sessionContextWindowTokens(session), int(providers.DefaultMaxOutputTokens))
	summary, err := agentcontext.Summarize(ctx, runtime, agentcontext.SummaryInput{
		SessionID:   session.ID,
		Previous:    agentcontext.SummaryText(previous),
		Messages:    window.Messages,
		ChunkTokens: limit / 2,
	})
	if err != nil {
		return CompactSessionResult{}, err
	}
	compaction := transcript.Compaction{
		Summary:          summary,
		CoversThroughSeq: window.Messages[len(window.Messages)-1].Seq,
		TokensBefore:     agentcontext.SessionTokens(base, previous, window.Messages),
	}
	compaction.TokensAfter = agentcontext.SessionTokens(base, &compaction, nil)
	message, err := c.appendBoundary(ctx, session.ID, agentcontext.BoundaryLabel(compaction), compaction)
	if err != nil {
		return CompactSessionResult{}, err
	}
	next, err := c.contextWindow(ctx, session.ID)
	if err != nil {
		return CompactSessionResult{}, err
	}
	return CompactSessionResult{Message: message, Context: c.contextReportForSession(session, next)}, nil
}
```

```go
func (c *Core) contextReportForSession(session Session, window agent.Window) ContextReport {
	report := c.contextReport(session.ID, window)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	recommended, reason := agentcontext.Recommendation(report.TokenEstimate, report.WindowTokens)
	report.Compact = ContextCompact{Recommended: recommended, Reason: reason}
	return report
}

func (c *Core) contextReport(sessionID string, window agent.Window) ContextReport {
	assistant := c.assistantProfile()
	systemPrompt := prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)
	customInstructions := strings.TrimSpace(assistant.CustomInstructions)
	blocks := make([]ContextBlock, 0, 5)
	if systemPrompt != "" {
		blocks = append(blocks, ContextBlock{ID: "system", Kind: ContextBlockSystemPrompt, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(systemPrompt), Included: true, CacheStability: "stable"})
	}
	if customInstructions != "" {
		blocks = append(blocks, ContextBlock{ID: "custom_instructions", Kind: ContextBlockCustomInstructions, Source: "assistant_profile", TokenEstimate: agentcontext.EstimateTextTokens(customInstructions), Included: true, CacheStability: "stable"})
	}
	if compaction := window.Compaction(); compaction != nil {
		block := ContextBlock{ID: "compact_summary", Kind: ContextBlockCompactSummary, Source: "session_compact", TokenEstimate: agentcontext.EstimateTextTokens(agentcontext.SummaryText(compaction)), Included: true, CacheStability: "stable"}
		if compaction.Cleared {
			block.ID, block.Kind, block.Source = "clear_marker", ContextBlockClearMarker, "session_clear"
		}
		blocks = append(blocks, block)
	}
	if len(window.Messages) > 0 {
		blocks = append(blocks, ContextBlock{ID: "messages", Kind: ContextBlockMessages, Source: "session_history", TokenEstimate: agentcontext.EstimateMessageTokens(window.Messages), Included: true, CacheStability: "dynamic"})
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
	return ContextReport{
		SessionID:         sessionID,
		Estimated:         true,
		TokenEstimate:     total,
		MessageCount:      len(window.Messages),
		Blocks:            blocks,
		LastProviderUsage: latestProviderUsage(window.Messages),
	}
}
```

- [ ] **Step 10: Core tests** — in `internal/core/native_run_characterization_test.go`:

Replace the var `compactMarkerPattern` and the function `countCompactMarkers` with (drop the `regexp` import if unused):

```go
func countBoundaries(t *testing.T, db *store.SQLiteStore, sessionID string) int {
	t.Helper()
	count := 0
	for _, message := range sessionMessages(t, db, sessionID) {
		if message.Compaction != nil && !message.Compaction.Cleared {
			count++
		}
	}
	return count
}
```

Replace `TestNativeRunCompactsLargeHistoryBeforeTheModelCall` and `TestContextLengthErrorForcesCompactionAndRetriesOnce` with:

```go
func TestNativeRunCompactsLargeHistoryBeforeTheModelCall(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	big := strings.Repeat("x", 330_000)
	var summaryRequests, mainRequests int
	var mainPrompt, mainFirst string
	var leaked bool
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			summaryRequests++
			return providers.Response{Text: "SUMMARY"}, nil
		}
		mainRequests++
		mainPrompt, mainFirst = request.SystemPrompt, request.Messages[0].Content
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
	if !strings.Contains(mainFirst, "SUMMARY") || strings.Contains(mainPrompt, "SUMMARY") || leaked {
		t.Fatalf("main request: first message %.80q, summary in system prompt=%v, old history leaked=%v", mainFirst, strings.Contains(mainPrompt, "SUMMARY"), leaked)
	}
	if got := countBoundaries(t, db, session.ID); got != 1 {
		t.Fatalf("boundaries = %d, want 1", got)
	}
}

func TestContextLengthErrorForcesCompactionAndRetriesOnce(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var summaryRequests, mainRequests int
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			summaryRequests++
			return providers.Response{Text: "SUMMARY"}, nil
		}
		mainRequests++
		if mainRequests == 1 {
			return providers.Response{}, errors.New("provider: context_length_exceeded")
		}
		return providers.Response{Text: "Recovered."}, nil
	})}})
	old := strings.Repeat("y", 200_000)
	session, run := saveNativeRunWithHistory(t, db, "overflow", transcript.Message{
		ID: "msg_old_user", Role: transcript.MessageRoleUser, Content: old, Parts: transcript.NormalizeMessageParts(old, nil),
	})

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if summaryRequests != 1 || mainRequests != 2 {
		t.Fatalf("summary=%d main=%d, want 1/2", summaryRequests, mainRequests)
	}
	if got := countBoundaries(t, db, session.ID); got != 1 {
		t.Fatalf("boundaries = %d, want 1", got)
	}
}

func TestNativeRunSeesOnlyTheNewestBoundaryAndLaterMessages(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var seen providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		seen = request
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveNativeRunWithHistory(t, db, "window", transcript.Message{
		ID: "msg_forgotten", Role: transcript.MessageRoleUser, Content: "forgotten detail", Parts: transcript.NormalizeMessageParts("forgotten detail", nil),
	})
	forgotten, err := db.GetMessage(context.Background(), "msg_forgotten")
	if err != nil {
		t.Fatal(err)
	}
	saveRunRecoveryTestMessage(t, db, transcript.Message{
		ID: "msg_boundary", SessionID: session.ID, Role: transcript.MessageRoleSystem, Content: "Context compacted.",
		Compaction: &transcript.Compaction{Summary: "EARLIER WORK", CoversThroughSeq: forgotten.Seq},
		CreatedAt:  runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime(),
	})

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	if len(seen.Messages) != 2 || !strings.Contains(seen.Messages[0].Content, "EARLIER WORK") || seen.Messages[1].Content != "original task window" {
		t.Fatalf("request messages = %+v", seen.Messages)
	}
}
```

- [ ] **Step 11: Run the tests**

Run: `go test ./internal/agent/... ./internal/core/ ./internal/store/ ./internal/controlplane/`
Expected: PASS. `grep -rn "🧠 Context compacted\|🧹 Context cleared" --include=*.go internal/ | grep -v _test.go` shows only `internal/store/schema_compaction.go` (the legacy prefixes of the migration). (The TUI still has its own prefix detection until Task 6; new boundaries show there as plain system notes meanwhile.)

- [ ] **Step 12: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git rm internal/agent/context/markers.go internal/agent/context/compact.go
git add internal/agent/context/boundary.go internal/agent/context/boundary_test.go internal/agent/context/summarize.go internal/agent/context/summarize_test.go internal/agent/context/estimate.go internal/agent/context/attachments.go internal/agent/ports.go internal/agent/journal.go internal/agent/budget.go internal/agent/request.go internal/agent/engine.go internal/agent/compact.go internal/agent/compact_test.go internal/agent/engine_test.go internal/agent/agenttest/agenttest.go internal/core/ports.go internal/core/agent_journal.go internal/core/agent_prompts.go internal/core/plan_tools.go internal/core/context_boundary.go internal/core/system_messages.go internal/core/session_context.go internal/core/native_run_characterization_test.go
git commit -m "feat(agent): cut context at a seq boundary with a whole-turn tail

The engine and core read the newest structured boundary and only the
messages after the seq it covers. A summary covers everything before a
tail of the newest whole turns (a quarter of the usable window, half of
that after a context-length error), keeps the run's own user messages and
steers verbatim, and reaches the model as the first message instead of
the system prompt. Summaries that save under a tenth twice in a row stop
further ones; the run no longer keeps all of its own messages past a
summary. Text markers and their parsing are gone.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: TUI renders boundaries from the structured field

**Files:**
- Modify: `clients/terminal/ui/surface/message/message.go` (`Message`, new `ContextBoundary`)
- Modify: `clients/terminal/chat/viewmodel/surface_adapter.go` (`ToSurfaceMessage`)
- Modify: `clients/terminal/ui/surface/chat/compact_summary.go`
- Modify: `clients/terminal/chat/runtime/app_usage.go` (`estimateVisibleContextTokens`)
- Test: `clients/terminal/chat/viewmodel/surface_adapter_test.go`, create `clients/terminal/ui/surface/chat/compact_summary_test.go`, create `clients/terminal/chat/runtime/app_usage_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `clients/terminal/chat/viewmodel/surface_adapter_test.go`:

```go
func TestBoundaryMessagesCarryTheirCompaction(t *testing.T) {
	message := transcript.Message{ID: "b1", Role: transcript.MessageRoleSystem, Content: "Context compacted: ~12k -> ~3.0k tokens.", Compaction: &transcript.Compaction{Summary: "Goal: ship.", TokensBefore: 12_000, TokensAfter: 3_000}}
	surface := ToSurfaceMessage(message)
	if surface.Boundary == nil || surface.Boundary.Summary != "Goal: ship." || surface.Boundary.Cleared || surface.Boundary.TokensBefore != 12_000 {
		t.Fatalf("surface boundary = %+v", surface.Boundary)
	}
	if plain := ToSurfaceMessage(transcript.Message{ID: "s1", Role: transcript.MessageRoleSystem, Content: "🧠 Context compacted: looks like a marker"}); plain.Boundary != nil {
		t.Fatalf("text alone made a boundary: %+v", plain.Boundary)
	}
}
```

`clients/terminal/ui/surface/chat/compact_summary_test.go`:

```go
package chat

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

func TestContextBoundariesAreRecognisedByTheirField(t *testing.T) {
	compacted := &surfacemessage.Message{Role: surfacemessage.System, Boundary: &surfacemessage.ContextBoundary{Summary: "Goal", TokensBefore: 12_000, TokensAfter: 3_000}}
	cleared := &surfacemessage.Message{Role: surfacemessage.System, Boundary: &surfacemessage.ContextBoundary{Cleared: true}}
	plain := &surfacemessage.Message{Role: surfacemessage.System, Parts: []surfacemessage.ContentPart{surfacemessage.TextContent{Text: "🧠 Context compacted"}}}

	if !IsCompactSummaryMessage(compacted) || IsContextClearedMessage(compacted) {
		t.Fatal("compaction not recognised")
	}
	if !IsContextClearedMessage(cleared) || IsCompactSummaryMessage(cleared) {
		t.Fatal("clear not recognised")
	}
	if IsCompactSummaryMessage(plain) || IsContextClearedMessage(plain) {
		t.Fatal("marker text alone is a boundary")
	}
	if got := compactSummaryStats(*compacted.Boundary); got != "(~12k -> ~3.0k tokens)" {
		t.Fatalf("stats = %q", got)
	}
}
```

`clients/terminal/chat/runtime/app_usage_test.go`:

```go
package runtime

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

func TestVisibleContextStartsAtTheLatestBoundary(t *testing.T) {
	text := func(value string) surfacemessage.Message {
		return surfacemessage.Message{Role: surfacemessage.User, Parts: []surfacemessage.ContentPart{surfacemessage.TextContent{Text: value}}}
	}
	messages := []surfacemessage.Message{
		text("forgotten forgotten forgotten forgotten"),
		{Role: surfacemessage.System, Boundary: &surfacemessage.ContextBoundary{Summary: "abcd"}},
		text("abcdefgh"),
	}

	tokens, marker := estimateVisibleContextTokens(messages)

	if tokens != 3 || marker != headerContextMarkerCompact {
		t.Fatalf("tokens = %d marker = %v, want 3 (summary 1 + message 2) after a compaction", tokens, marker)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./clients/terminal/...`
Expected: build failure `unknown field Boundary`.

- [ ] **Step 3: Implement**

In `clients/terminal/ui/surface/message/message.go` add `Boundary *ContextBoundary` as the last field of `Message` and, after the struct:

```go
// ContextBoundary marks a message the model's context starts after: a
// compaction with its summary, or a /clear.
type ContextBoundary struct {
	Summary      string
	Cleared      bool
	TokensBefore int
	TokensAfter  int
}
```

In `clients/terminal/chat/viewmodel/surface_adapter.go`, `ToSurfaceMessage`, right before the final `return out` add:

```go
	if compaction := message.Compaction; compaction != nil {
		out.Boundary = &surfacemessage.ContextBoundary{Summary: compaction.Summary, Cleared: compaction.Cleared, TokensBefore: compaction.TokensBefore, TokensAfter: compaction.TokensAfter}
	}
```

Replace `clients/terminal/ui/surface/chat/compact_summary.go` with:

```go
package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
)

type contextMarkerKind string

const (
	contextMarkerCompact contextMarkerKind = "compact"
	contextMarkerClear   contextMarkerKind = "clear"
)

type CompactSummaryMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	id      string
	kind    contextMarkerKind
	content string
	stats   string
	sty     *surfacestyles.Styles
}

func NewCompactSummaryMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) *CompactSummaryMessageItem {
	return newContextMarkerMessageItem(sty, message, contextMarkerCompact)
}

func NewContextClearedMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message) *CompactSummaryMessageItem {
	return newContextMarkerMessageItem(sty, message, contextMarkerClear)
}

func newContextMarkerMessageItem(sty *surfacestyles.Styles, message *surfacemessage.Message, kind contextMarkerKind) *CompactSummaryMessageItem {
	var boundary surfacemessage.ContextBoundary
	if message.Boundary != nil {
		boundary = *message.Boundary
	}
	return &CompactSummaryMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		id:                       message.ID + ":" + kind.idSuffix(),
		kind:                     kind,
		content:                  strings.TrimSpace(boundary.Summary),
		stats:                    compactSummaryStats(boundary),
		sty:                      sty,
	}
}

func IsCompactSummaryMessage(message *surfacemessage.Message) bool {
	return message != nil && message.Boundary != nil && !message.Boundary.Cleared
}

func IsContextClearedMessage(message *surfacemessage.Message) bool {
	return message != nil && message.Boundary != nil && message.Boundary.Cleared
}

func (c *CompactSummaryMessageItem) ID() string {
	return c.id
}

func (c *CompactSummaryMessageItem) RawRender(width int) string {
	innerWidth := cappedMessageWidth(width)
	content, height, ok := c.getCachedRender(innerWidth)
	if !ok {
		content = c.renderContent(innerWidth)
		height = lipgloss.Height(content)
		c.setCachedRender(content, innerWidth, height)
	}
	return c.renderHighlighted(content, innerWidth, height)
}

func (c *CompactSummaryMessageItem) Render(width int) string {
	return renderUnifiedMessageLines(c.sty, c.RawRender(width), c.focused, c.sty.Chat.Message.ToolMarker)
}

func (c *CompactSummaryMessageItem) HandleKeyEvent(key tea.KeyPressMsg) (bool, tea.Cmd) {
	switch key.String() {
	case "enter", "v":
	default:
		return false, nil
	}
	if c.content == "" {
		return false, nil
	}
	return true, func() tea.Msg {
		return surfacedialog.ActionOpenFilePreview{Data: surfacedialog.FilePreviewData{
			Title:   c.kind.previewTitle(),
			Content: c.content,
		}}
	}
}

func (c *CompactSummaryMessageItem) renderContent(width int) string {
	parts := []string{toolNameStyle(c.sty, false).Render(c.kind.label())}
	if c.stats != "" {
		parts = append(parts, c.sty.Tool.ParamMain.Render(c.stats))
	}
	if c.content != "" {
		parts = append(parts, c.sty.Muted.Render("press enter to view"))
	}
	line := strings.Join(parts, " ")
	if width >= 0 {
		line = ansi.Truncate(line, width, "…")
	}
	return line
}

// compactSummaryStats shows how far a compaction shrank the context.
func compactSummaryStats(boundary surfacemessage.ContextBoundary) string {
	if boundary.Cleared || boundary.TokensBefore <= 0 {
		return ""
	}
	return fmt.Sprintf("(~%s -> ~%s tokens)", agentcontext.FormatShortNumber(boundary.TokensBefore), agentcontext.FormatShortNumber(boundary.TokensAfter))
}

func (k contextMarkerKind) idSuffix() string {
	switch k {
	case contextMarkerClear:
		return "context-clear"
	default:
		return "compact-summary"
	}
}

func (k contextMarkerKind) label() string {
	switch k {
	case contextMarkerClear:
		return "Context cleared"
	default:
		return "Context compacted"
	}
}

func (k contextMarkerKind) previewTitle() string {
	switch k {
	case contextMarkerClear:
		return "Context Clear"
	default:
		return "Context Summary"
	}
}
```

In `clients/terminal/chat/runtime/app_usage.go` delete the constants `headerCompactSummaryPrefix` / `headerContextClearedPrefix` and the functions `headerCompactSummary` / `headerClearSummary`, and replace `estimateVisibleContextTokens` with:

```go
func estimateVisibleContextTokens(messages []surfacemessage.Message) (int, headerContextMarker) {
	for i := len(messages) - 1; i >= 0; i-- {
		boundary := messages[i].Boundary
		if boundary == nil {
			continue
		}
		marker := headerContextMarkerCompact
		if boundary.Cleared {
			marker = headerContextMarkerClear
		}
		return agentcontext.EstimateTextTokens(boundary.Summary) + estimateMessagesTokens(messages[i+1:]), marker
	}
	return estimateMessagesTokens(messages), headerContextMarkerNone
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./clients/terminal/...` and `grep -rn "🧠 Context compacted\|🧹 Context cleared" --include=*.go clients/ | grep -v _test.go` (prints nothing).
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add clients/terminal/ui/surface/message/message.go clients/terminal/chat/viewmodel/surface_adapter.go clients/terminal/chat/viewmodel/surface_adapter_test.go clients/terminal/ui/surface/chat/compact_summary.go clients/terminal/ui/surface/chat/compact_summary_test.go clients/terminal/chat/runtime/app_usage.go clients/terminal/chat/runtime/app_usage_test.go
git commit -m "feat(tui): show context boundaries from their structured field

Compaction and clear markers are recognised by the message's compaction,
not by an emoji prefix; the item shows the token stats it carries and
opens its summary. The header estimate starts after the newest boundary.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Effective window and usage-anchored token accounting

**Files:**
- Modify: `internal/agent/task.go` (`Task.WindowTokens`), `internal/agent/ports.go` (`Prompts` loses `Budget`)
- Create: `internal/agent/fit.go`, `internal/agent/fit_test.go`
- Modify: `internal/agent/request.go` (delete `contextBudget`, `budget`, `autoCompact`, `requestNeedsCompact`; `buildRequest` records the request seq), `internal/agent/compact.go` (`compactHistory`, `writeBoundary`), `internal/agent/engine.go` (`run`, `step`)
- Modify: `internal/agent/context/estimate.go` (delete `Threshold`, `Recommendation`, `minimumCompactThreshold`; add `SummaryPercent`, `SummaryDue`)
- Modify: `internal/agent/agenttest/agenttest.go` (`Prompts`, `Fixture.Window`, `Fixture.Task`)
- Modify: `internal/core/agent_prompts.go` (delete `Budget`), `internal/core/run_execute.go` (`nativeEngine`), `internal/core/session_context.go` (`contextReportForSession`)
- Test: `internal/agent/engine_test.go`, `internal/agent/compact_test.go`

- [ ] **Step 1: Write the failing tests** — create `internal/agent/fit_test.go`:

```go
package agent_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestReportedPromptTokensDecideWhenToSummarise(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}},
		text("SUMMARY"),
		text("Done."),
	)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 3 || !isSummaryRequest(requests[1]) {
		t.Fatalf("outcome = %+v requests = %d, want a summary before the second step", outcome, len(requests))
	}
	marks := boundaries(f.Journal.Messages)
	if len(marks) != 1 || marks[0].Compaction.TokensBefore < 70_000 {
		t.Fatalf("boundaries = %+v, want tokens before from the reported usage", marks)
	}
}

func TestSmallEstimateWithoutReportedUsageDoesNotSummarise(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), text("Done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(model.Requests()) != 2 || len(boundaries(f.Journal.Messages)) != 0 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
}
```

In `internal/agent/engine_test.go` delete `TestContextBudgetIsReadOncePerStep` (the budget port is gone). In `internal/agent/compact_test.go`:
- replace every `f.Prompts.WindowTokens = ` with `f.Window = `;
- replace `TestSummaryKeepsTheAssignmentStepsAndWholeToolSteps`'s first lines up to and including the `model :=` line with:

```go
func TestSummaryKeepsTheAssignmentStepsAndWholeToolSteps(t *testing.T) {
	f := agenttest.NewFixture()
	// A 20k window keeps a tail of ~2.5k tokens; the usage reported for the
	// third step puts the fourth over the summary threshold.
	f.Window = 20_000
	big := strings.Repeat("b", 10_000)
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result { return tools.Result{Content: call.ToolCallID + big} }
	f.Inbox.Steers = []string{"focus on the parser"}
	third := agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("r3", "read")}, Usage: providers.Usage{PromptTokens: 7_000}}}
	model := agenttest.NewScriptedModel(calls(call("r1", "read")), calls(call("r2", "read")), third, text("SUMMARY"), text("Done."))
```

- replace `TestTwoLowYieldSummariesStopSummarising` with:

```go
func TestTwoLowYieldSummariesStopSummarising(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = counterTool()
	full := func(id string) agenttest.Turn {
		return agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call(id, "read")}, Usage: providers.Usage{PromptTokens: 75_000}}}
	}
	model := agenttest.NewScriptedModel(full("c1"), text("SUM1"), full("c2"), text("SUM2"), full("c3"), text("Done."))

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || outcome.Assistant.Content != "Done." || len(requests) != 6 || summaryCount(requests) != 2 {
		t.Fatalf("outcome = %+v requests = %d summaries = %d", outcome, len(requests), summaryCount(requests))
	}
	marks := boundaries(f.Journal.Messages)
	if len(marks) != 2 || len(marks[1].Compaction.Kept) != 1 || marks[1].Compaction.Kept[0] != "User: do the task" {
		t.Fatalf("boundaries = %+v", marks)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/`
Expected: build failure `f.Window undefined`.

- [ ] **Step 3: agentcontext** — in `internal/agent/context/estimate.go` delete `minimumCompactThreshold`, `Threshold` and `Recommendation`, and add:

```go
// SummaryPercent of the usable window is where older history gets summarised.
const SummaryPercent = 80

// SummaryDue reports whether a prompt of tokens has reached the summary
// threshold of a usable window of limit tokens.
func SummaryDue(tokens, limit int) bool {
	return tokens >= limit*SummaryPercent/100
}
```

- [ ] **Step 4: Engine** — `internal/agent/task.go`: add to `Task`, after `Model`:

```go
	// WindowTokens is the model's context window; 0 means unknown.
	WindowTokens int
```

`internal/agent/ports.go`: `Prompts` becomes

```go
// Prompts supplies the core-owned system prompt and custom instructions of
// every request.
type Prompts interface {
	System(ctx context.Context, history []transcript.Message) (system, custom string)
}
```

Create `internal/agent/fit.go`:

```go
package agent

import (
	"context"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// promptAnchor is the prompt size a provider reported for the run's last request
// and the newest seq that request held.
type promptAnchor struct {
	tokens int
	seq    int64
}

// contextLimit is the prompt room of the run's model: its window without the
// output limit the next request sends and a reserve.
func (r *run) contextLimit() int {
	output := r.counters.OutputLimit
	if output == 0 {
		output = int(providers.DefaultMaxOutputTokens)
		if limiter, ok := r.task.Model.(providers.OutputLimiter); ok {
			if current, _ := limiter.OutputLimits(); current > 0 {
				output = int(current)
			}
		}
	}
	return agentcontext.EffectiveWindow(r.task.WindowTokens, output)
}

// promptTokens is the prompt size of request: the provider's report for the last
// request plus an estimate of what was written since, or an estimate of the
// whole request when no report matches the current prefix.
func (r *run) promptTokens(request providers.Request) int {
	if r.anchor == nil {
		return agentcontext.EstimateRequestTokens(request)
	}
	_, messages := r.history.window()
	tokens := r.anchor.tokens
	for _, message := range messages {
		if message.Seq > r.anchor.seq {
			tokens += agentcontext.EstimateMessageTokens([]transcript.Message{message})
		}
	}
	return tokens
}

// anchorUsage takes the prompt size a main generation reported as the authority
// for the next step.
func (r *run) anchorUsage(response providers.Response) {
	r.anchor = nil
	if response.Usage.PromptTokens > 0 {
		r.anchor = &promptAnchor{tokens: int(response.Usage.PromptTokens), seq: r.requestSeq}
	}
}

// fitRequest builds the step's request and first summarises older history when
// the prompt has reached the summary threshold.
func (r *run) fitRequest(ctx context.Context, final StopReason) (providers.Request, error) {
	request, err := r.buildRequest(ctx, final)
	if err != nil {
		return providers.Request{}, err
	}
	tokens := r.promptTokens(request)
	if !agentcontext.SummaryDue(tokens, r.contextLimit()) || r.counters.LowYield >= lowYieldLimit {
		return request, nil
	}
	compacted, err := r.compactHistory(ctx, tokens, agentcontext.TailPercent)
	if err != nil || !compacted {
		return request, err
	}
	return r.buildRequest(ctx, final)
}
```

`internal/agent/compact.go`: replace `compactHistory`'s signature and first lines so it reads the limit from the run, and drop the anchor after writing a boundary:

```go
func (r *run) compactHistory(ctx context.Context, before int, tailPercent int) (bool, error) {
	previous, messages := r.history.window()
	limit := r.contextLimit()
	cut := agentcontext.TailStart(messages, limit*tailPercent/100)
	if cut == 0 {
		return false, nil
	}
	covered := messages[:cut]
	summary, err := agentcontext.Summarize(ctx, summaryModel{r: r}, agentcontext.SummaryInput{
		SessionID:   r.task.SessionID,
		Previous:    agentcontext.SummaryText(previous),
		Messages:    covered,
		ChunkTokens: limit / 2,
	})
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, r.writeBoundary(ctx, previous, covered, summary, before)
}
```

In `writeBoundary`, directly after `r.counters.observeSummary(...)` add `r.anchor = nil`. Remove the now unused `providers` import of `compact.go`.

`internal/agent/request.go`: delete `contextBudget`, `budget`, `autoCompact` and `requestNeedsCompact`. In `buildRequest`, right after `compaction, messages := r.history.window()` add:

```go
	if len(messages) > 0 {
		r.requestSeq = messages[len(messages)-1].Seq
	}
```

`internal/agent/engine.go`: add to the `run` struct

```go
	anchor     *promptAnchor
	requestSeq int64
```

and replace `step` with:

```go
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
	request, err := r.fitRequest(ctx, final)
	if err != nil {
		return failedStep(err)
	}
	r.counters.Steps++
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && agentcontext.IsContextLengthExceeded(err) {
		compacted, compactErr := r.compactHistory(ctx, r.promptTokens(request), agentcontext.TailPercent/2)
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
	if err == nil {
		r.anchorUsage(gen.response)
	}
	if final != "" && errors.Is(err, providers.ErrEmptyResponse) {
		return finalTurn(gen, final)
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
```

`internal/agent/agenttest/agenttest.go`: `Prompts` becomes

```go
// Prompts returns Text as the system prompt.
type Prompts struct {
	Text string
}

func (p *Prompts) System(context.Context, []transcript.Message) (string, string) {
	return p.Text, ""
}
```

(delete its `Budget` method); add `Window int` to `Fixture` with the comment `// Window is the model window of the fixture's task; 0 means unknown.` and make `Task` set it:

```go
// Task returns the fixture's run for model.
func (f *Fixture) Task(model agent.Model) agent.Task {
	return agent.Task{RunID: RunID, SessionID: SessionID, WorkingDir: "/work", Model: model, WindowTokens: f.Window}
}
```

- [ ] **Step 5: Core** — `internal/core/agent_prompts.go`: delete `corePrompts.Budget`. `internal/core/run_execute.go`, `nativeEngine`: add `WindowTokens: c.sessionContextWindowTokens(session),` to the `agent.Task` literal. `internal/core/session_context.go`: replace `contextReportForSession` with:

```go
func (c *Core) contextReportForSession(session Session, window agent.Window) ContextReport {
	report := c.contextReport(session.ID, window)
	report.WindowTokens = c.sessionContextWindowTokens(session)
	limit := agentcontext.EffectiveWindow(report.WindowTokens, int(providers.DefaultMaxOutputTokens))
	if agentcontext.SummaryDue(report.TokenEstimate, limit) {
		report.Compact = ContextCompact{Recommended: true, Reason: fmt.Sprintf("estimated context has reached %d%% of the model's usable window; compact before continuing", agentcontext.SummaryPercent)}
	}
	return report
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/agent/... ./internal/core/ ./internal/controlplane/`
Expected: PASS. `grep -rn "agentcontext.Threshold\|Recommendation(\|minimumCompactThreshold\|Prompts.BaseTokens\|BudgetReads\|contextBudget" --include=*.go internal/ clients/` returns nothing.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/task.go internal/agent/ports.go internal/agent/fit.go internal/agent/fit_test.go internal/agent/request.go internal/agent/compact.go internal/agent/engine.go internal/agent/engine_test.go internal/agent/compact_test.go internal/agent/context/estimate.go internal/agent/agenttest/agenttest.go internal/core/agent_prompts.go internal/core/run_execute.go internal/core/session_context.go
git commit -m "feat(agent): account context by reported usage within the usable window

The prompt size of a step is the provider's report for the last request
plus an estimate of what was written since; after a summary the estimate
decides until the next report. Older history is summarised at 80% of the
usable window: the model window without the output limit and a 5%
reserve. The 80k floor and the per-step context budget port are gone.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Large tool outputs kept in session files

**Files:**
- Modify: `internal/tools/types.go` (`Result.OutputPath`), `internal/transcript/message.go` (`ToolResultPart.OutputPath`), `internal/agent/messages.go` (`ToolResultMessage`)
- Modify: `internal/agent/context/estimate.go` (`LargeOutputTokens`, `HeadTail`), `internal/agent/context/conversation.go` (`providerVisibleToolResultContent` and its callers)
- Create: `internal/core/tool_output.go`, `internal/core/tool_output_test.go`
- Modify: `internal/core/core.go` (`Core.sessionFiles`), `internal/core/tool_call_run.go` (`executeToolWithGrant`), `internal/core/sessions.go` (`DeleteSession`)
- Modify: `internal/daemoncmd/run_helpers.go`, `internal/daemoncmd/run.go`
- Test: `internal/agent/context/estimate_test.go`, `internal/agent/context/conversation_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/agent/context/estimate_test.go`:

```go
func TestHeadTailKeepsBothEndsWithinTheLimit(t *testing.T) {
	text := "HEAD" + strings.Repeat("x", 100_000) + "TAIL"
	got := HeadTail(text, 1_000)
	if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || !strings.Contains(got, "tokens omitted") || EstimateTextTokens(got) > 1_000 {
		t.Fatalf("HeadTail = %d tokens: %.60q ... %.60q", EstimateTextTokens(got), got, got[len(got)-60:])
	}
	if got := HeadTail("short", 1_000); got != "short" {
		t.Fatalf("HeadTail of a short text = %q", got)
	}
}
```

Append to `internal/agent/context/conversation_test.go`:

```go
func TestOversizedResultReachesTheModelAsHeadAndTail(t *testing.T) {
	big := strings.Repeat("r", 200_000)
	history := []transcript.Message{
		{ID: "c1", Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "c1", Name: "browser_snapshot", Input: "{}"}}}},
		{ID: "c1_result", Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: "c1", Name: "browser_snapshot", Content: big}}}},
	}
	messages, err := Conversation(context.Background(), history, nil, "run", false, Identity{})
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %d err = %v", len(messages), err)
	}
	content := messages[1].Content
	if EstimateTextTokens(content) > LargeOutputTokens || !strings.Contains(content, "tokens omitted") || EstimateTextTokens(content) < 4_000 {
		t.Fatalf("tool content = %d tokens", EstimateTextTokens(content))
	}
}
```

Create `internal/core/tool_output_test.go`:

```go
package core_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type outputTool struct {
	spec    tools.Spec
	content string
}

func (t outputTool) Spec() tools.Spec { return t.spec }
func (t outputTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Content: t.content, Status: tools.ResultStatusSuccess}, nil
}

func TestLargeToolOutputIsKeptInASessionFile(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	root := t.TempDir()
	big := "FIRST LINE\n" + strings.Repeat("output line\n", 20_000) + "LAST LINE"
	app.WithSessionFiles(root)
	app.WithTools(tools.NewRegistry(outputTool{spec: recoveryToolSpec("dump", tools.EffectReadOnly), content: big}))
	session, _ := saveCrashRecoveryRun(t, db, "output", core.RunStatusCompleted, false)

	result, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "dump", Args: json.RawMessage(`{}`)})
	if err != nil || result.ToolResultMessage == nil {
		t.Fatalf("ExecuteTool = %+v err = %v", result, err)
	}

	part := result.ToolResultMessage.Parts[0].ToolResult
	if part.OutputPath == "" || !strings.HasPrefix(part.OutputPath, root) || !strings.Contains(part.Content, part.OutputPath) {
		t.Fatalf("result = %.200q path = %q", part.Content, part.OutputPath)
	}
	if !strings.Contains(part.Content, "FIRST LINE") || !strings.Contains(part.Content, "LAST LINE") || agentcontext.EstimateTextTokens(part.Content) > agentcontext.LargeOutputTokens {
		t.Fatalf("result keeps %d tokens without both ends", agentcontext.EstimateTextTokens(part.Content))
	}
	stored, err := os.ReadFile(part.OutputPath)
	if err != nil || string(stored) != big {
		t.Fatalf("stored output: %d bytes err = %v", len(stored), err)
	}
	info, err := os.Stat(part.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", info.Mode().Perm())
	}

	if err := app.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(part.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("output file after session delete: err = %v", err)
	}
}

func TestSmallToolOutputStaysInline(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionFiles(t.TempDir())
	app.WithTools(tools.NewRegistry(outputTool{spec: recoveryToolSpec("dump", tools.EffectReadOnly), content: "small"}))
	session, _ := saveCrashRecoveryRun(t, db, "small_output", core.RunStatusCompleted, false)

	result, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "dump", Args: json.RawMessage(`{}`)})

	if err != nil || result.ToolResultMessage == nil || result.ToolResultMessage.Parts[0].ToolResult.Content != "small" || result.ToolResultMessage.Parts[0].ToolResult.OutputPath != "" {
		t.Fatalf("result = %+v err = %v", result.ToolResultMessage, err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/context/ -run 'HeadTail|Oversized'; go test ./internal/core/ -run 'ToolOutput'`
Expected: build failure `undefined: HeadTail / LargeOutputTokens / app.WithSessionFiles`.

- [ ] **Step 3: Carry the path** — `internal/tools/types.go`, `Result`: add as the last field

```go
	// OutputPath is the file holding the full output when Content was cut.
	OutputPath string `json:"output_path,omitempty"`
```

`internal/transcript/message.go`, `ToolResultPart`: add as the last field

```go
	// OutputPath is the file holding the full output when Content was cut.
	OutputPath string `json:"output_path,omitempty"`
```

`internal/agent/messages.go`, `ToolResultMessage`: add `OutputPath: result.OutputPath,` to the `transcript.ToolResultPart` literal.

- [ ] **Step 4: Head and tail** — in `internal/agent/context/estimate.go` add (imports `unicode`):

```go
// LargeOutputTokens is the size above which a tool result is kept in a file and
// the model gets its head and tail.
const LargeOutputTokens = 8_000

// HeadTail keeps the head and the tail of text within about maxTokens and says
// how much was left out between them.
func HeadTail(text string, maxTokens int) string {
	if EstimateTextTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	keep := maxTokens * 5 / 2
	head := keep * 2 / 3
	tail := keep - head
	omitted := EstimateTextTokens(string(runes[head : len(runes)-tail]))
	return strings.TrimRightFunc(string(runes[:head]), unicode.IsSpace) +
		fmt.Sprintf("\n\n[... ~%s tokens omitted ...]\n\n", FormatShortNumber(omitted)) +
		strings.TrimLeftFunc(string(runes[len(runes)-tail:]), unicode.IsSpace)
}
```

In `internal/agent/context/conversation.go` delete the constant block (`maxProviderToolResultRunes`, `maxProviderBrowserSnapshotRunes`, `providerToolResultTruncationNotice`), `isBrowserSnapshotToolName` and `trimProviderToolResult`, and replace `providerVisibleToolResultContent` with:

```go
// providerVisibleToolResultContent is a result as the model sees it; results
// written before large outputs went to files are cut to their head and tail.
func providerVisibleToolResultContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "(empty result)"
	}
	return HeadTail(content, LargeOutputTokens)
}
```

Update its three callers to the one-argument form: in `formatToolResultAsText` `content = providerVisibleToolResultContent(content)`, in `toProviderMessages` `content = providerVisibleToolResultContent(content)`, and in `estimate.go`'s `EstimateMessageTokens` `EstimateTextTokens(providerVisibleToolResultContent(part.ToolResult.Content))`. Drop imports that became unused (`unicode/utf8` in `conversation.go`).

- [ ] **Step 5: Core** — add `sessionFiles string` to the `Core` struct in `internal/core/core.go` (after `budgets`). Create `internal/core/tool_output.go`:

```go
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// WithSessionFiles sets the directory that holds per-session files such as full
// tool outputs; a session's files are removed with it.
func (c *Core) WithSessionFiles(root string) *Core {
	c.sessionFiles = strings.TrimSpace(root)
	return c
}

// keepLargeOutput writes a result too large for the model's context to a file of
// the session and leaves the model its head and tail with the file's path.
func (c *Core) keepLargeOutput(sessionID string, result tools.Result) tools.Result {
	tokens := agentcontext.EstimateTextTokens(result.Content)
	if tokens <= agentcontext.LargeOutputTokens {
		return result
	}
	excerpt := agentcontext.HeadTail(result.Content, agentcontext.LargeOutputTokens-200)
	path, err := c.writeToolOutput(sessionID, result.Content)
	if err != nil {
		log.Printf("core: keep large tool output of session %q: %v", sessionID, err)
		result.Content = excerpt
		return result
	}
	result.OutputPath = path
	result.Content = fmt.Sprintf("Output is ~%s tokens; the full output is in %s (read it with offset and limit, or grep it).\n\n%s", agentcontext.FormatShortNumber(tokens), path, excerpt)
	return result
}

// writeToolOutput stores content under the session, named by its hash so a
// repeated output lands in the same file.
func (c *Core) writeToolOutput(sessionID string, content string) (string, error) {
	if c.sessionFiles == "" {
		return "", errors.New("no session files directory is configured")
	}
	dir := filepath.Join(c.sessionFiles, sessionDirName(sessionID), "tool-output")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(content))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+".txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// removeSessionFiles deletes the files kept for a session.
func (c *Core) removeSessionFiles(sessionID string) error {
	if c.sessionFiles == "" {
		return nil
	}
	return os.RemoveAll(filepath.Join(c.sessionFiles, sessionDirName(sessionID)))
}

// sessionDirName is a session ID made safe to use as one path element.
func sessionDirName(sessionID string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, sessionID)
}
```

`internal/core/tool_call_run.go`: replace `executeToolWithGrant` with:

```go
func (c *Core) executeToolWithGrant(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput) (tools.Result, error) {
	result, execErr := c.executePreparedTool(ctx, prepared, input.Approved, input.Args, input.Client, input.ExternalKey)
	if result.Approval != nil && !input.Approved {
		autoApproved, err := c.autoApprovesTool(ctx, prepared, result)
		if err != nil {
			return tools.Result{}, err
		}
		if !autoApproved {
			return result, execErr
		}
		result, execErr = c.executePreparedTool(ctx, prepared, true, input.Args, input.Client, input.ExternalKey)
	}
	if execErr != nil || result.Approval != nil {
		return result, execErr
	}
	return c.keepLargeOutput(prepared.SessionID, result), nil
}
```

`internal/core/sessions.go`: replace `DeleteSession` with (add the `log` import if missing):

```go
func (c *Core) DeleteSession(ctx context.Context, sessionID string) error {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
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

- [ ] **Step 6: Daemon wiring** — in `internal/daemoncmd/run_helpers.go` replace `defaultStorageRoot` with:

```go
// dataDir is the directory of the database, which holds the daemon's files.
func dataDir(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = setup.DefaultDBPath()
	}
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}
	return filepath.Dir(dbPath)
}

func defaultStorageRoot(dbPath string) string {
	return filepath.Join(dataDir(dbPath), "storage")
}

// sessionFilesRoot holds per-session files such as full tool outputs.
func sessionFilesRoot(dbPath string) string {
	return filepath.Join(dataDir(dbPath), "sessions")
}
```

In `internal/daemoncmd/run.go` add `WithSessionFiles(sessionFilesRoot(bootstrap.DBPath)).` to the `core.New(sqliteStore).` chain, right after `WithRunBudgets(bootstrap.Budgets).`.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/agent/... ./internal/core/ ./internal/daemoncmd/ ./internal/tools/...`
Expected: PASS.

- [ ] **Step 8: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/tools/types.go internal/transcript/message.go internal/agent/messages.go internal/agent/context/estimate.go internal/agent/context/estimate_test.go internal/agent/context/conversation.go internal/agent/context/conversation_test.go internal/core/core.go internal/core/tool_output.go internal/core/tool_output_test.go internal/core/tool_call_run.go internal/core/sessions.go internal/daemoncmd/run_helpers.go internal/daemoncmd/run.go
git commit -m "feat(core): keep large tool outputs in session files

A tool result over ~8k tokens is written to a file of its session, named
by its hash, and the model gets its head and tail with the path. This
holds for engine runs, run-less tool calls and recovery replays alike;
the files go with the session. Older results are cut to head and tail in
the provider view instead of the 12k-rune truncation.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Stable system prompt and the context message

**Files:**
- Modify: `internal/agent/ports.go` (`Prompts`), `internal/agent/engine.go` (`Run`, `run`, `step`), `internal/agent/request.go` (`buildRequest`), `internal/agent/budget.go` (`Counters`), `internal/agent/compact.go` (`writeBoundary`), `internal/agent/fit.go` (`fitRequest`)
- Create: `internal/agent/context_note.go`, `internal/agent/context_note_test.go`
- Modify: `internal/agent/agenttest/agenttest.go` (`Prompts`, `Tools`)
- Modify: `internal/core/agent_prompts.go`, `internal/core/run_execute.go` (`nativeEngine`)
- Create: `internal/core/context_note_test.go`
- Test: `internal/core/run_crash_recovery_integration_test.go` (`TestRecoverRunningGenerationSkipsPartialAndCompletes`)

- [ ] **Step 1: Write the failing engine tests** — create `internal/agent/context_note_test.go`:

```go
package agent_test

import (
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func contextNotes(messages []transcript.Message) []transcript.Message {
	var notes []transcript.Message
	for _, message := range messages {
		if message.Origin == transcript.OriginEngineModel && strings.HasPrefix(message.Content, "Context update") {
			notes = append(notes, message)
		}
	}
	return notes
}

func mentions(request providers.Request, text string) int {
	count := 0
	for _, message := range request.Messages {
		count += strings.Count(message.Content, text)
	}
	return count
}

func TestSystemPromptAndToolsAreFixedForTheRun(t *testing.T) {
	f := agenttest.NewFixture()
	f.Tools.Funcs["read"] = counterTool()
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), calls(call("c2", "read")), text("Done."))

	run(t, f, model)

	requests := model.Requests()
	if f.Prompts.SystemCalls != 1 || f.Tools.SpecReads != 1 || len(requests) != 3 {
		t.Fatalf("system prompt built %d times, tools listed %d times over %d requests", f.Prompts.SystemCalls, f.Tools.SpecReads, len(requests))
	}
	for _, request := range requests[1:] {
		if request.SystemPrompt != requests[0].SystemPrompt || len(request.Tools) != len(requests[0].Tools) {
			t.Fatalf("request prefix changed: %q / %d tools", request.SystemPrompt, len(request.Tools))
		}
	}
}

func TestContextNoteIsJournaledOnlyWhenItChanges(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Plan: step A"
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		if call.ToolCallID == "c2" {
			f.Prompts.ContextText = "Plan: step B"
		}
		return tools.Result{Content: "read " + call.ToolCallID}
	}
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), calls(call("c2", "read")), text("Done."))

	run(t, f, model)

	notes := contextNotes(f.Journal.Messages)
	if len(notes) != 2 || !strings.Contains(notes[0].Content, "Plan: step A") || !strings.Contains(notes[1].Content, "Plan: step B") || notes[0].RunID != agenttest.RunID {
		t.Fatalf("context notes = %+v", notes)
	}
	requests := model.Requests()
	if last := lastMessage(requests[0]); last.Role != "user" || !strings.Contains(last.Content, "Plan: step A") {
		t.Fatalf("first request ends with %+v", last)
	}
	if mentions(requests[1], "Plan: step") != 1 {
		t.Fatalf("an unchanged context was sent again: %+v", requests[1].Messages)
	}
	if last := lastMessage(requests[2]); !strings.Contains(last.Content, "Plan: step B") {
		t.Fatalf("third request ends with %+v", last)
	}
}

func TestResumedRunDoesNotRepeatAnUnchangedContextNote(t *testing.T) {
	f := agenttest.NewFixture()
	f.Prompts.ContextText = "Recovery notice: the daemon restarted."
	f.Tools.Funcs["read"] = readTool
	run(t, f, agenttest.NewScriptedModel(calls(call("c1", "read")), text("First.")))
	task := f.Task(agenttest.NewScriptedModel(text("Second.")))
	task.Resume = f.Journal.States[len(f.Journal.States)-1].Counters

	runTask(t, f, task)

	if notes := contextNotes(f.Journal.Messages); len(notes) != 1 {
		t.Fatalf("context notes = %d, want 1", len(notes))
	}
}

func TestContextNoteIsSentAgainWhenASummaryCoversIt(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 20_000
	f.Prompts.ContextText = "Plan: parser"
	big := strings.Repeat("b", 10_000)
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result { return tools.Result{Content: call.ToolCallID + big} }
	model := agenttest.NewScriptedModel(append(toolSteps(4, "read"), text("SUMMARY"), text("Done."))...)

	run(t, f, model)

	requests := model.Requests()
	if len(requests) != 6 || !isSummaryRequest(requests[4]) {
		t.Fatalf("requests = %d", len(requests))
	}
	last := requests[5]
	if notes := contextNotes(f.Journal.Messages); len(notes) != 2 || mentions(last, "Plan: parser") != 1 || !strings.Contains(lastMessage(last).Content, "Plan: parser") {
		t.Fatalf("notes = %d, last request = %+v", len(notes), last.Messages)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/ -run 'Fixed|ContextNote'`
Expected: build failure `f.Prompts.SystemCalls undefined` / `ContextText undefined`.

- [ ] **Step 3: Engine** — `internal/agent/ports.go`: `Prompts` becomes

```go
// Prompts supplies the core-owned parts of every request: the system prompt and
// custom instructions, fixed for a run, and the changing state the engine sends
// as a context message whenever it changes.
type Prompts interface {
	System(ctx context.Context, history []transcript.Message) (system, custom string)
	Context(ctx context.Context) string
}
```

`internal/agent/budget.go`, `Counters`: add after `LowYield`

```go
	ContextHash   string        `json:"context_hash,omitempty"`
	ContextSeq    int64         `json:"context_seq,omitempty"`
```

Create `internal/agent/context_note.go`:

```go
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// syncContext sends the state the system prompt leaves out as a context note
// whenever it differs from what the run last sent.
func (r *run) syncContext(ctx context.Context) error {
	text := strings.TrimSpace(r.Prompts.Context(ctx))
	hash := contextHash(text)
	if hash == r.counters.ContextHash {
		return nil
	}
	if err := r.appendEngineMessage(ctx, transcript.OriginEngineModel, contextNoteText(text)); err != nil {
		return err
	}
	messages := r.history.all()
	r.counters.ContextHash, r.counters.ContextSeq = hash, messages[len(messages)-1].Seq
	return nil
}

func contextHash(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func contextNoteText(text string) string {
	if text == "" {
		return "Context update: nothing from the earlier context updates applies any more."
	}
	return "Context update (the current state; it replaces earlier context updates):\n\n" + text
}
```

`internal/agent/compact.go`, `writeBoundary`: directly after `r.anchor = nil` add

```go
	if r.counters.ContextSeq <= compaction.CoversThroughSeq {
		r.counters.ContextHash = ""
	}
```

`internal/agent/fit.go`, `fitRequest`: replace everything from `compacted, err := r.compactHistory(…)` to the end of the function with

```go
	compacted, err := r.compactHistory(ctx, tokens, agentcontext.TailPercent)
	if err != nil || !compacted {
		return request, err
	}
	if final == "" {
		if err := r.syncContext(ctx); err != nil {
			return providers.Request{}, err
		}
	}
	return r.buildRequest(ctx, final)
```

`internal/agent/engine.go`: add to the `run` struct

```go
	system, custom string
	tools          []providers.ToolDefinition
```

In `Run`, replace the line `r := &run{…}` with:

```go
	r := &run{Config: e.cfg, task: task, history: newHistory(e.cfg.Journal, e.cfg.Sink, window), counters: task.Resume, started: e.cfg.Now()}
	r.system, r.custom = e.cfg.Prompts.System(ctx, window.Messages)
	if ToolUseAllowed(task.Model) {
		r.tools = toolDefinitions(e.cfg.Tools.Specs(ctx))
	}
```

In `step`, directly before `final, err := r.prepareStep(ctx)` insert:

```go
	if err := r.syncContext(ctx); err != nil {
		return failedStep(err)
	}
```

`internal/agent/request.go`, `buildRequest`: delete the line `system, custom := r.Prompts.System(ctx, messages)`, use `SystemPrompt: r.system, CustomInstructions: r.custom,` in the request literal, and replace `request.Tools = toolDefinitions(r.Tools.Specs(ctx))` with `request.Tools = r.tools`. Update its doc comment's first line to "buildRequest assembles the step's request from the run's fixed prompt and tools:".

`internal/agent/agenttest/agenttest.go`: `Prompts` becomes

```go
// Prompts returns Text as the system prompt and ContextText as the changing
// context; SystemCalls counts system prompt builds.
type Prompts struct {
	Text        string
	ContextText string
	SystemCalls int
}

func (p *Prompts) System(context.Context, []transcript.Message) (string, string) {
	p.SystemCalls++
	return p.Text, ""
}

func (p *Prompts) Context(context.Context) string {
	return p.ContextText
}
```

and `Tools` gains `SpecReads int` (doc: "SpecReads counts tool listings."), incremented as the first statement of `Specs`.

- [ ] **Step 4: Run the engine tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 5: Write the failing core test** — create `internal/core/context_note_test.go`:

```go
package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type funcTool struct {
	spec tools.Spec
	run  func(context.Context, tools.Call) tools.Result
}

func (t funcTool) Spec() tools.Spec { return t.spec }
func (t funcTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	return t.run(ctx, call), nil
}

func TestMemoryWrittenDuringARunReachesTheModelAsAContextNote(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := app.CreateMemory(ctx, core.MemoryEntry{Scope: core.MemoryScopeGlobal, Content: "prefers tabs"}); err != nil {
		t.Fatal(err)
	}
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("remember", tools.EffectReadOnly), run: func(ctx context.Context, _ tools.Call) tools.Result {
		if _, err := app.CreateMemory(ctx, core.MemoryEntry{Scope: core.MemoryScopeGlobal, Content: "uses Go 1.26"}); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}
		}
		return tools.Result{Content: "remembered"}
	}}))
	var requests []providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		if len(requests) == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call_remember", Name: "remember", Arguments: json.RawMessage(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "memory", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	if len(requests) != 2 || requests[0].SystemPrompt != requests[1].SystemPrompt || !strings.Contains(requests[0].SystemPrompt, "prefers tabs") || strings.Contains(requests[1].SystemPrompt, "uses Go 1.26") {
		t.Fatalf("system prompts changed during the run or carry the new memory (%d requests)", len(requests))
	}
	last := requests[1].Messages[len(requests[1].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Memory changed during this run") || !strings.Contains(last.Content, "uses Go 1.26") {
		t.Fatalf("second request ends with %+v", last)
	}
}
```

In `internal/core/run_crash_recovery_integration_test.go`, `TestRecoverRunningGenerationSkipsPartialAndCompletes`, replace the block from `request := runtime.lastRequest()` through the closing brace of the `for _, message := range request.Messages` loop with:

```go
	request := runtime.lastRequest()
	notice := false
	for _, message := range request.Messages {
		notice = notice || strings.Contains(message.Content, "daemon restarted")
		if strings.Contains(message.Content, "partial answer") {
			t.Fatalf("interrupted partial assistant leaked into provider retry: %#v", request.Messages)
		}
	}
	if !notice || strings.Contains(request.SystemPrompt, "daemon restarted") {
		t.Fatalf("recovery notice is not a context message: system prompt %q", request.SystemPrompt)
	}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/core/ -run 'TestMemoryWrittenDuringARun|TestRecoverRunningGenerationSkipsPartial'`
Expected: build failure `corePrompts does not implement agent.Prompts (missing method Context)`.

- [ ] **Step 7: Core** — in `internal/core/agent_prompts.go` replace `corePrompts`, its `System`, and `nativeSystemPrompt` with:

```go
// corePrompts is the Prompts port of one native run; memory is the memory text
// the run's system prompt was built with.
type corePrompts struct {
	c      *Core
	turn   nativeTurn
	memory string
}

func (p *corePrompts) System(ctx context.Context, history []transcript.Message) (string, string) {
	assistant := p.c.assistantProfile()
	if !p.turn.Subagent {
		p.memory = p.c.MemoryPromptContext(ctx, p.turn.WorkingDir)
	}
	return p.c.nativeSystemPrompt(ctx, p.turn, assistant, p.memory, history), assistant.CustomInstructions
}

// Context is what changes during a run: the recovery notice, runtime status,
// memory written since the run started and the session plan.
func (p *corePrompts) Context(ctx context.Context) string {
	var sections []string
	if checkpoint, ok, err := p.c.runCheckpoint(ctx, p.turn.RunID); err == nil && ok {
		sections = append(sections, runCheckpointRecoveryPrompt(checkpoint))
	}
	if p.turn.Subagent {
		return prompt.JoinSections(sections...)
	}
	sections = append(sections, p.c.nativeStatusPrompt(ctx, p.turn))
	if memory := p.c.MemoryPromptContext(ctx, p.turn.WorkingDir); memory != p.memory {
		sections = append(sections, memoryChangedPrompt(memory))
	}
	sections = append(sections, p.c.sessionPlanPrompt(ctx, p.turn.SessionID))
	return prompt.JoinSections(sections...)
}

func memoryChangedPrompt(memory string) string {
	if memory == "" {
		return "Memory changed during this run: every entry was removed."
	}
	return "Memory changed during this run; it now is:\n" + memory
}

// nativeSystemPrompt is the part of the prompt that stays fixed for a run.
func (c *Core) nativeSystemPrompt(ctx context.Context, turn nativeTurn, assistant AssistantProfile, memory string, history []transcript.Message) string {
	sections := []string{prompt.AssistantSystemPrompt(assistant.Name, assistant.SystemPrompt)}
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
	if c.delegateTaskPromptAvailable() {
		sections = append(sections, c.delegateTaskGuidancePrompt(ctx))
	}
	sections = append(sections, memory)
	if skillsPrompt := c.nativeSkillsPrompt(ctx, turn, history); skillsPrompt != "" {
		sections = append(sections, skillsPrompt)
	}
	return prompt.JoinSections(sections...)
}
```

In `internal/core/run_execute.go`, `nativeEngine`, change `Prompts: corePrompts{c: c, turn: turn},` to `Prompts: &corePrompts{c: c, turn: turn},`.

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/agent/... ./internal/core/`
Expected: PASS. If a pre-existing core test counts messages and now sees one `engine_model` context note more (runs with a recovery notice or a plan), update that count and report it.

- [ ] **Step 9: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/ports.go internal/agent/budget.go internal/agent/context_note.go internal/agent/context_note_test.go internal/agent/compact.go internal/agent/fit.go internal/agent/engine.go internal/agent/request.go internal/agent/agenttest/agenttest.go internal/core/agent_prompts.go internal/core/run_execute.go internal/core/context_note_test.go internal/core/run_crash_recovery_integration_test.go
git commit -m "feat(agent): keep the system prompt fixed and journal changing context

The system prompt and tool definitions are built once per run: identity,
guidance, project root, memory and skills as of the run's start. The
recovery notice, runtime status, memory written during the run and the
session plan reach the model as a context note that is journaled only
when it changes, and again after a summary covers it, so the request
prefix stays byte-stable between steps.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Level-1 elision at 60%

**Files:**
- Create: `internal/agent/context/elide.go`, `internal/agent/context/elide_test.go`
- Modify: `internal/agent/budget.go` (`Counters`), `internal/agent/fit.go` (`fitRequest`, new `elision`, `advanceElision`), `internal/agent/request.go` (`buildRequest`)
- Test: `internal/agent/fit_test.go`

- [ ] **Step 1: Write the failing tests** — create `internal/agent/context/elide_test.go`:

```go
package agentcontext

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestNextElisionKeepsTheLastFiveRoundsAndThreeReplies(t *testing.T) {
	messages := []transcript.Message{textMessage(1, transcript.MessageRoleUser, "r1", "task")}
	var rounds []int64
	for i, seq := 0, int64(2); i < 7; i, seq = i+1, seq+3 {
		rounds = append(rounds, seq)
		messages = append(messages, toolStep(seq, fmt.Sprintf("c%d", i), "out")...)
	}

	if got, want := NextElision(messages), (Elision{ResultsThroughSeq: rounds[2] - 1, ImagesThroughSeq: rounds[4] - 1}); got != want {
		t.Fatalf("NextElision = %+v, want %+v", got, want)
	}
	if got := NextElision(messages[:16]); got.ResultsThroughSeq != 0 {
		t.Fatalf("five rounds elide results through %d, want nothing", got.ResultsThroughSeq)
	}
}

func TestElideHidesOldBulkyResultsAndImagesOnly(t *testing.T) {
	big := strings.Repeat("x", 8_000)
	result := func(seq int64, callID, content, path string) transcript.Message {
		return transcript.Message{ID: callID + "_result", Seq: seq, Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: callID, Name: "read", Content: content, OutputPath: path}}}}
	}
	messages := []transcript.Message{
		{ID: "c1", Seq: 2, Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "c1", Name: "read", Input: `{"path":"a.go","limit":20}`}}}},
		result(3, "c1", big, "/data/s1/out.txt"),
		result(4, "c2", "tiny", ""),
		{ID: "img", Seq: 5, Role: transcript.MessageRoleUser, Content: "look", Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindImage, Image: &transcript.ImagePart{Name: "shot.png", MIMEType: "image/png", StoragePath: "images/shot.png"}}}},
		result(9, "c3", big, ""),
	}

	out := Elide(messages, Elision{ResultsThroughSeq: 4, ImagesThroughSeq: 5})

	want := `[output of read(limit=20, path="a.go") hidden, ~2.0k tokens; call again or read /data/s1/out.txt if needed]`
	if got := out[1].Parts[0].ToolResult.Content; got != want {
		t.Fatalf("elided result = %q, want %q", got, want)
	}
	if out[2].Parts[0].ToolResult.Content != "tiny" || out[4].Parts[0].ToolResult.Content != big {
		t.Fatal("a small or newer result was elided")
	}
	if len(out[3].Parts) != 0 || !strings.Contains(out[3].Content, "look") || !strings.Contains(out[3].Content, "[image shot.png") {
		t.Fatalf("elided image message = %+v", out[3])
	}
	if messages[1].Parts[0].ToolResult.Content != big || len(messages[3].Parts) != 1 {
		t.Fatal("Elide changed the history it was given")
	}
}
```

Append to `internal/agent/fit_test.go` (add `"strings"`, `"github.com/Suren878/matrixclaw/internal/tools"` imports):

```go
func TestOldBulkyResultsAreElidedAtSixtyPercentAndStayElided(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = func(call tools.Call) tools.Result {
		return tools.Result{Content: call.ToolCallID + " " + strings.Repeat("x", 20_000)}
	}
	model := agenttest.NewScriptedModel(append(toolSteps(11, "read"), text("Done."))...)

	outcome := run(t, f, model)

	requests := model.Requests()
	if outcome.Status != agent.StatusCompleted || len(requests) != 12 {
		t.Fatalf("outcome = %+v requests = %d, want no summary", outcome, len(requests))
	}
	eleventh, twelfth := requests[10], requests[11]
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("read%d", i)
		if got := toolContent(eleventh, id); !strings.HasPrefix(got, "[output of read() hidden") || toolContent(twelfth, id) != got {
			t.Fatalf("%s in steps 11/12 = %.60q / %.60q", id, got, toolContent(twelfth, id))
		}
	}
	if got := toolContent(eleventh, "read6"); !strings.HasPrefix(got, "read6 x") {
		t.Fatalf("read6 = %.40q, want it in full", got)
	}
	if stored, _ := f.Journal.Result("read1"); !strings.HasPrefix(stored.Content, "read1 x") {
		t.Fatalf("journal result = %.40q, want the full output kept", stored.Content)
	}
	if last := f.Journal.States[len(f.Journal.States)-1].Counters; last.ElidedResults == 0 {
		t.Fatalf("counters = %+v, want the elision checkpointed", last)
	}
}
```

(`fmt` is already imported by other `agent_test` files; add it to `fit_test.go`'s imports too.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/context/ -run 'Elision|Elide'; go test ./internal/agent/ -run TestOldBulkyResults`
Expected: build failure `undefined: NextElision / Elide / ElidedResults`.

- [ ] **Step 3: agentcontext** — create `internal/agent/context/elide.go`:

```go
package agentcontext

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ElisionPercent of the usable window is where old bulky results get elided.
const ElisionPercent = 60

const (
	elideKeepRounds  = 5
	elideKeepReplies = 3
	elideMinTokens   = 1_000
	argsSummaryRunes = 80
)

// ElisionDue reports whether a prompt of tokens has reached the elision
// threshold of a usable window of limit tokens.
func ElisionDue(tokens, limit int) bool {
	return tokens >= limit*ElisionPercent/100
}

// Elision hides the tool results and images of messages up to these seqs from a
// request; the transcript keeps them.
type Elision struct {
	ResultsThroughSeq int64
	ImagesThroughSeq  int64
}

// NextElision covers the results before the last five tool rounds and the
// images before the last three replies of messages.
func NextElision(messages []transcript.Message) Elision {
	var rounds, replies []int64
	for _, message := range messages {
		if message.Role != transcript.MessageRoleAssistant || len(messageToolCallIDs(message)) > 0 {
			continue
		}
		replies = append(replies, message.Seq)
		if isToolStepReply(message) {
			rounds = append(rounds, message.Seq)
		}
	}
	var elision Elision
	if len(rounds) > elideKeepRounds {
		elision.ResultsThroughSeq = rounds[len(rounds)-elideKeepRounds] - 1
	}
	if len(replies) > elideKeepReplies {
		elision.ImagesThroughSeq = replies[len(replies)-elideKeepReplies] - 1
	}
	return elision
}

// Elide returns messages with the covered results over ~1k tokens and the
// covered images replaced by short notes; messages itself is not changed.
func Elide(messages []transcript.Message, elision Elision) []transcript.Message {
	if elision.ResultsThroughSeq == 0 && elision.ImagesThroughSeq == 0 {
		return messages
	}
	calls := map[string]transcript.ToolCallPart{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolCall != nil {
				calls[strings.TrimSpace(part.ToolCall.ID)] = *part.ToolCall
			}
		}
	}
	out := make([]transcript.Message, len(messages))
	for i, message := range messages {
		if message.Seq <= elision.ResultsThroughSeq && message.Role == transcript.MessageRoleTool {
			message = elideResults(message, calls)
		}
		if message.Seq <= elision.ImagesThroughSeq {
			message = elideImages(message)
		}
		out[i] = message
	}
	return out
}

func elideResults(message transcript.Message, calls map[string]transcript.ToolCallPart) transcript.Message {
	parts := slices.Clone(message.Parts)
	for i, part := range parts {
		if part.ToolResult == nil {
			continue
		}
		tokens := EstimateTextTokens(part.ToolResult.Content)
		if tokens <= elideMinTokens {
			continue
		}
		result := *part.ToolResult
		result.Content = elidedResultNote(calls[strings.TrimSpace(result.ToolCallID)], result, tokens)
		parts[i].ToolResult = &result
	}
	message.Parts = parts
	return message
}

func elidedResultNote(call transcript.ToolCallPart, result transcript.ToolResultPart, tokens int) string {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = strings.TrimSpace(result.Name)
	}
	note := fmt.Sprintf("[output of %s(%s) hidden, ~%s tokens; call again", name, argsSummary(call.Input), FormatShortNumber(tokens))
	if result.OutputPath != "" {
		note += " or read " + result.OutputPath
	}
	return note + " if needed]"
}

// argsSummary renders call arguments as short, sorted key=value pairs.
func argsSummary(input string) string {
	var args map[string]any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&args); err != nil {
		return trimRunes(strings.TrimSpace(input), argsSummaryRunes)
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		value, _ := json.Marshal(args[key])
		pairs = append(pairs, key+"="+string(value))
	}
	return trimRunes(strings.Join(pairs, ", "), argsSummaryRunes)
}

func trimRunes(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}

func elideImages(message transcript.Message) transcript.Message {
	parts := make([]transcript.MessagePart, 0, len(message.Parts))
	var notes []string
	for _, part := range message.Parts {
		if part.Image != nil {
			notes = append(notes, "[image "+imagePartLabel(*part.Image)+" hidden to save context]")
			continue
		}
		parts = append(parts, part)
	}
	if len(notes) == 0 {
		return message
	}
	message.Parts = parts
	message.Content = strings.TrimSpace(strings.Join(append([]string{message.Content}, notes...), "\n\n"))
	return message
}
```

- [ ] **Step 4: Engine** — `internal/agent/budget.go`, `Counters`: add after `ContextSeq`

```go
	ElidedResults int64         `json:"elided_results,omitempty"`
	ElidedImages  int64         `json:"elided_images,omitempty"`
```

`internal/agent/fit.go`: add

```go
// elision is what the run currently hides from its requests.
func (r *run) elision() agentcontext.Elision {
	return agentcontext.Elision{ResultsThroughSeq: r.counters.ElidedResults, ImagesThroughSeq: r.counters.ElidedImages}
}

// advanceElision moves the elision up to the history's current rounds and
// reports whether it moved; the request prefix changes only then.
func (r *run) advanceElision() bool {
	_, messages := r.history.window()
	next := agentcontext.NextElision(messages)
	if next.ResultsThroughSeq <= r.counters.ElidedResults && next.ImagesThroughSeq <= r.counters.ElidedImages {
		return false
	}
	r.counters.ElidedResults = max(r.counters.ElidedResults, next.ResultsThroughSeq)
	r.counters.ElidedImages = max(r.counters.ElidedImages, next.ImagesThroughSeq)
	r.anchor = nil
	return true
}
```

and replace `fitRequest` with:

```go
// fitRequest builds the step's request within the model's window: old bulky
// results are elided at 60% of it, older history is summarised at 80%.
func (r *run) fitRequest(ctx context.Context, final StopReason) (providers.Request, error) {
	request, err := r.buildRequest(ctx, final)
	if err != nil {
		return providers.Request{}, err
	}
	limit := r.contextLimit()
	tokens := r.promptTokens(request)
	if agentcontext.ElisionDue(tokens, limit) && r.advanceElision() {
		if request, err = r.buildRequest(ctx, final); err != nil {
			return providers.Request{}, err
		}
		tokens = agentcontext.EstimateRequestTokens(request)
	}
	if !agentcontext.SummaryDue(tokens, limit) || r.counters.LowYield >= lowYieldLimit {
		return request, nil
	}
	compacted, err := r.compactHistory(ctx, tokens, agentcontext.TailPercent)
	if err != nil || !compacted {
		return request, err
	}
	if final == "" {
		if err := r.syncContext(ctx); err != nil {
			return providers.Request{}, err
		}
	}
	return r.buildRequest(ctx, final)
}
```

`internal/agent/request.go`, `buildRequest`: right after the `r.requestSeq` block add

```go
	messages = agentcontext.Elide(messages, r.elision())
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 6: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/context/elide.go internal/agent/context/elide_test.go internal/agent/budget.go internal/agent/fit.go internal/agent/fit_test.go internal/agent/request.go
git commit -m "feat(agent): elide old bulky tool results at 60% of the window

Once a prompt reaches 60% of the usable window, results over ~1k tokens
older than the last five tool rounds and images older than the last three
replies are replaced in the request by a short note naming the call and,
for kept outputs, the file. The elision moves only at the threshold and
is checkpointed, so the request prefix changes rarely; the transcript
keeps everything.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Cache-friendly summary, exhaustion and overflow recovery

**Files:**
- Modify: `internal/agent/context/summarize.go` (`SummaryInstruction`, exported `SummaryReply`)
- Modify: `internal/agent/task.go` (`StopContextExhausted`, `ErrContextExhausted`), `internal/agent/budget.go` (`finalTurn`, `finalFallback`), `internal/agent/engine.go` (`step`, `settle`), `internal/agent/compact.go` (`compactHistory`, new `summary`, `prefixSummary`), `internal/agent/fit.go` (`fitRequest`, new `exhaustedTurn`)
- Modify: `internal/core/run_outcome.go` (`applyOutcome`)
- Test: `internal/agent/compact_test.go`, `internal/agent/fit_test.go`, `internal/core/native_run_characterization_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/agent/compact_test.go` replace `isSummaryRequest` (add the `agentcontext` import) and replace `TestTwoLowYieldSummariesStopSummarising` with `TestLowYieldSummariesEndTheRunAsContextExhausted`:

```go
func isSummaryRequest(request providers.Request) bool {
	if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
		return true
	}
	last := lastMessage(request)
	return last.Role == "user" && last.Content == agentcontext.SummaryInstruction
}
```

```go
func TestLowYieldSummariesEndTheRunAsContextExhausted(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = counterTool()
	full := func(id string) agenttest.Turn {
		return agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call(id, "read")}, Usage: providers.Usage{PromptTokens: 75_000}}}
	}
	reply := "Stopped: the context is full; the parser is half done."
	model := agenttest.NewScriptedModel(full("c1"), text("SUM1"), full("c2"), text("SUM2"), full("c3"), text(reply))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || outcome.StopReason != agent.StopContextExhausted || !errors.Is(outcome.Err, agent.ErrContextExhausted) || !outcome.MarkErrored {
		t.Fatalf("outcome = %+v", outcome)
	}
	requests := model.Requests()
	if outcome.Assistant == nil || outcome.Assistant.Content != reply || len(requests) != 6 || summaryCount(requests) != 2 {
		t.Fatalf("assistant = %+v requests = %d summaries = %d", outcome.Assistant, len(requests), summaryCount(requests))
	}
	if final := requests[5]; final.ToolChoice != providers.ToolChoiceNone || !strings.Contains(lastMessage(final).Content, "no longer fits") {
		t.Fatalf("final turn = %q / %+v", final.ToolChoice, lastMessage(final))
	}
}
```

Append to `internal/agent/fit_test.go` (imports: `errors`, `agentcontext`):

```go
func TestSummaryReusesTheStepsRequestWithToolsDisabled(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	model := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}},
		text("SUMMARY"),
		text("Done."),
	)

	run(t, f, model)

	requests := model.Requests()
	summary := requests[1]
	if summary.SystemPrompt != requests[0].SystemPrompt || len(summary.Tools) == 0 || len(summary.Tools) != len(requests[0].Tools) || summary.ToolChoice != providers.ToolChoiceNone || summary.CacheKey != agenttest.SessionID {
		t.Fatalf("summary request does not reuse the step's prefix: %+v", summary)
	}
	if summary.Messages[0].Content != "do the task" || lastMessage(summary).Content != agentcontext.SummaryInstruction {
		t.Fatalf("summary messages = %+v", summary.Messages)
	}
	if first := requests[2].Messages[0]; !strings.Contains(first.Content, "SUMMARY") {
		t.Fatalf("next request starts with %+v", first)
	}
}

func TestRequestOverTheWindowIsSummarisedOnItsOwn(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(400_000)...)
	f.Window = 100_000
	model := agenttest.NewScriptedModel(text("SUMMARY"), text("Done."))

	run(t, f, model)

	if summary := model.Requests()[0]; !strings.HasPrefix(summary.SystemPrompt, "You compact matrixclaw chat histories") || len(summary.Tools) != 0 {
		t.Fatalf("summary request = %q with %d tools, want a standalone summary", summary.SystemPrompt, len(summary.Tools))
	}
}

func TestSecondOverflowExhaustsTheContext(t *testing.T) {
	f := agenttest.NewFixture()
	f.WithHistory(pastTurn(80_000)...)
	overflow := agenttest.Turn{Err: errors.New("context_length_exceeded")}
	model := agenttest.NewScriptedModel(overflow, text("SUMMARY"), overflow)

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || outcome.StopReason != agent.StopContextExhausted || !errors.Is(outcome.Err, agent.ErrContextExhausted) || len(model.Requests()) != 3 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
}

func TestOverflowWithNothingToSummariseExhaustsTheContext(t *testing.T) {
	f := agenttest.NewFixture()
	model := agenttest.NewScriptedModel(agenttest.Turn{Err: errors.New("context_length_exceeded")})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusFailed || outcome.StopReason != agent.StopContextExhausted || len(model.Requests()) != 1 {
		t.Fatalf("outcome = %+v requests = %d", outcome, len(model.Requests()))
	}
}
```

Append to `internal/core/native_run_characterization_test.go` (import `agent` if missing):

```go
func TestContextOverflowWithNothingToSummariseFailsAsContextExhausted(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{}, errors.New("provider: context_length_exceeded")
	})})
	_, run := saveCrashRecoveryRun(t, db, "exhausted", core.RunStatusAccepted, false)

	err := app.ExecuteRun(context.Background(), run.ID)

	stored, getErr := db.GetRun(context.Background(), run.ID)
	if !errors.Is(err, agent.ErrContextExhausted) || getErr != nil || stored.Status != core.RunStatusFailed || stored.StopReason != agent.StopContextExhausted {
		t.Fatalf("ExecuteRun err = %v, run = %+v (%v)", err, stored, getErr)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/ ./internal/core/ -run 'ContextExhausted|Overflow|Reuses|OnItsOwn'`
Expected: build failure `undefined: agent.StopContextExhausted / agent.ErrContextExhausted / agentcontext.SummaryInstruction`.

- [ ] **Step 3: agentcontext** — in `internal/agent/context/summarize.go` rename `summaryReply` to `SummaryReply` (doc: "SummaryReply is the text of a summary reply; a cut, filtered or empty one fails."), update its call in `generateSummary`, and add:

```go
// SummaryInstruction ends the run's own request when it asks for the summary
// that replaces the older part of the conversation.
const SummaryInstruction = "Summarise the conversation so far for yourself: the older messages will be replaced by this summary, the most recent ones stay visible. Do not call tools. Use these sections: Goal, Decisions, Files changed, Errors and fixes, Current state, Next step. Be concise and factual; leave out raw tool output, secrets and long code blocks."
```

- [ ] **Step 4: Engine** — `internal/agent/task.go` (import `errors`): add to the `StopReason` constants

```go
	StopContextExhausted StopReason = "context_exhausted"
```

and after them

```go
// ErrContextExhausted fails a run whose conversation no longer fits the model's
// window even after summarising it.
var ErrContextExhausted = errors.New("context_exhausted: the conversation no longer fits the model's context window")
```

`internal/agent/budget.go`: replace `finalTurn` and `finalFallback` with

```go
// finalTurn completes the run with the final turn's text; tool calls a provider
// sent despite tool_choice none are dropped. A context-exhausted final turn
// fails the run with its reply kept.
func finalTurn(gen generation, reason StopReason) stepResult {
	response := gen.response
	response.ToolCalls = nil
	response.Text = sanitizeAssistantOutput(response.Text)
	if response.Text == "" {
		response.Text = finalFallback(reason)
	}
	result := stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: response, stop: reason}
	if reason == StopContextExhausted {
		reply := finalReply(gen.assistant, response)
		result.assistant, result.err, result.markErrored = &reply, ErrContextExhausted, true
	}
	return result
}

// finalFallback is the reply of a final turn that produced no text.
func finalFallback(reason StopReason) string {
	switch reason {
	case StopLoopDetected:
		return "This run stopped: it repeated the same action without progress."
	case StopContextExhausted:
		return "This run stopped: its conversation no longer fits the model's context window."
	default:
		return "This run stopped: it reached its budget."
	}
}
```

`internal/agent/compact.go` (imports `slices`, `providers`): replace `compactHistory` and add `summary` and `prefixSummary`:

```go
// compactHistory summarises the history before the newest turns that fit in
// tailPercent of the window into a boundary; reuse, when set, is the step's
// request to ask for the summary with. False means nothing could be summarised.
func (r *run) compactHistory(ctx context.Context, reuse *providers.Request, before int, tailPercent int) (bool, error) {
	previous, messages := r.history.window()
	limit := r.contextLimit()
	cut := agentcontext.TailStart(messages, limit*tailPercent/100)
	if cut == 0 {
		return false, nil
	}
	covered := messages[:cut]
	summary, err := r.summary(ctx, reuse, previous, covered, limit)
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, r.writeBoundary(ctx, previous, covered, summary, before)
}

// summary asks for the summary with the step's own request, so the provider
// reuses its cached prefix; without one, or when that request is too long, the
// covered history is summarised on its own, in chunks.
func (r *run) summary(ctx context.Context, reuse *providers.Request, previous *transcript.Compaction, covered []transcript.Message, limit int) (string, error) {
	if reuse != nil {
		text, err := r.prefixSummary(ctx, *reuse)
		if err == nil || !agentcontext.IsContextLengthExceeded(err) {
			return text, err
		}
	}
	return agentcontext.Summarize(ctx, summaryModel{r: r}, agentcontext.SummaryInput{
		SessionID:   r.task.SessionID,
		Previous:    agentcontext.SummaryText(previous),
		Messages:    covered,
		ChunkTokens: limit / 2,
	})
}

// prefixSummary sends request with the summary instruction appended and tool
// calls forbidden.
func (r *run) prefixSummary(ctx context.Context, request providers.Request) (string, error) {
	request.Messages = append(slices.Clone(request.Messages), providers.Message{Role: string(transcript.MessageRoleUser), Content: agentcontext.SummaryInstruction})
	request.ToolChoice = providers.ToolChoiceNone
	response, err := summaryModel{r: r}.Generate(ctx, request)
	if err != nil {
		return "", err
	}
	return agentcontext.SummaryReply(response)
}
```

`internal/agent/fit.go` (import `transcript` is already there): replace `fitRequest` and add `exhaustedTurn` and the note text:

```go
const contextExhaustedText = "This run's conversation no longer fits the model's context window, even after summarising it, so the run stops here. Do not call tools. Reply briefly: what is done, what remains, and how to continue."

// fitRequest builds the step's request within the model's window: old bulky
// results are elided at 60% of it and older history is summarised at 80%; once
// summaries stop paying off, the step becomes a context-exhausted final turn.
func (r *run) fitRequest(ctx context.Context, final StopReason) (providers.Request, StopReason, error) {
	request, err := r.buildRequest(ctx, final)
	if err != nil {
		return providers.Request{}, final, err
	}
	limit := r.contextLimit()
	tokens := r.promptTokens(request)
	if agentcontext.ElisionDue(tokens, limit) && r.advanceElision() {
		if request, err = r.buildRequest(ctx, final); err != nil {
			return providers.Request{}, final, err
		}
		tokens = agentcontext.EstimateRequestTokens(request)
	}
	if !agentcontext.SummaryDue(tokens, limit) {
		return request, final, nil
	}
	if r.counters.LowYield >= lowYieldLimit {
		return r.exhaustedTurn(ctx, request, final)
	}
	var reuse *providers.Request
	if tokens <= limit {
		reuse = &request
	}
	compacted, err := r.compactHistory(ctx, reuse, tokens, agentcontext.TailPercent)
	if err != nil || !compacted {
		return request, final, err
	}
	if final == "" {
		if err := r.syncContext(ctx); err != nil {
			return providers.Request{}, final, err
		}
	}
	request, err = r.buildRequest(ctx, final)
	return request, final, err
}

// exhaustedTurn turns the step into a tool-less final turn that explains the
// context is exhausted; a final turn already under way stays as it is.
func (r *run) exhaustedTurn(ctx context.Context, request providers.Request, final StopReason) (providers.Request, StopReason, error) {
	if final != "" {
		return request, final, nil
	}
	if err := r.appendEngineMessage(ctx, transcript.OriginEngineModel, contextExhaustedText); err != nil {
		return providers.Request{}, final, err
	}
	request, err := r.buildRequest(ctx, StopContextExhausted)
	return request, StopContextExhausted, err
}
```

`internal/agent/engine.go` (import `fmt`): replace `step` with

```go
func (r *run) step(ctx context.Context) stepResult {
	waiting, err := r.resumeApproved(ctx)
	if err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingApproval}
	}
	if err := r.syncContext(ctx); err != nil {
		return failedStep(err)
	}
	final, err := r.prepareStep(ctx)
	if err != nil {
		return failedStep(err)
	}
	if err := r.checkpoint(ctx, PhaseModel, "", ""); err != nil {
		return failedStep(err)
	}
	request, final, err := r.fitRequest(ctx, final)
	if err != nil {
		return failedStep(err)
	}
	r.counters.Steps++
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && agentcontext.IsContextLengthExceeded(err) {
		compacted, compactErr := r.compactHistory(ctx, nil, r.promptTokens(request), agentcontext.TailPercent/2)
		if compactErr != nil {
			return failedStep(compactErr)
		}
		if !compacted {
			err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
		} else {
			retry, buildErr := r.buildRequest(ctx, final)
			if buildErr != nil {
				return failedStep(buildErr)
			}
			if gen, err = r.generateWithRetry(ctx, retry); err != nil && agentcontext.IsContextLengthExceeded(err) {
				err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
			}
		}
	}
	if err == nil {
		r.anchorUsage(gen.response)
	}
	if final != "" && errors.Is(err, providers.ErrEmptyResponse) {
		return finalTurn(gen, final)
	}
	if err != nil {
		result := stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: gen.response, err: err, markErrored: true}
		if errors.Is(err, ErrContextExhausted) {
			result.stop = StopContextExhausted
		}
		return result
	}
	if r.canceled(ctx) {
		return stepResult{kind: stepDone, canceled: true, assistant: &gen.assistant, saved: gen.saved}
	}
	if final != "" {
		return finalTurn(gen, final)
	}
	return r.handleResponse(ctx, gen)
}
```

In `settle`, replace `outcome := Outcome{Status: StatusFailed, Err: result.err}` with `outcome := Outcome{Status: StatusFailed, Err: result.err, StopReason: result.stop}`, and in the `Outcome` doc comment (`task.go`) change the last sentence to "StopReason is set whenever the run completed or reached completion, and for a context-exhausted failure."

- [ ] **Step 5: Core** — `internal/core/run_outcome.go`, `applyOutcome`: replace the `case agent.StatusFailed:` branch with

```go
	case agent.StatusFailed:
		run.StopReason = outcome.StopReason
		if outcome.MarkErrored && outcome.Assistant != nil {
			return false, c.persistAssistantError(ctx, run, outcome.Assistant, outcome.AssistantSaved, outcome.Err)
		}
		return false, c.failRunByID(ctx, run, outcome.Err)
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/agent/... ./internal/core/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/context/summarize.go internal/agent/task.go internal/agent/budget.go internal/agent/engine.go internal/agent/compact.go internal/agent/fit.go internal/agent/compact_test.go internal/agent/fit_test.go internal/core/run_outcome.go internal/core/native_run_characterization_test.go
git commit -m "feat(agent): summarise with the step's own request and stop on exhaustion

A summary is asked with the step's own request plus a trailing
instruction and tool_choice none, so the provider reads the prefix from
its cache; a request over the window, or one the provider rejects as too
long, is summarised on its own in chunks. After two summaries in a row
that saved under a tenth, a step still over the threshold becomes a
tool-less final turn and the run fails with stop reason
context_exhausted; so does a second context-length error or one with
nothing left to summarise.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Optional `compact_model`

**Files:**
- Modify: `internal/agent/task.go` (`Task.CompactModel`), `internal/agent/generate.go` (`summaryModel`), `internal/agent/compact.go` (`summary`, `prefixSummary`)
- Create: `internal/core/compact_model.go`, `internal/core/compact_model_test.go`
- Modify: `internal/core/core.go` (fields), `internal/core/run_execute.go` (`nativeEngine`), `internal/core/session_context.go` (`CompactSession`)
- Modify: `internal/setup/types.go` (`DaemonConfig`, new `CompactModelConfig`), `internal/daemoncmd/bootstrap.go`, `internal/daemoncmd/run.go`
- Test: `internal/agent/compact_test.go`

- [ ] **Step 1: Write the failing tests** — append to `internal/agent/compact_test.go`:

```go
func TestSummariesUseTheCompactModel(t *testing.T) {
	f := agenttest.NewFixture()
	f.Window = 100_000
	f.Tools.Funcs["read"] = readTool
	main := agenttest.NewScriptedModel(
		agenttest.Turn{Response: providers.Response{ToolCalls: []providers.ToolCall{call("c1", "read")}, Usage: providers.Usage{PromptTokens: 70_000}}},
		text("Done."),
	)
	cheap := agenttest.NewScriptedModel(agenttest.Turn{Response: providers.Response{Text: "SUMMARY", Model: "cheap-model"}})
	task := f.Task(main)
	task.CompactModel = cheap

	outcome := runTask(t, f, task)

	if outcome.Status != agent.StatusCompleted || len(main.Requests()) != 2 || len(cheap.Requests()) != 1 {
		t.Fatalf("outcome = %+v main = %d cheap = %d", outcome, len(main.Requests()), len(cheap.Requests()))
	}
	if summary := cheap.Requests()[0]; !strings.HasPrefix(summary.SystemPrompt, "You compact matrixclaw chat histories") || len(summary.Tools) != 0 {
		t.Fatalf("compact model got %q with %d tools, want a standalone summary", summary.SystemPrompt, len(summary.Tools))
	}
	if !strings.Contains(main.Requests()[1].Messages[0].Content, "SUMMARY") {
		t.Fatalf("main request starts with %+v", main.Requests()[1].Messages[0])
	}
	if steps := f.Journal.Steps; len(steps) != 3 || steps[1].StopReason != "compact" || steps[1].Model != "cheap-model" {
		t.Fatalf("steps = %+v", steps)
	}
}
```

Create `internal/core/compact_model_test.go`:

```go
package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
)

// twoModelLLMs resolves the provider "cheap" to its own runtime.
type twoModelLLMs struct {
	recoveryLLMs
	cheap providers.Runtime
}

func (l twoModelLLMs) Resolve(ctx context.Context, providerID string, modelID string) (providers.Runtime, core.SessionProviderOption, string, error) {
	if providerID == "cheap" {
		return l.cheap, core.SessionProviderOption{ID: "cheap", Configured: true, DefaultModel: modelID}, modelID, nil
	}
	return l.recoveryLLMs.Resolve(ctx, providerID, modelID)
}

func TestManualCompactUsesTheCompactModel(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var mainCalls, cheapCalls int
	app.WithSessionLLMs(twoModelLLMs{
		recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
			mainCalls++
			return providers.Response{Text: "main summary"}, nil
		})},
		cheap: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
			cheapCalls++
			return providers.Response{Text: "CHEAP SUMMARY"}, nil
		}),
	})
	app.WithCompactModel("cheap", "small")
	session, _ := saveCrashRecoveryRun(t, db, "cheap", core.RunStatusCompleted, false)

	result, err := app.CompactSession(context.Background(), session.ID)

	if err != nil || mainCalls != 0 || cheapCalls != 1 || result.Message.Compaction == nil || result.Message.Compaction.Summary != "CHEAP SUMMARY" {
		t.Fatalf("result = %+v err = %v main = %d cheap = %d", result.Message, err, mainCalls, cheapCalls)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/agent/ -run TestSummariesUseTheCompactModel; go test ./internal/core/ -run TestManualCompactUsesTheCompactModel`
Expected: build failure `task.CompactModel undefined` / `app.WithCompactModel undefined`.

- [ ] **Step 3: Engine** — `internal/agent/task.go`, `Task`: add after `WindowTokens`

```go
	// CompactModel writes the run's summaries instead of Model when set.
	CompactModel Model
```

`internal/agent/generate.go`: replace `summaryModel` and its `Generate` with

```go
// summaryModel generates summaries with model and records every successful one
// as a compact step of the run.
type summaryModel struct {
	r     *run
	model Model
}

func (m summaryModel) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	started := time.Now()
	response, err := m.model.Generate(ctx, request)
	if err != nil {
		return response, err
	}
	return response, m.r.recordStep(ctx, response, compactStopReason, time.Since(started))
}
```

`internal/agent/compact.go`: replace `summary` with the version below and use `summaryModel{r: r, model: r.task.Model}` in `prefixSummary`:

```go
// summary asks for the summary with the step's own request, so the provider
// reuses its cached prefix; without one, when that request is too long, or with
// a compact model set, the covered history is summarised on its own, in chunks.
func (r *run) summary(ctx context.Context, reuse *providers.Request, previous *transcript.Compaction, covered []transcript.Message, limit int) (string, error) {
	if reuse != nil && r.task.CompactModel == nil {
		text, err := r.prefixSummary(ctx, *reuse)
		if err == nil || !agentcontext.IsContextLengthExceeded(err) {
			return text, err
		}
	}
	model := r.task.Model
	if r.task.CompactModel != nil {
		model = r.task.CompactModel
	}
	return agentcontext.Summarize(ctx, summaryModel{r: r, model: model}, agentcontext.SummaryInput{
		SessionID:   r.task.SessionID,
		Previous:    agentcontext.SummaryText(previous),
		Messages:    covered,
		ChunkTokens: limit / 2,
	})
}
```

- [ ] **Step 4: Core** — add `compactProvider string` and `compactModel string` to the `Core` struct in `internal/core/core.go` (after `sessionFiles`). Create `internal/core/compact_model.go`:

```go
package core

import (
	"context"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// WithCompactModel names the provider and model that write context summaries;
// empty keeps each run's own model.
func (c *Core) WithCompactModel(providerID string, modelID string) *Core {
	c.compactProvider, c.compactModel = strings.TrimSpace(providerID), strings.TrimSpace(modelID)
	return c
}

// compactRuntime is the configured summary model; nil means the run's own model,
// also when the configured one cannot be resolved.
func (c *Core) compactRuntime(ctx context.Context) providers.Runtime {
	if c.compactProvider == "" {
		return nil
	}
	llms := c.sessionLLMs()
	if llms == nil {
		return nil
	}
	runtime, _, _, err := llms.Resolve(ctx, c.compactProvider, c.compactModel)
	if err != nil || runtime == nil {
		log.Printf("core: compact model %s/%s is unavailable, summaries use the run's model: %v", c.compactProvider, c.compactModel, err)
		return nil
	}
	return runtime
}
```

`internal/core/run_execute.go`, `nativeEngine`: right before `return task, engine, nil` add

```go
	if compact := c.compactRuntime(ctx); compact != nil {
		task.CompactModel = compact
	}
```

`internal/core/session_context.go`, `CompactSession`: replace

```go
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return CompactSessionResult{}, err
	}
```

with

```go
	runtime := c.compactRuntime(ctx)
	if runtime == nil {
		if runtime, err = c.resolveSessionRuntime(ctx, session); err != nil {
			return CompactSessionResult{}, err
		}
	}
```

- [ ] **Step 5: Daemon configuration** — `internal/setup/types.go`: add to `DaemonConfig` after `Budgets`

```go
	CompactModel    CompactModelConfig `json:"compact_model,omitzero"`
```

and after `RunBudgetConfig`:

```go
// CompactModelConfig names the provider and model that write context summaries
// instead of each run's own model; empty keeps the run's model.
type CompactModelConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}
```

`internal/daemoncmd/bootstrap.go`: add `CompactModel setup.CompactModelConfig` to `bootstrapConfig` and, in `loadBootstrap` right after `cfg.Budgets = budgets`, `cfg.CompactModel = setupCfg.Daemon.CompactModel`. `internal/daemoncmd/run.go`: add `WithCompactModel(bootstrap.CompactModel.Provider, bootstrap.CompactModel.Model).` to the `core.New(sqliteStore).` chain after `WithSessionFiles(…)`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/agent/... ./internal/core/ ./internal/setup/ ./internal/daemoncmd/`
Expected: PASS.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/task.go internal/agent/generate.go internal/agent/compact.go internal/agent/compact_test.go internal/core/core.go internal/core/compact_model.go internal/core/compact_model_test.go internal/core/run_execute.go internal/core/session_context.go internal/setup/types.go internal/daemoncmd/bootstrap.go internal/daemoncmd/run.go
git commit -m "feat(core): optional compact model for context summaries

daemon.compact_model {provider, model} names a model that writes the
summaries of runs and of /compact instead of the session's own model. It
summarises on its own, in chunks, since it cannot reuse the run's cached
prefix; if it cannot be resolved the run's model is used.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Anthropic cache breakpoints on system and the latest turns

**Files:**
- Modify: `internal/providers/ai/anthropiccompat/wire.go` (`anthropicBlock`), `internal/providers/ai/anthropiccompat/encode.go` (`encodeRequest`, `markToolsCacheBreakpoint` → `markCacheBreakpoints`)
- Test: `internal/providers/ai/anthropiccompat/adapter_test.go` (`TestCacheBreakpointMarksOnlyTheTools`)

- [ ] **Step 1: Write the failing test** — replace `TestCacheBreakpointMarksOnlyTheTools` with:

```go
func TestCacheBreakpointsMarkToolsSystemAndTheLatestTurns(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"), textReply("ok"))
	request := providers.Request{
		CacheKey:     "session-1",
		SystemPrompt: "System rules.",
		Tools:        testTools,
		Messages: []providers.Message{
			{Role: "user", Content: "Start"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "toolu_2", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
			{Role: "tool", ToolCallID: "toolu_2", Content: "package a"},
		},
	}
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(sent(0)), `"cache_control"`); got != 4 {
		t.Fatalf("cache_control count = %d, want 4: %s", got, sent(0))
	}
	body := decodeSent(t, sent(0))
	marked := func(block anthropicBlock) bool { return block.CacheControl != nil && block.CacheControl.Type == "ephemeral" }
	if body.Tools[1].CacheControl == nil || body.Tools[0].CacheControl != nil || !marked(body.System[0]) {
		t.Fatalf("tools/system breakpoints wrong: %s", sent(0))
	}
	turns := body.Messages
	if len(turns) != 5 || !marked(turns[4].Content[len(turns[4].Content)-1]) || !marked(turns[2].Content[len(turns[2].Content)-1]) || marked(turns[0].Content[0]) {
		t.Fatalf("message breakpoints wrong: %s", sent(0))
	}
	request.CacheKey = ""
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sent(1)), "cache_control") {
		t.Fatalf("request without cache key has breakpoints: %s", sent(1))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/providers/ai/anthropiccompat/ -run TestCacheBreakpoints`
Expected: build failure `block.CacheControl undefined`.

- [ ] **Step 3: Implement** — `wire.go`: add as the last field of `anthropicBlock`

```go
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
```

`encode.go`: in `encodeRequest` replace `markToolsCacheBreakpoint(payload.Tools)` with `markCacheBreakpoints(payload)`, change its doc comment's second line to "Cache breakpoints are set only for session requests (non-empty CacheKey).", and replace `markToolsCacheBreakpoint` with:

```go
// markCacheBreakpoints caches the stable prefix and the conversation so far:
// the tools, the system prompt, the newest user turn and the one the previous
// request ended with; four breakpoints, the most the API allows.
func markCacheBreakpoints(payload *anthropicRequest) {
	if last := len(payload.Tools) - 1; last >= 0 {
		payload.Tools[last].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
	if last := len(payload.System) - 1; last >= 0 {
		payload.System[last].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
	marked := 0
	for i := len(payload.Messages) - 1; i >= 0 && marked < 2; i-- {
		content := payload.Messages[i].Content
		if payload.Messages[i].Role != "user" || len(content) == 0 {
			continue
		}
		content[len(content)-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
		marked++
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/providers/...`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/providers/ai/anthropiccompat/wire.go internal/providers/ai/anthropiccompat/encode.go internal/providers/ai/anthropiccompat/adapter_test.go
git commit -m "feat(providers): cache the system prompt and the latest turns on Anthropic

Session requests mark four cache breakpoints: the tool definitions, the
system prompt, the newest user turn and the turn the previous request
ended with, so each step reads the conversation so far from the cache.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: OpenRouter Claude content-part cache breakpoints

**Files:**
- Modify: `internal/providers/openai_chat_options.go` (`OpenAIChatOptions`, `ResolveOpenAIChatOptions`, new `claudeModel`)
- Modify: `internal/providers/ai/openaicompat/types.go` (`chatCompletionContentPart`, new `chatCacheControl`), `config.go` (`Runtime`, `New`), `chat.go` (`chatPayload`), `message.go` (new `markContentCacheBreakpoints`, `withCacheBreakpoint`)
- Test: `internal/providers/openai_chat_options_test.go`, `internal/providers/ai/openaicompat/request_controls_test.go`

- [ ] **Step 1: Write the failing tests** — append to `internal/providers/openai_chat_options_test.go`:

```go
func TestContentCacheBreakpointsOnlyForClaudeOnOpenRouter(t *testing.T) {
	router := "https://openrouter.ai/api/v1"
	for _, tc := range []struct {
		baseURL string
		model   string
		want    bool
	}{
		{router, "anthropic/claude-sonnet-4.6", true},
		{router, "openai/gpt-5.4", false},
		{"https://api.openai.com/v1", "claude-sonnet-4.6", false},
	} {
		got := ResolveOpenAIChatOptions(ProfileForModel("openrouter", TypeOpenAICompat, tc.model), tc.baseURL, tc.model).ContentCacheControl
		if got != tc.want {
			t.Errorf("%s %s: content cache control = %v, want %v", tc.baseURL, tc.model, got, tc.want)
		}
	}
}
```

Append to `internal/providers/ai/openaicompat/request_controls_test.go`:

```go
func TestClaudeOnOpenRouterGetsContentCacheBreakpoints(t *testing.T) {
	request := providers.Request{
		CacheKey:     "session-1",
		SystemPrompt: "rules",
		Messages: []providers.Message{
			{Role: "user", Content: "start"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "c1", Content: "a.go"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c2", Name: "read", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "c2", Content: "package a"},
		},
	}
	claude := &Runtime{model: "anthropic/claude-sonnet-4.6", contentCacheControl: true}
	body, err := json.Marshal(claude.chatPayload(context.Background(), request))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(body), `"cache_control"`); got != 3 {
		t.Fatalf("cache_control count = %d, want system + two newest tool results: %s", got, body)
	}
	if !strings.Contains(string(body), `{"role":"system","content":[{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}]}`) {
		t.Fatalf("system message not marked: %s", body)
	}
	request.CacheKey = ""
	if body, _ := json.Marshal(claude.chatPayload(context.Background(), request)); strings.Contains(string(body), "cache_control") {
		t.Fatalf("request without cache key has breakpoints: %s", body)
	}
	request.CacheKey = "session-1"
	if body, _ := json.Marshal((&Runtime{model: "openai/gpt-5.4"}).chatPayload(context.Background(), request)); strings.Contains(string(body), "cache_control") {
		t.Fatalf("non-Claude model has breakpoints: %s", body)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/providers/ -run TestContentCacheBreakpoints; go test ./internal/providers/ai/openaicompat/ -run TestClaudeOnOpenRouter`
Expected: build failure `ContentCacheControl undefined` / `unknown field contentCacheControl`.

- [ ] **Step 3: Implement** — `internal/providers/openai_chat_options.go`: add to `OpenAIChatOptions`

```go
	ContentCacheControl bool // mark cache breakpoints in content parts; OpenRouter passes them to Claude models
```

in `ResolveOpenAIChatOptions`, right after the `options.PromptCacheKey = …` line:

```go
	options.ContentCacheControl = openAICompatibleHost(baseURL, "openrouter.ai") && claudeModel(model)
```

and add:

```go
// claudeModel reports whether a gateway model ID names an Anthropic Claude model.
func claudeModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "anthropic/") || strings.Contains(model, "claude")
}
```

`internal/providers/ai/openaicompat/types.go`: add as the last field of `chatCompletionContentPart`

```go
	CacheControl *chatCacheControl              `json:"cache_control,omitempty"`
```

and

```go
type chatCacheControl struct {
	Type string `json:"type"`
}
```

`config.go`: add `contentCacheControl bool` to `Runtime` (after `promptCacheKey`) and `contentCacheControl: chatOptions.ContentCacheControl,` to the literal in `New`.

`chat.go`, `chatPayload`: directly after the `for _, message := range request.Messages { … }` loop add

```go
	if r.contentCacheControl && strings.TrimSpace(request.CacheKey) != "" {
		markContentCacheBreakpoints(payload.Messages)
	}
```

`message.go`: add

```go
// markContentCacheBreakpoints marks the system prompt and the two newest user or
// tool messages as cache breakpoints, turning their content into parts.
func markContentCacheBreakpoints(messages []chatCompletionMessage) {
	marked := 0
	for i := len(messages) - 1; i >= 0; i-- {
		switch role := messages[i].Role; {
		case role == "system":
			withCacheBreakpoint(&messages[i])
		case marked < 2 && (role == "user" || role == "tool"):
			if withCacheBreakpoint(&messages[i]) {
				marked++
			}
		}
	}
}

// withCacheBreakpoint puts a cache breakpoint on the last text part of message.
func withCacheBreakpoint(message *chatCompletionMessage) bool {
	switch content := message.Content.(type) {
	case string:
		if strings.TrimSpace(content) == "" {
			return false
		}
		message.Content = []chatCompletionContentPart{{Type: "text", Text: content, CacheControl: &chatCacheControl{Type: "ephemeral"}}}
		return true
	case []chatCompletionContentPart:
		for j := len(content) - 1; j >= 0; j-- {
			if content[j].Type == "text" {
				content[j].CacheControl = &chatCacheControl{Type: "ephemeral"}
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/providers/...`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/providers/openai_chat_options.go internal/providers/openai_chat_options_test.go internal/providers/ai/openaicompat/types.go internal/providers/ai/openaicompat/config.go internal/providers/ai/openaicompat/chat.go internal/providers/ai/openaicompat/message.go internal/providers/ai/openaicompat/request_controls_test.go
git commit -m "feat(providers): cache breakpoints for Claude models on OpenRouter

Session requests to a Claude model through OpenRouter send the system
prompt and the two newest user or tool messages as content parts with
cache_control, which OpenRouter passes on to Anthropic.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 15: Cache hit in `/usage`

**Files:**
- Modify: `internal/controlplane/usage.go` (`usageInfoRows`, new `cacheHitLabel`)
- Test: `internal/controlplane/usage_test.go` (`TestUsageCommandShowsStepsAndCacheTokens`)

- [ ] **Step 1: Write the failing test** — in `TestUsageCommandShowsStepsAndCacheTokens` replace the `want` line and the row count:

```go
	want := "Runs: 2\nSteps: 5\nPrompt: 12k tokens\nCache read: 9.0k tokens\nCache write: 1.5k tokens\nCache hit: 75%\nOutput: 800 tokens\nReasoning: 200 tokens"
	if result.Info == nil || result.Info.Text != want || len(result.Info.Rows) != 8 {
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/controlplane/ -run TestUsageCommandShowsStepsAndCacheTokens`
Expected: FAIL (no "Cache hit" row).

- [ ] **Step 3: Implement** — in `internal/controlplane/usage.go` replace `usageInfoRows` and add `cacheHitLabel`:

```go
func usageInfoRows(summary core.UsageSummary) []InfoRow {
	return []InfoRow{
		{Label: "Runs", Value: fmt.Sprintf("%d", summary.Runs)},
		{Label: "Steps", Value: fmt.Sprintf("%d", summary.Steps)},
		{Label: "Prompt", Value: usageTokenLabel(summary.PromptTokens)},
		{Label: "Cache read", Value: usageTokenLabel(summary.CacheReadTokens)},
		{Label: "Cache write", Value: usageTokenLabel(summary.CacheWriteTokens)},
		{Label: "Cache hit", Value: cacheHitLabel(summary)},
		{Label: "Output", Value: usageTokenLabel(summary.OutputTokens)},
		{Label: "Reasoning", Value: usageTokenLabel(summary.ReasoningTokens)},
	}
}

// cacheHitLabel is the share of prompt tokens the provider read from its cache.
func cacheHitLabel(summary core.UsageSummary) string {
	if summary.PromptTokens <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d%%", summary.CacheReadTokens*100/summary.PromptTokens)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/controlplane/ ./clients/...`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/controlplane/usage.go internal/controlplane/usage_test.go
git commit -m "feat(controlplane): show the prompt cache hit rate in /usage

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 16: Long-run regression tests

These tests pin what the stage is for: hundreds of steps with growing outputs finish without overflowing the window and without splitting a call from its result.

**Files:**
- Create: `internal/agent/long_run_test.go`, `internal/core/long_run_test.go`

- [ ] **Step 1: Write the tests** — `internal/agent/long_run_test.go`:

```go
package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// pairedResults reports whether every tool result of request follows the
// assistant message that made its call.
func pairedResults(request providers.Request) bool {
	called := map[string]bool{}
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			called[call.ID] = true
		}
		if message.Role == "tool" && !called[message.ToolCallID] {
			return false
		}
	}
	return true
}

func TestTwoHundredStepRunWithGrowingOutputsStaysWithinTheWindow(t *testing.T) {
	const steps = 200
	f := agenttest.NewFixture()
	f.Window = 60_000
	limit := agentcontext.EffectiveWindow(f.Window, int(providers.DefaultMaxOutputTokens))
	outputs := 0
	f.Tools.Funcs["read"] = func(tools.Call) tools.Result {
		outputs++
		return tools.Result{Content: fmt.Sprintf("output %d\n%s", outputs, strings.Repeat("x", min(400*outputs, 30_000)))}
	}
	var mainRequests, summaries, largest int
	elided := false
	model := agenttest.ModelFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		tokens := agentcontext.EstimateRequestTokens(request)
		usage := providers.Usage{PromptTokens: int64(tokens), OutputTokens: 20}
		if isSummaryRequest(request) {
			summaries++
			return providers.Response{Text: fmt.Sprintf("SUMMARY %d: reading outputs", summaries), Usage: usage}, nil
		}
		mainRequests++
		largest = max(largest, tokens)
		if !pairedResults(request) {
			return providers.Response{}, fmt.Errorf("request %d has a tool result without its call", mainRequests)
		}
		for _, message := range request.Messages {
			elided = elided || strings.HasPrefix(message.Content, "[output of read(")
		}
		if mainRequests > steps {
			return providers.Response{Text: "All outputs read.", Usage: usage}, nil
		}
		id := fmt.Sprintf("c%d", mainRequests)
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: id, Name: "read", Arguments: []byte(fmt.Sprintf(`{"n":%d}`, mainRequests))}}, Usage: usage}, nil
	})

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || outcome.StopReason != agent.StopDone || outcome.Assistant.Content != "All outputs read." {
		t.Fatalf("outcome = %+v", outcome)
	}
	if mainRequests != steps+1 || len(f.Tools.Calls) != steps {
		t.Fatalf("main requests = %d tool calls = %d", mainRequests, len(f.Tools.Calls))
	}
	if largest >= limit || summaries == 0 || !elided {
		t.Fatalf("largest request = %d of %d, summaries = %d, elided = %v", largest, limit, summaries, elided)
	}
}
```

`internal/core/long_run_test.go`:

```go
package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestLongNativeRunCompactsKeepsLargeOutputsAndCompletes(t *testing.T) {
	const steps = 120
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	defaults := core.DefaultRunBudgets()
	app.WithRunBudgets(core.RunBudgets{User: agent.Budget{Steps: steps + 10}, Subagent: defaults.Subagent, Automation: defaults.Automation})
	app.WithSessionFiles(t.TempDir())
	outputs := 0
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("probe", tools.EffectReadOnly), run: func(context.Context, tools.Call) tools.Result {
		outputs++
		size := min(300*outputs, 24_000)
		if outputs%40 == 0 {
			size = 60_000
		}
		return tools.Result{Content: fmt.Sprintf("probe %d\n%s", outputs, strings.Repeat("y", size))}
	}}))
	limit := agentcontext.EffectiveWindow(60_000, int(providers.DefaultMaxOutputTokens))
	var mainRequests, summaries, largest int
	app.WithSessionLLMs(windowLLMs{window: 60_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		tokens := agentcontext.EstimateRequestTokens(request)
		usage := providers.Usage{PromptTokens: int64(tokens), OutputTokens: 10}
		last := request.Messages[len(request.Messages)-1]
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") || last.Content == agentcontext.SummaryInstruction {
			summaries++
			return providers.Response{Text: "SUMMARY: probing", Usage: usage}, nil
		}
		mainRequests++
		largest = max(largest, tokens)
		if mainRequests > steps {
			return providers.Response{Text: "Probed everything.", Usage: usage}, nil
		}
		arguments := json.RawMessage(fmt.Sprintf(`{"n":%d}`, mainRequests))
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("probe_%d", mainRequests), Name: "probe", Arguments: arguments}}, Usage: usage}, nil
	})}})
	session, run := saveCrashRecoveryRun(t, db, "long", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if mainRequests != steps+1 || summaries == 0 || largest >= limit {
		t.Fatalf("main = %d summaries = %d largest = %d of %d", mainRequests, summaries, largest, limit)
	}
	boundaries, kept := 0, 0
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Compaction != nil {
			boundaries++
		}
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.OutputPath != "" {
				kept++
			}
		}
	}
	if boundaries == 0 || kept != steps/40 {
		t.Fatalf("boundaries = %d outputs kept in files = %d, want some and %d", boundaries, kept, steps/40)
	}
}
```

- [ ] **Step 2: Run them**

Run: `go test ./internal/agent/ -run TestTwoHundredStepRun -v; go test ./internal/core/ -run TestLongNativeRun -v`
Expected: PASS (they exercise Tasks 5–11; if one fails, the bug is in those tasks — fix it there and report it, do not loosen the test).

- [ ] **Step 3: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/agent/long_run_test.go internal/core/long_run_test.go
git commit -m "test: long runs with growing outputs stay within the model window

A scripted 200-step engine run and a 120-step SQLite-backed run with
growing tool outputs complete; every request stays under the usable
window, results stay paired with their calls, older results are elided,
history is summarised and oversized outputs land in session files.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 17: Raise the default budgets

**Files:**
- Modify: `internal/core/run_budget.go` (`DefaultRunBudgets`)
- Test: `internal/daemoncmd/run_budgets_test.go` (`TestRunBudgetsFromConfigOverrideOnlyTheGivenFields`)

- [ ] **Step 1: Keep the override test meaningful** — the test overrides the user steps with 300, which becomes the default. In `TestRunBudgetsFromConfigOverrideOnlyTheGivenFields` change `Steps: 300` to `Steps: 120` and `budgets.User.Steps != 300` to `budgets.User.Steps != 120`.

- [ ] **Step 2: Raise the defaults** — in `internal/core/run_budget.go` replace `DefaultRunBudgets` with:

```go
// DefaultRunBudgets apply when the daemon configuration names none.
func DefaultRunBudgets() RunBudgets {
	return RunBudgets{
		User:       agent.Budget{Steps: 300, ActiveTime: 4 * time.Hour},
		Subagent:   agent.Budget{Steps: 100, ActiveTime: time.Hour},
		Automation: agent.Budget{Steps: 50, ActiveTime: 30 * time.Minute},
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/core/ ./internal/daemoncmd/ ./internal/controlplane/`
Expected: PASS (budget tests derive their numbers from `DefaultRunBudgets`; if one hardcodes 32, derive it from the defaults instead and report it).

- [ ] **Step 4: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/run_budget.go internal/daemoncmd/run_budgets_test.go
git commit -m "feat(core): raise default run budgets to 300, 100 and 50 steps

User runs get 300 steps in 4 h of active time, subagents 100 in 1 h,
automation and auto-wake runs 50 in 30 min, now that a run compacts its
own context instead of overflowing it.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 18: Stage verification

- [ ] **Step 1: Full checks**

```bash
gofmt -l ./internal ./clients ./cmd 2>/dev/null
go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/core/...
```

Expected: `gofmt -l` prints nothing; everything passes.

- [ ] **Step 2: Leftovers**

```bash
grep -rn "LatestSummaryForRun\|LatestMarker\|IsMarker\|ClearedMarkerContent\|CompactBackoffActive\|PlanSnapshot\|Recommendation(\|minimumCompactThreshold\|maxProviderToolResultRunes\|isBrowserSnapshotToolName" --include=*.go . | grep -v '/.worktrees/'
grep -rn "🧠 Context compacted\|🧹 Context cleared" --include=*.go . | grep -v '/.worktrees/'
```

Expected: the first prints nothing; the second prints only `internal/store/schema_compaction.go` (the migration's legacy prefixes) and test files.

- [ ] **Step 3: Report for the owner** — list the commits of this stage (`git log --oneline <first-stage-3-commit>^..HEAD`) and remind the owner of the manual checks that need the test stand (executors do not start daemons): a long task through the TUI with a Claude model (native Anthropic and via OpenRouter) — `/usage` should show a rising cache hit, `/context` a boundary after compaction, a run past the old 32 steps should complete; an old session with emoji markers should load with its migrated boundary; `daemon.compact_model` can be tried in `setup.json`.

---

## Self-review

**Spec coverage (§3 Context, Stages row 3):**
- Normalised usage — done in Stage 0; used here as the anchor (`promptTokens`, Task 7).
- Token accounting: last `PromptTokens` + estimate since; estimate authoritative after elision/summary (anchor dropped in `advanceElision`, `writeBoundary`) and at run start; the model is fixed per run, so a model change always starts a new run without an anchor — Task 7/10. Cyrillic-aware estimate, 1500 per image — Task 1.
- Effective window = window − output limit − 5%, unknown → 128k, 80k floor removed — Tasks 1, 5, 7.
- Large outputs to files with path + head/tail, replacing the 12k-rune truncation; run-less calls included; cleanup with the session — Task 8.
- Level 1 elision at 60%, results > ~1k tokens older than 5 rounds, images older than 3 steps, all at once, DB keeps everything — Task 10.
- Level 2 summary at 80% or when elision was not enough, reusing the main request with trailing instruction and `tool_choice: none`, optional `compact_model`, tail ~25% in whole turns never splitting call/result, assignment and steers verbatim, summary structure, chunked summaries merged — Tasks 5, 11, 12. Todo re-sent by the context message — Stage 5 adds todo to `Prompts.Context`.
- Boundary `{summary, covers_through_seq, run_id, tokens_before, tokens_after}` and `Journal.Load` = latest boundary + `seq > covers_through_seq` — Tasks 3, 5 (plus `kept`, `cleared`; stored as a message field, decision 1). Old markers migrated — Task 3.
- Low-yield backoff from tokens_before/after; tripped and still over threshold → final turn, `failed` / `context_exhausted` — Tasks 5, 11.
- Context-length error → summary with half the tail, retry once, second overflow `context_exhausted` — Tasks 5, 11.
- Stable prompt (identity, rules, tool guidance, project root, skills, memory at run start); changing state as `engine_model` context messages only when changed (runtime status, recovery notice, memory changes, plan until Stage 5) — Task 9.
- Anthropic `cache_control` on system + tools + rolling message breakpoints (max 4); OpenRouter Claude content parts; `prompt_cache_key` unchanged — Tasks 13, 14.
- Cache read/write per step in `run_steps` (Stage 0) shown with a hit rate in `/usage` — Task 15.
- Defaults raised to 300 / 4 h, 100 / 1 h, 50 / 30 min after compaction works — Task 17, after the long-run tests of Task 16.

**Ordering:** every task leaves the tree building and tested: the store learns boundaries first (3), `/clear` writes them while the old reader still sees its legacy text (4), readers and writers switch together (5), clients follow (6); accounting (7) precedes elision (10) and prefix summaries (11); the stable prompt (9) precedes the cache breakpoints that depend on it (13, 14); budgets rise last (17).

**Type consistency:** `transcript.Compaction` fields (`Summary`, `Kept`, `CoversThroughSeq`, `RunID`, `TokensBefore`, `TokensAfter`, `Cleared`), `agent.Window{Boundary, Messages}` + `Compaction()`, `agentcontext.{TailPercent, EffectiveWindow, TailStart, Kept, SummaryText, SummaryMessages, BoundaryLabel, Summarize, SummaryInput, SummaryInstruction, SummaryReply, SummaryPercent, SummaryDue, ElisionPercent, ElisionDue, Elision, NextElision, Elide, LargeOutputTokens, HeadTail}`, `Counters.{LowYield, ContextHash, ContextSeq, ElidedResults, ElidedImages}`, `compactHistory(ctx, reuse, before, tailPercent)` from Task 11 on (Tasks 5–10 use the earlier signatures shown in them), `fitRequest` returning `(request, final, err)` from Task 11 on, `Task.{WindowTokens, CompactModel}`, `StopContextExhausted`, `ErrContextExhausted`, core `contextWindow`, `appendBoundary`, `ClearContext`, `WithSessionFiles`, `WithCompactModel`, `compactRuntime` are used with the same names throughout.

