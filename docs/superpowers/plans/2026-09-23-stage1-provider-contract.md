# Stage 1: Provider Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every provider adapter one contract: a normalised `StopReason`, a per-request `MaxOutputTokens`, a `ToolChoice`, a `CacheKey`, and a reasoning carrier that is sent back unchanged. The existing core loop behaves as it does today.

**Architecture:** `internal/providers/contract.go` gains the types. Small shared helpers (`ResolveStopReason`, `ResolveMaxOutputTokens`) live next to it. Each adapter maps its wire fields into the contract and has its own tests against a local `httptest` server or its decoder. Core does four things: it sets `Request.CacheKey`, turns `max_tokens` and `content_filter` into the same non-retryable error `finish_reason=length` gives today, stores a tool step's reasoning on the reply message, and replays each model response as one assistant message followed by its results.

**Tech Stack:** Go 1.26 (module `github.com/Suren878/matrixclaw`), `net/http/httptest`, SQLite-backed core integration tests (`newCrashRecoveryCore`).

---

## Ground rules for the executor

- **Stage 0 is merged before you start.** That means `internal/transcript` holds the message types (`transcript.Message`, `transcript.MessagePart`, `transcript.ReasoningPart`, …), `providers.Usage` is normalised, the HTTP clients use idle timeouts, and `run_steps` / `core.RunStep` / `SaveRunStep` / `ListRunSteps` exist. **All line numbers in this plan point at the pre-Stage-0 baseline `6176876`.** Stage 0 moves code around, so find each edit by the quoted code, not by the line number. Where Stage 0 renamed a core identifier to `transcript.X`, this plan already uses `transcript.X`.
- Work in `main`. Stage only the files each task lists and never run `git add -A`, because another session may be editing the repo. Every commit must pass `go build ./... && go test ./...`.
- Owner rules: delete replaced code (no shims, aliases or dead helpers), keep doc comments to 4 lines or fewer with no history in them, and test only observable behaviour.
- Run `gofmt -w` on every Go file you touch before running tests.

## Design decisions (read once)

1. **Stop reasons.** Adapters map their wire value and then call `providers.ResolveStopReason(reason, len(toolCalls))`, because gateways send `stop` alongside tool calls. A reply with no text and no calls is valid only for `max_tokens`, `refusal` and `content_filter` (`StopReason.AllowsEmptyReply`); otherwise it is still `ErrEmptyResponse`. When a reply stops at `max_tokens`, tool calls whose JSON was cut off are dropped rather than returned as an error, so Stage 2 can raise the limit and retry.
2. **Old loop.** In core, `stopReasonError` turns `StopMaxTokens` and `StopContentFilter` into `"<provider>: generation stopped before completion (<reason>)"`. That is the error openaicompat raises today for `length`/`content_filter`, and it is not retried. `StopRefusal` completes with the refusal text, as Codex does today. Gemini `MAX_TOKENS` used to return truncated text as a success; it now fails the same way (uniform, per the contract).
3. **Max output resolution** (`providers.ResolveMaxOutputTokens`): request `MaxOutputTokens > 0`, then provider config, then **the catalog limit only when it is below 16384**, then 16384. *Deviation from the literal contract:* catalog maxima run 64k to 128k. Sending those as `max_tokens` shrinks the usable window, and OpenRouter and vLLM reject a request when prompt + max_tokens exceeds the context. The catalog value is therefore used only to stay under a model's hard limit. Stage 2's "raise ×2, capped by the model" reads the cap from `ResolveModelMetadata(...).MaxOutputTokens`. Catalog sources: OpenRouter `top_provider.max_completion_tokens`, Gemini `outputTokenLimit` and Anthropic `/v1/models` `max_tokens`. openaicompat also gets a one-shot retry that drops the limit when a gateway rejects its value (`"valid range of max_tokens is [1, 8192]"`). This matters because openaicompat used to omit the field entirely.
4. **Codex never sends `max_output_tokens`.** The ChatGPT Codex backend answers `400 Unsupported parameter: max_output_tokens` (the Codex CLI never sends it). `openaicodex.Config.MaxOutputTokens` and the field are deleted, so `Request.MaxOutputTokens` has no effect on Codex.
5. **CacheKey.** openaicompat sends `prompt_cache_key` only when the base URL host is `api.openai.com` (`OpenAIChatOptions.PromptCacheKey`); other gateways are not known to accept it. Codex always sends it, as the Codex CLI does. Gemini relies on implicit caching and needs nothing. Native Anthropic is covered in Stage 1b, which marks only the tools breakpoint. **Claude via OpenRouter `cache_control` content parts, and native Anthropic system and message breakpoints, are deferred to Stage 3.** The system prompt still changes every turn until Stage 3, so a breakpoint after it would write the cache at 1.25× input price and never read it back. `prompt_cache_key` only routes requests and costs nothing, so it ships now.
6. **Reasoning carrier.** `providers.ReasoningBlock{Text, Signature, RedactedData}` goes on `Message.Reasoning` and `Response.Reasoning`. Gemini puts its thought signature in `Signature` and Codex puts `encrypted_content` in `RedactedData`. Core stores the blocks, plus plain `ReasoningContent`, as `transcript.ReasoningPart`s on the **reply message of the tool step** (the assistant message that `saveAssistantToolTurn` saves with finish reason `tool_calls`). Blocks are replayed only for tool steps; reasoning from final text replies is not needed by any provider. `ReasoningContent` (openaicompat `reasoning_content`) is built only from parts with no `Signature`/`RedactedData`, so today's DeepSeek behaviour stays byte-identical.
7. **One assistant message per model response.** The conversation builder merges a tool step into one `providers.Message`: the reply text, its reasoning and every call. Results follow in call order and still pair by id, with synthetic failures for missing ones. The step starts at the reply message (finish reason `tool_calls`) or, in older histories without one, at the first tool-call message. `providers.Message.IsError` carries `ToolResultPart.IsError`.
8. **Gemini thought signatures.** Gemini signs the first `functionCall` of a step. The adapter keeps that one signature (`Response.Reasoning = [{Signature}]`) and puts it back on the first `functionCall` part. Signatures on text parts are optional per Gemini's docs and are not kept.
9. **Streamed usage (Task 6b).** openaicompat streaming requests send `stream_options: {"include_usage": true}`, so streamed replies report usage in `run_steps`. The provider profile has no per-gateway allow-list; like the other optional parameters, it relies on a request quirk (`RetryWithoutStreamOptions`) that drops the field once when a gateway rejects it.
10. **Gemini streams (Task 9b).** Generation moves to `:streamGenerateContent?alt=sse`, so Stage 0's idle timeout covers Gemini and the TUI gets real deltas. A stream is complete only once a chunk carries `finishReason`; otherwise it is `ErrIncompleteResponse`, the same rule as the other SSE readers.
11. **run_steps.stop_reason.** Stage 0 writes it through `recordRunStep`, with `generationStopReason(response)` for loop generations and the literal `"compact"` for summaries. Task 3 changes only `generationStopReason` to use `Response.StopReason`, so the `compact` marker is never overwritten.

## Per-adapter mapping

| Adapter | Stop reason field | Mapping | `ToolChoiceNone` | `MaxOutputTokens` | `CacheKey` |
|---|---|---|---|---|---|
| openaicompat | `choices[0].finish_reason` (JSON and stream chunk) | `stop`, other → end_turn (tool_use with calls); `tool_calls`/`function_call` → tool_use; `length`/`max_tokens` → max_tokens; `content_filter` → content_filter | `"tool_choice":"none"` when tools are present | resolved value in `max_tokens` or `max_completion_tokens` (existing field choice); dropped once if the gateway rejects the value | `prompt_cache_key` only for host `api.openai.com` |
| gemini (SSE `streamGenerateContent`, Task 9b) | `candidates[0].finishReason` of the chunk that carries it (none = incomplete) | `STOP`, other → end_turn/tool_use; `MAX_TOKENS` → max_tokens; `SAFETY`, `RECITATION`, `BLOCKLIST`, `PROHIBITED_CONTENT`, `SPII`, `IMAGE_SAFETY` → content_filter; `MALFORMED_FUNCTION_CALL`, `UNEXPECTED_TOOL_CALL` → `ErrMalformedToolCall` (retryable) | `toolConfig.functionCallingConfig.mode = "NONE"` when tools are present | `generationConfig.maxOutputTokens` = resolved value (the old 4096 default is removed) | none (implicit caching) |
| openaicodex | `status` + `incomplete_details.reason` | `completed` → end_turn/tool_use (refusal part with no calls → refusal); `incomplete`+`max_output_tokens` → max_tokens; `incomplete`+`content_filter` → content_filter; other incomplete, `failed`, `cancelled` → error | `"tool_choice":"none"` when tools are present | never sent (backend rejects it) | `prompt_cache_key` = CacheKey |
| anthropiccompat (text only; tools in 1b) | `stop_reason` (JSON) / `message_delta.delta.stop_reason` (stream) | `end_turn`, `stop_sequence`, `pause_turn`, other → end_turn; `max_tokens`, `model_context_window_exceeded` → max_tokens; `refusal` → refusal; `tool_use` → tool_use | n/a (tools disabled until 1b) | `max_tokens` = resolved value (the old 4096 default is removed) | Stage 1b (tools breakpoint only) |

## File structure

| File | Responsibility |
|---|---|
| `internal/providers/contract.go` (modify) | `StopReason`, `ToolChoice`, `ReasoningBlock`, new Request/Response/Message fields |
| `internal/providers/stop_reason.go` (create) | `ResolveStopReason`, `StopReason.AllowsEmptyReply` |
| `internal/providers/max_output.go` (create) | `DefaultMaxOutputTokens`, `ResolveMaxOutputTokens` |
| `internal/providers/model_metadata.go` (modify) | catalog `MaxOutputTokens` |
| `internal/providers/generation_errors.go` (modify) | `ErrMalformedToolCall` (retryable) |
| `internal/providers/openai_chat_options.go` (modify) | `PromptCacheKey` option, `RetryWithoutMaxTokens` quirk |
| `internal/providers/ai/openaicompat/{chat,stream,message,types,config,models}.go` | stop reasons, tool_choice, output limit, prompt_cache_key, OpenRouter catalog limit, `stream_options.include_usage` |
| `internal/providers/ai/gemini/{adapter,types}.go` | finishReason, toolConfig, output limit, catalog limit, batched responses, error results, signatures, SSE streaming |
| `internal/providers/ai/openaicodex/{runtime,response}.go`, `factory/factory.go` | incomplete handling, tool_choice, prompt_cache_key, no output limit, encrypted reasoning |
| `internal/providers/ai/anthropiccompat/adapter.go` | stop_reason for text replies, output limit, catalog limit |
| `internal/transcript/message.go` (modify; Stage 0 Task 1 moved `ReasoningPart` there) | `ReasoningPart.RedactedData` |
| `internal/core/execution_generation.go` | `stopReasonError`, reasoning parts on the tool-step reply |
| `internal/core/context_compact.go`, `execution_request.go`, `usage.go` | stop-reason error for summaries (after the `compact` step is recorded), `CacheKey`, `generationStopReason` from `Response.StopReason` |
| `internal/core/execution_conversation.go`, `execution_tools.go` | one assistant message per step, reasoning replay, `IsError`; delete `batchAdjacentToolCallMessages`, `attachReasoningToToolCallMessage` |
| `clients/terminal/{chat/viewmodel/surface_adapter.go,ui/surface/message/message.go}`, `internal/providers/model_capabilities.go` | delete the unused `ThoughtSignature` / `ToolID` / `ResponsesData` fields, their dead surface methods, and `ModelCapabilities.ThoughtSignatures` (Task 13) |

---

### Task 1: Contract types

**Files:**
- Modify: `internal/providers/contract.go:29-54`
- Create: `internal/providers/stop_reason.go`
- Test: `internal/providers/stop_reason_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/stop_reason_test.go`:

```go
package providers

import "testing"

func TestResolveStopReasonFollowsTheReply(t *testing.T) {
	for _, tc := range []struct {
		reason    StopReason
		toolCalls int
		want      StopReason
	}{
		{"", 0, StopEndTurn},
		{StopEndTurn, 2, StopToolUse},
		{StopToolUse, 0, StopEndTurn},
		{StopMaxTokens, 1, StopMaxTokens},
		{StopContentFilter, 0, StopContentFilter},
		{StopRefusal, 0, StopRefusal},
	} {
		if got := ResolveStopReason(tc.reason, tc.toolCalls); got != tc.want {
			t.Errorf("ResolveStopReason(%q, %d) = %q, want %q", tc.reason, tc.toolCalls, got, tc.want)
		}
	}
}

func TestOnlyTruncatedOrBlockedRepliesMayBeEmpty(t *testing.T) {
	for reason, want := range map[StopReason]bool{
		StopEndTurn: false, StopToolUse: false, StopMaxTokens: true, StopRefusal: true, StopContentFilter: true,
	} {
		if got := reason.AllowsEmptyReply(); got != want {
			t.Errorf("%q.AllowsEmptyReply() = %v, want %v", reason, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers -run 'TestResolveStopReason|TestOnlyTruncated' -count=1`
Expected: FAIL at build, with `undefined: StopReason` and `undefined: ResolveStopReason`.

- [ ] **Step 3: Extend the contract**

In `internal/providers/contract.go`, replace the `Message` struct (baseline lines 29-36):

```go
type Message struct {
	Role             string
	Content          string
	ReasoningContent *string
	Images           []ImageContent
	ToolCallID       string
	ToolCalls        []ToolCall
}
```

with:

```go
type Message struct {
	Role             string
	Content          string
	ReasoningContent *string
	Reasoning        []ReasoningBlock // signed or encrypted reasoning, sent back unchanged
	Images           []ImageContent
	ToolCallID       string
	ToolCalls        []ToolCall
	IsError          bool // a failed tool result
}

// ReasoningBlock is provider reasoning that must be sent back as received.
// Signature: Anthropic thinking signature or Gemini thought signature.
// RedactedData: Anthropic redacted thinking or Responses API encrypted reasoning.
type ReasoningBlock struct {
	Text         string
	Signature    string
	RedactedData string
}
```

In `Request` (baseline lines 38-45), add these three fields after `Tools []ToolDefinition`:

```go
	MaxOutputTokens    int        // 0 = provider config, then model catalog, then DefaultMaxOutputTokens
	ToolChoice         ToolChoice // tools stay defined with ToolChoiceNone so the cached prefix survives
	CacheKey           string     // session id; adapters use it for prompt_cache_key or cache breakpoints
```

Replace the `Response` struct (baseline lines 47-54) with the version below. The `Usage` field keeps the Stage 0 type:

```go
type Response struct {
	Text             string
	ReasoningContent *string
	Model            string
	Provider         string
	Reasoning        []ReasoningBlock
	ToolCalls        []ToolCall
	StopReason       StopReason
	Usage            Usage
}

// StopReason says why generation ended, normalised across providers.
type StopReason string

const (
	StopEndTurn       StopReason = "end_turn"
	StopToolUse       StopReason = "tool_use"
	StopMaxTokens     StopReason = "max_tokens"
	StopRefusal       StopReason = "refusal"
	StopContentFilter StopReason = "content_filter"
)

// ToolChoice limits tool use for one request.
type ToolChoice string

const (
	ToolChoiceAuto ToolChoice = ""
	ToolChoiceNone ToolChoice = "none"
)
```

Create `internal/providers/stop_reason.go`:

```go
package providers

// ResolveStopReason reconciles a mapped wire reason with the reply itself:
// gateways report "stop" alongside tool calls, or "tool_calls" without any.
func ResolveStopReason(reason StopReason, toolCalls int) StopReason {
	switch reason {
	case "", StopEndTurn, StopToolUse:
		if toolCalls > 0 {
			return StopToolUse
		}
		return StopEndTurn
	default:
		return reason
	}
}

// AllowsEmptyReply reports whether a reply with neither text nor tool calls is a
// valid outcome for this reason rather than a failed generation.
func (r StopReason) AllowsEmptyReply() bool {
	return r == StopMaxTokens || r == StopRefusal || r == StopContentFilter
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `gofmt -w internal/providers/contract.go internal/providers/stop_reason.go && go test ./internal/providers -run 'TestResolveStopReason|TestOnlyTruncated' -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/contract.go internal/providers/stop_reason.go internal/providers/stop_reason_test.go
git commit -m "feat(providers): add stop reason, tool choice and reasoning carrier to the contract

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Max output resolution and catalog limit

**Files:**
- Modify: `internal/providers/model_metadata.go:17-32,34-41,43-50,59,120-121,138-145,147-150,194-197,219-226`
- Create: `internal/providers/max_output.go`
- Test: `internal/providers/max_output_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/max_output_test.go`:

```go
package providers

import "testing"

func TestResolveMaxOutputTokensOrder(t *testing.T) {
	RegisterModelMetadata("maxout-test", TypeOpenAICompat, "small-output-model", ModelMetadataRegistration{MaxOutputTokens: 8192})
	RegisterModelMetadata("maxout-test", TypeOpenAICompat, "large-output-model", ModelMetadataRegistration{MaxOutputTokens: 128000})
	for _, tc := range []struct {
		name       string
		requested  int
		configured int64
		model      string
		want       int64
	}{
		{"request wins", 40000, 2048, "small-output-model", 40000},
		{"config before catalog", 0, 2048, "small-output-model", 2048},
		{"catalog below default", 0, 0, "small-output-model", 8192},
		{"catalog above default is capped", 0, 0, "large-output-model", DefaultMaxOutputTokens},
		{"unknown model", 0, 0, "unlisted-model", DefaultMaxOutputTokens},
	} {
		if got := ResolveMaxOutputTokens(tc.requested, tc.configured, "maxout-test", TypeOpenAICompat, tc.model); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := ResolveModelMetadata("maxout-test", TypeOpenAICompat, "large-output-model").MaxOutputTokens; got != 128000 {
		t.Errorf("catalog metadata MaxOutputTokens = %d, want 128000", got)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers -run TestResolveMaxOutputTokensOrder -count=1`
Expected: FAIL at build, with `unknown field MaxOutputTokens in struct literal of type ModelMetadataRegistration` and `undefined: ResolveMaxOutputTokens`.

- [ ] **Step 3: Add the catalog field**

In `internal/providers/model_metadata.go`:

1. In `type ModelMetadata struct`, below `ContextWindow          int                 \`json:"context_window,omitempty"\``, add:
```go
	MaxOutputTokens        int                 `json:"max_output_tokens,omitempty"`
```
2. In `type ModelMetadataRegistration struct`, below `ContextWindow       int`, add:
```go
	MaxOutputTokens     int
```
3. In `type cachedModelMetadata struct`, below `ContextWindow       int      \`json:"context_window,omitempty"\``, add:
```go
	MaxOutputTokens     int      `json:"max_output_tokens,omitempty"`
```
4. In `RegisterModelMetadata`, change the start of the early-return condition from `if metadata.ContextWindow <= 0 && metadata.ToolCalling == nil` to `if metadata.ContextWindow <= 0 && metadata.MaxOutputTokens <= 0 && metadata.ToolCalling == nil` (the rest of the line stays).
5. In the `return ModelMetadata{...}` of `ResolveModelMetadata`, below `ContextWindow:          contextWindow,`, add:
```go
		MaxOutputTokens:        liveMetadata.MaxOutputTokens,
```
6. In `normalizeModelMetadataRegistration`, after the `ContextWindow < 0` block, add:
```go
	if metadata.MaxOutputTokens < 0 {
		metadata.MaxOutputTokens = 0
	}
```
7. In **both** `mergeCachedModelMetadata` and `mergeCachedMetadata`, after the `if next.ContextWindow > 0 { ... }` block, add:
```go
	if next.MaxOutputTokens > 0 {
		existing.MaxOutputTokens = next.MaxOutputTokens
	}
```
8. In `cachedModelMetadataNonZero`, change `return metadata.ContextWindow > 0 ||` to:
```go
	return metadata.ContextWindow > 0 ||
		metadata.MaxOutputTokens > 0 ||
```

Create `internal/providers/max_output.go`:

```go
package providers

// DefaultMaxOutputTokens applies when nothing more specific names an output limit.
const DefaultMaxOutputTokens int64 = 16384

// ResolveMaxOutputTokens picks the request value, then the provider config, then
// the model catalog limit when it is below the default, then the default.
func ResolveMaxOutputTokens(requested int, configured int64, providerID, providerType, modelID string) int64 {
	if requested > 0 {
		return int64(requested)
	}
	if configured > 0 {
		return configured
	}
	if limit := int64(ResolveModelMetadata(providerID, providerType, modelID).MaxOutputTokens); limit > 0 && limit < DefaultMaxOutputTokens {
		return limit
	}
	return DefaultMaxOutputTokens
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `gofmt -w internal/providers/model_metadata.go internal/providers/max_output.go && go test ./internal/providers -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/model_metadata.go internal/providers/max_output.go internal/providers/max_output_test.go
git commit -m "feat(providers): resolve max output tokens from request, config and catalog

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Core keeps today's behaviour, sets CacheKey, records stop reason

**Files:**
- Modify: `internal/core/execution_generation.go:1-31`
- Modify: `internal/core/context_compact.go:22-33`
- Modify: `internal/core/execution_request.go:18-23`
- Modify: `internal/core/usage.go` (`generationStopReason`, added by Stage 0 Task 5)
- Test: `internal/core/provider_contract_test.go`

Stage 0 records one `run_steps` row per generation: `generateAssistantTurn` calls `c.recordRunStep(ctx, turn.RunID, response, generationStopReason(response), …)` after every successful `Generate`, and `generateCompactSummary` calls `c.recordRunStep(ctx, runID, response, "compact", …)`. This task changes only `generationStopReason`, so the compaction row keeps its `compact` marker. Stage 0's `TestRunStepsCountCompactionGeneration` keeps guarding that marker and must stay green. The step row is written before `stopReasonError` runs, so a truncated reply is still recorded, as `max_tokens`.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/provider_contract_test.go`. `generationRuntimeFunc`, `newCrashRecoveryCore`, `recoveryLLMs`, `saveCrashRecoveryRun` and `assertRecoveryRunStatus` are existing helpers in `internal/core/*_test.go`.

```go
package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestTruncatedOrFilteredReplyFailsTheTurnWithoutRetry(t *testing.T) {
	for _, reason := range []providers.StopReason{providers.StopMaxTokens, providers.StopContentFilter} {
		t.Run(string(reason), func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			calls := 0
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
				calls++
				if err := providers.StreamText(ctx, "Cut off"); err != nil {
					return providers.Response{}, err
				}
				return providers.Response{Text: "Cut off", Provider: "recovery-test", StopReason: reason}, nil
			})})
			session, run := saveCrashRecoveryRun(t, db, "stop-"+string(reason), core.RunStatusAccepted, false)
			err := app.ExecuteRun(context.Background(), run.ID)
			if want := "generation stopped before completion (" + string(reason) + ")"; err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error=%v, want %q", err, want)
			}
			assertRecoveryRunStatus(t, db, run.ID, core.RunStatusFailed)
			if calls != 1 {
				t.Fatalf("model calls=%d, want 1 (not retryable)", calls)
			}
			messages, err := db.ListMessages(context.Background(), session.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			kept := false
			for _, message := range messages {
				kept = kept || (message.Role == transcript.MessageRoleAssistant && strings.Contains(message.Content, "Cut off"))
			}
			if !kept {
				t.Fatal("partial reply was lost")
			}
		})
	}
}

func TestProviderRequestCarriesTheSessionCacheKey(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var cacheKey string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		cacheKey = request.CacheKey
		return providers.Response{Text: "Done", StopReason: providers.StopEndTurn}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cache-key", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if cacheKey != session.ID {
		t.Fatalf("CacheKey=%q, want session id %q", cacheKey, session.ID)
	}
}

func TestRunStepRecordsTheProviderStopReason(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Text: "Cut off", Provider: "recovery-test", StopReason: providers.StopMaxTokens}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "run-step-stop", core.RunStatusAccepted, false)
	_ = app.ExecuteRun(context.Background(), run.ID)
	steps, err := db.ListRunSteps(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].StopReason != "max_tokens" {
		t.Fatalf("run steps=%+v, want one step with stop_reason max_tokens", steps)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/core -run 'TestTruncatedOrFilteredReply|TestProviderRequestCarriesTheSessionCacheKey|TestRunStepRecordsTheProviderStopReason' -count=1`
Expected: FAIL. `TestTruncatedOrFilteredReply…` reports `error=<nil>`, the cache-key test reports `CacheKey=""`, and the run-step test reports `stop_reason end_turn`, because Stage 0 derives it from the tool calls alone.

- [ ] **Step 3a: Convert stop reasons in the generation retry**

In `internal/core/execution_generation.go`, add `"fmt"` to the imports. In `generateAssistantTurnWithRetry`, replace:

```go
		assistant, saved, response, err := c.generateAssistantTurn(ctx, turn, request)
		if err == nil && sanitizeAssistantOutput(response.Text) == "" && len(response.ToolCalls) == 0 {
```

with:

```go
		assistant, saved, response, err := c.generateAssistantTurn(ctx, turn, request)
		if err == nil {
			err = stopReasonError(response)
		}
		if err == nil && sanitizeAssistantOutput(response.Text) == "" && len(response.ToolCalls) == 0 {
```

Add this function directly above the `// Finalize the model's commentary and usage before dispatching its tools.` comment:

```go
// stopReasonError fails a reply cut by the output limit or a content filter:
// the current loop has no continuation path for either.
func stopReasonError(response providers.Response) error {
	switch response.StopReason {
	case providers.StopMaxTokens, providers.StopContentFilter:
		return fmt.Errorf("%s: generation stopped before completion (%s)", response.Provider, response.StopReason)
	default:
		return nil
	}
}
```

- [ ] **Step 3b: Same rule for compaction summaries, and the cache key**

In `internal/core/context_compact.go` (`generateCompactSummary`), directly after Stage 0's line `c.recordRunStep(ctx, runID, response, "compact", time.Since(started))`, add the lines below. The summary's tokens are recorded first, and the `compact` marker is untouched.

```go
	if err := stopReasonError(response); err != nil {
		return "", err
	}
```

In `internal/core/execution_request.go` (`buildProviderRequest`), add `CacheKey` to the request literal:

```go
	request := providers.Request{
		RunID:              turn.RunID,
		SessionID:          turn.SessionID,
		SystemPrompt:       c.providerSystemPrompt(ctx, turn, assistant, compactSummary, effectiveHistory),
		CustomInstructions: assistant.CustomInstructions,
		CacheKey:           turn.SessionID,
	}
```

- [ ] **Step 3c: Record the stop reason in run_steps**

In `internal/core/usage.go`, replace Stage 0's `generationStopReason` with the version below. It takes the adapter's reason and falls back to the tool-call count when a runtime reports none; Stage 0's test fakes report none, so their `tool_use`/`end_turn` expectations still hold. `recordRunStep` and the `"compact"` call site do not change.

```go
func generationStopReason(response providers.Response) string {
	return string(providers.ResolveStopReason(response.StopReason, len(response.ToolCalls)))
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/core && go test ./internal/core -run 'TestTruncatedOrFilteredReply|TestProviderRequestCarriesTheSessionCacheKey|TestRunStepRecordsTheProviderStopReason' -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/core`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/core/execution_generation.go internal/core/context_compact.go internal/core/execution_request.go internal/core/usage.go internal/core/provider_contract_test.go
git commit -m "feat(core): fail truncated replies as before, send session cache key, record stop reason

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: One assistant message per tool step, reasoning replay, IsError

**Files:**
- Modify: `internal/transcript/message.go` (`type ReasoningPart struct`)
- Modify: `internal/core/execution_generation.go:35-65` (`saveAssistantToolTurn`)
- Modify: `internal/core/execution_tools.go:15-84,130-156`
- Modify: `internal/core/execution_conversation.go:20-139,299-430`
- Test: `internal/core/tool_step_conversation_test.go` (new, package `core`), `internal/core/provider_contract_test.go` (append)

- [ ] **Step 1: Write the failing tests**

Create `internal/core/tool_step_conversation_test.go`:

```go
package core

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func stepCallMessage(id string) transcript.Message {
	return transcript.Message{Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: "read", Input: `{}`}}}}
}

func stepResultMessage(id string, content string, failed bool) transcript.Message {
	return transcript.Message{Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: id, Name: "read", Content: content, IsError: failed}}}}
}

func TestToolStepIsReplayedAsOneAssistantMessage(t *testing.T) {
	stepEnd := transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}}
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Compare a and b"},
		{Role: transcript.MessageRoleAssistant, Content: "Reading both.", Parts: []transcript.MessagePart{
			{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{RedactedData: "enc-1"}},
			{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: "Reading both."}},
			stepEnd,
		}},
		stepCallMessage("a"), stepResultMessage("a", "A", false),
		stepCallMessage("b"), stepResultMessage("b", "missing", true),
		{Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{stepEnd}},
		stepCallMessage("c"), stepResultMessage("c", "C", false),
		{Role: transcript.MessageRoleAssistant, Content: "Done"},
	}
	conversation, err := buildProviderConversationWithAttachmentsForRun(context.Background(), history, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 7 {
		t.Fatalf("conversation=%+v, want user, step, 2 results, step, result, reply", conversation)
	}
	first := conversation[1]
	if first.Content != "Reading both." || len(first.ToolCalls) != 2 || first.ToolCalls[1].ID != "b" || len(first.Reasoning) != 1 || first.Reasoning[0].RedactedData != "enc-1" || first.ReasoningContent != nil {
		t.Fatalf("first step=%+v", first)
	}
	if conversation[2].ToolCallID != "a" || conversation[2].IsError || conversation[3].ToolCallID != "b" || !conversation[3].IsError {
		t.Fatalf("results=%+v %+v", conversation[2], conversation[3])
	}
	if second := conversation[4]; len(second.ToolCalls) != 1 || second.ToolCalls[0].ID != "c" || conversation[5].ToolCallID != "c" || conversation[6].Content != "Done" {
		t.Fatalf("second step and reply=%+v", conversation[4:])
	}
}

func TestPlainReasoningOnOlderToolCallsStaysReasoningContent(t *testing.T) {
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect"},
		{Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{
			{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: "thinking"}},
			{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "a", Name: "read", Input: `{}`}},
		}},
		stepResultMessage("a", "A", false),
	}
	conversation, err := buildProviderConversationWithAttachmentsForRun(context.Background(), history, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	step := conversation[1]
	if step.ReasoningContent == nil || *step.ReasoningContent != "thinking" || len(step.Reasoning) != 0 {
		t.Fatalf("step=%+v", step)
	}
}
```

Append to `internal/core/provider_contract_test.go`. Add `"github.com/Suren878/matrixclaw/internal/tools"` to its imports; `recoveryTool` and `recoveryToolSpec` are existing helpers.

```go
func TestToolStepReasoningIsSentBackWithItsCalls(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{
				Text:      "Inspecting both.",
				Reasoning: []providers.ReasoningBlock{{RedactedData: "enc-1"}},
				ToolCalls: []providers.ToolCall{
					{ID: "call-a", Name: "inspect_state", Arguments: []byte(`{}`)},
					{ID: "call-b", Name: "inspect_state", Arguments: []byte(`{}`)},
				},
				StopReason: providers.StopToolUse,
			}, nil
		}
		for i, message := range request.Messages {
			if len(message.ToolCalls) == 0 {
				continue
			}
			if message.Content != "Inspecting both." || len(message.ToolCalls) != 2 || len(message.Reasoning) != 1 || message.Reasoning[0].RedactedData != "enc-1" || message.ReasoningContent != nil {
				t.Fatalf("tool step=%+v", message)
			}
			if i+2 >= len(request.Messages) || request.Messages[i+1].ToolCallID != "call-a" || request.Messages[i+2].ToolCallID != "call-b" {
				t.Fatalf("results must follow the step in call order: %+v", request.Messages)
			}
			return providers.Response{Text: "Both inspected.", StopReason: providers.StopEndTurn}, nil
		}
		t.Fatalf("no tool step in %+v", request.Messages)
		return providers.Response{}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "reasoning-replay", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if calls != 2 || tool.callCount() != 2 {
		t.Fatalf("model calls=%d tool calls=%d", calls, tool.callCount())
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/core -run 'TestToolStepIsReplayedAsOneAssistantMessage|TestPlainReasoningOnOlderToolCalls|TestToolStepReasoningIsSentBackWithItsCalls' -count=1`
Expected: FAIL at build with `unknown field RedactedData in struct literal of type transcript.ReasoningPart`.

- [ ] **Step 3a: Add the transcript field**

In `internal/transcript/message.go`, `type ReasoningPart struct`, add this as the last field:

```go
	RedactedData     string          `json:"redacted_data,omitempty"`
```

- [ ] **Step 3b: Store reasoning on the tool-step reply**

In `internal/core/execution_generation.go` (`saveAssistantToolTurn`), replace:

```go
	assistant.Parts = transcript.NormalizeMessageParts(assistant.Content, nil)
	finish := providerUsageFinishPart(response.Usage)
```

with:

```go
	assistant.Parts = append(responseReasoningParts(response), transcript.NormalizeMessageParts(assistant.Content, nil)...)
	finish := providerUsageFinishPart(response.Usage)
```

Append to the same file:

```go
// responseReasoningParts keeps the reasoning a provider needs back with this
// tool step: plain reasoning_content text and signed or encrypted blocks.
func responseReasoningParts(response providers.Response) []transcript.MessagePart {
	var parts []transcript.MessagePart
	if response.ReasoningContent != nil {
		parts = append(parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: *response.ReasoningContent}})
	}
	for _, block := range response.Reasoning {
		parts = append(parts, transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: block.Text, Signature: block.Signature, RedactedData: block.RedactedData}})
	}
	return parts
}
```

- [ ] **Step 3c: Delete the old per-call reasoning attachment**

In `internal/core/execution_tools.go` (`executeRequestedTools`), delete this block:

```go
		if index == 0 {
			if attachErr := c.attachReasoningToToolCallMessage(ctx, result.ToolCallMessage, response.ReasoningContent); attachErr != nil {
				return false, attachErr
			}
		}
```

Change the loop header `for index, toolCall := range response.ToolCalls {` to `for _, toolCall := range response.ToolCalls {`. Then delete the whole `func (c *Core) attachReasoningToToolCallMessage(...)` (baseline lines 130-156). The reply message from Step 3b replaces it.

- [ ] **Step 3d: Build one assistant message per step**

In `internal/core/execution_conversation.go`, replace the loop in `buildProviderConversationWithAttachmentsForRun` and the `providerConversationEntry` type (baseline lines 27-56):

```go
	conversation := make([]providers.Message, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		providerMessages := entries[i].messages
		...
		conversation = appendProviderToolResults(conversation, batched.ToolCalls, toolResults)
	}
	return conversation, nil
}

type providerConversationEntry struct {
	messages []providers.Message
}
```

with:

```go
	conversation := make([]providers.Message, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		if isToolStepStart(entries[i]) {
			step, next := collectToolStep(entries, i)
			if len(step.ToolCalls) > 0 {
				conversation = append(conversation, step)
				conversation = appendProviderToolResults(conversation, step.ToolCalls, toolResults)
				i = next - 1
				continue
			}
		}
		for _, providerMessage := range entries[i].messages {
			if !isPairedToolResultMessage(providerMessage) {
				conversation = append(conversation, providerMessage)
			}
		}
	}
	return conversation, nil
}

type providerConversationEntry struct {
	source   transcript.Message
	messages []providers.Message
}

// isToolStepStart: a model response that called tools starts with its saved
// reply (finish reason tool_calls) or, in older histories, with its first call.
func isToolStepStart(entry providerConversationEntry) bool {
	return isToolStepReply(entry.source) || (len(entry.messages) == 1 && len(entry.messages[0].ToolCalls) > 0)
}

func isToolStepReply(message transcript.Message) bool {
	if message.Role != transcript.MessageRoleAssistant {
		return false
	}
	for _, part := range message.Parts {
		if part.Finish != nil && part.Finish.Reason == "tool_calls" {
			return true
		}
	}
	return false
}

// collectToolStep merges one model response (its reply, reasoning and every call
// it made) into one assistant message; results are paired after it in call order.
func collectToolStep(entries []providerConversationEntry, start int) (providers.Message, int) {
	step := providers.Message{Role: string(transcript.MessageRoleAssistant)}
	var parts []transcript.MessagePart
	i := start
collect:
	for ; i < len(entries); i++ {
		entry := entries[i]
		switch {
		case i == start:
			if len(entry.messages) == 1 {
				step.Content = entry.messages[0].Content
				step.Images = entry.messages[0].Images
				step.ToolCalls = append(step.ToolCalls, entry.messages[0].ToolCalls...)
			}
		case len(entry.messages) == 1 && isPairedToolResultMessage(entry.messages[0]):
			continue
		case !isToolStepReply(entry.source) && len(entry.messages) == 1 && isAdditionalBatchableToolCallMessage(entry.messages[0]):
			step.ToolCalls = append(step.ToolCalls, entry.messages[0].ToolCalls...)
		default:
			break collect
		}
		parts = append(parts, entry.source.Parts...)
	}
	step.ReasoningContent = messageReasoningContent(parts)
	step.Reasoning = messageReasoningBlocks(parts)
	return step, i
}
```

In `convertProviderConversationHistory`, change `entries = append(entries, providerConversationEntry{messages: providerMessages})` to:

```go
		entries = append(entries, providerConversationEntry{source: message, messages: providerMessages})
```

Delete `func batchAdjacentToolCallMessages(...)` (baseline lines 98-111); `collectToolStep` replaces it. Keep `isToolCallOnlyProviderMessage` and `isAdditionalBatchableToolCallMessage`, which are still used.

In `syntheticFailedToolResult`, add `IsError: true,` after `Content: "Tool execution failed before completion.",`.

In `toProviderMessages`:
- In both literals that set `ReasoningContent: reasoningContent,` (the tool-call return and the final return), add the line below after it:
```go
			Reasoning:        messageReasoningBlocks(message.Parts),
```
- In the tool-result literal, after `ToolCallID: strings.TrimSpace(part.ToolResult.ToolCallID),`, add:
```go
			IsError:    part.ToolResult.IsError,
```

In `messageReasoningContent`, change `if part.Reasoning == nil {` to `if part.Reasoning == nil || isSignedReasoning(*part.Reasoning) {`, and append after the function:

```go
// messageReasoningBlocks returns reasoning the provider signed or encrypted; it
// goes back unchanged, unlike plain reasoning_content text.
func messageReasoningBlocks(parts []transcript.MessagePart) []providers.ReasoningBlock {
	var blocks []providers.ReasoningBlock
	for _, part := range parts {
		if part.Reasoning != nil && isSignedReasoning(*part.Reasoning) {
			blocks = append(blocks, providers.ReasoningBlock{Text: part.Reasoning.Text, Signature: part.Reasoning.Signature, RedactedData: part.Reasoning.RedactedData})
		}
	}
	return blocks
}

func isSignedReasoning(part transcript.ReasoningPart) bool {
	return part.Signature != "" || part.RedactedData != ""
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/core internal/transcript && go test ./internal/core -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/core`. The existing `TestProviderConversationPairsResultsWithMixedTextAndToolCalls` and `TestToolTurnPersistsFinalCommentaryAndUsage` must still pass unchanged.

- [ ] **Step 5: Full suite**

Run: `go build ./... && go vet ./internal/core ./internal/transcript && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/transcript/message.go internal/core/execution_generation.go internal/core/execution_tools.go internal/core/execution_conversation.go internal/core/tool_step_conversation_test.go internal/core/provider_contract_test.go
git commit -m "feat(core): replay each model response as one assistant message with its reasoning

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: openaicompat stop reasons

**Files:**
- Modify: `internal/providers/ai/openaicompat/chat.go:317-356`
- Modify: `internal/providers/ai/openaicompat/stream.go:14-98`
- Modify: `internal/providers/ai/openaicompat/message.go:98`
- Modify: `internal/providers/ai/openaicompat/stream_test.go:27`
- Test: `internal/providers/ai/openaicompat/stop_reason_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/ai/openaicompat/stop_reason_test.go`:

```go
package openaicompat

import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func sseStream(chunks ...string) string {
	var out strings.Builder
	for _, chunk := range chunks {
		out.WriteString("data: " + chunk + "\n\n")
	}
	return out.String() + "data: [DONE]\n\n"
}

func TestStreamFinishReasonBecomesStopReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream string
		want   providers.StopReason
		text   string
		calls  []string
	}{
		{"stop", sseStream(`{"choices":[{"delta":{"content":"Done"},"finish_reason":"stop"}]}`), providers.StopEndTurn, "Done", nil},
		{"stop with tool calls", sseStream(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"stop"}]}`), providers.StopToolUse, "", []string{"c1"}},
		{"length keeps text", sseStream(`{"choices":[{"delta":{"content":"Partial"},"finish_reason":"length"}]}`), providers.StopMaxTokens, "Partial", nil},
		{"length drops truncated call", sseStream(`{"choices":[{"delta":{"content":"Reading","tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{}"}},{"index":1,"id":"c2","function":{"name":"read","arguments":"{\"pa"}}]},"finish_reason":"length"}]}`), providers.StopMaxTokens, "Reading", []string{"c1"}},
		{"length with nothing", sseStream(`{"choices":[{"delta":{},"finish_reason":"length"}]}`), providers.StopMaxTokens, "", nil},
		{"content filter", sseStream(`{"choices":[{"delta":{},"finish_reason":"content_filter"}]}`), providers.StopContentFilter, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := (&Runtime{model: "test"}).decodeStream(context.Background(), strings.NewReader(tc.stream))
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || response.Text != tc.text || len(response.ToolCalls) != len(tc.calls) {
				t.Fatalf("response=%+v, want stop=%q text=%q calls=%v", response, tc.want, tc.text, tc.calls)
			}
			for i, id := range tc.calls {
				if response.ToolCalls[i].ID != id {
					t.Fatalf("calls=%+v, want %v", response.ToolCalls, tc.calls)
				}
			}
		})
	}
}

func TestJSONFinishReasonLengthIsNotAnError(t *testing.T) {
	response, err := (&Runtime{model: "test"}).decodeChatResponse([]byte(`{"choices":[{"message":{"content":"Partial"},"finish_reason":"length"}]}`))
	if err != nil || response.StopReason != providers.StopMaxTokens || response.Text != "Partial" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
```

In `internal/providers/ai/openaicompat/stream_test.go`, `TestDecodeStreamRejectsErrorAndIncompleteToolArguments` asserts the old behaviour: `length` was an error. Delete this line (baseline 27):

```go
		`data: {"choices":[{"delta":{"content":"Partial"},"finish_reason":"length"}]}` + "\n\ndata: [DONE]\n\n",
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/providers/ai/openaicompat -run 'TestStreamFinishReason|TestJSONFinishReasonLength' -count=1`
Expected: FAIL. Cases report `openaicompat: generation stopped before completion (length)` or `response=… want stop="end_turn"`, because `StopReason` is empty.

- [ ] **Step 3: Implement**

In `chat.go` (`decodeChatResponse`), replace everything from `choice := response.Choices[0]` down to and **including** `func validateFinishReason(...) {...}` with:

```go
	choice := response.Choices[0]
	return r.finishResponse(choice.Message.Content, cloneStringPtr(choice.Message.ReasoningContent), decodeToolCalls(choice.Message.ToolCalls), choice.FinishReason, openAIUsage(response.Usage))
}

// finishResponse applies the finish reason: a reply cut by the output limit keeps
// its text and drops tool calls whose arguments were cut off.
func (r *Runtime) finishResponse(text string, reasoning *string, calls []providers.ToolCall, finishReason string, usage providers.Usage) (providers.Response, error) {
	stop := openAIStopReason(finishReason)
	if stop == providers.StopMaxTokens {
		calls = completeToolCalls(calls)
	} else if err := validateToolCalls(calls); err != nil {
		return providers.Response{}, err
	}
	text = strings.TrimSpace(text)
	stop = providers.ResolveStopReason(stop, len(calls))
	if text == "" && len(calls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("openaicompat: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{
		Text:             text,
		ReasoningContent: reasoning,
		Model:            r.model,
		Provider:         providers.TypeOpenAICompat,
		ToolCalls:        calls,
		StopReason:       stop,
		Usage:            usage,
	}, nil
}
```

`openAIUsage(usage chatCompletionUsage) providers.Usage` keeps its name and signature in Stage 0 (Task 7 rewrites only its body).

In `stream.go` (`decodeStream`):
- Below `var usage providers.Usage`, add `finishReason := ""`.
- Replace
```go
		if reason := chunk.Choices[0].FinishReason; reason != "" {
			if err := validateFinishReason(reason); err != nil {
				return err
			}
			completed = true
		}
```
with
```go
		if reason := chunk.Choices[0].FinishReason; reason != "" {
			finishReason = reason
			completed = true
		}
```
- Replace everything after the `if !completed { ... }` block, from `reply := strings.TrimSpace(text.String())` to the end of the function, with:
```go
	var responseReasoningContent *string
	if reasoningContentSeen {
		value := reasoningContent.String()
		responseReasoningContent = &value
	}
	return r.finishResponse(text.String(), responseReasoningContent, streamToolCalls(toolCalls), finishReason, usage)
}
```

In `message.go`, add above `func normalizeOpenAIRole`:

```go
func openAIStopReason(reason string) providers.StopReason {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens":
		return providers.StopMaxTokens
	case "content_filter":
		return providers.StopContentFilter
	case "tool_calls", "function_call":
		return providers.StopToolUse
	default:
		return providers.StopEndTurn
	}
}

// completeToolCalls keeps the calls a truncated reply finished before the limit.
func completeToolCalls(calls []providers.ToolCall) []providers.ToolCall {
	var out []providers.ToolCall
	for _, call := range calls {
		if strings.TrimSpace(call.Name) != "" && json.Valid(call.Arguments) {
			out = append(out, call)
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers/ai/openaicompat && go vet ./internal/providers/ai/openaicompat && go test ./internal/providers/ai/openaicompat -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/openaicompat`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/ai/openaicompat/chat.go internal/providers/ai/openaicompat/stream.go internal/providers/ai/openaicompat/message.go internal/providers/ai/openaicompat/stream_test.go internal/providers/ai/openaicompat/stop_reason_test.go
git commit -m "feat(openaicompat): map finish_reason to stop reason; length is no longer an error

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: openaicompat tool_choice, output limit, prompt_cache_key, catalog limit

**Files:**
- Modify: `internal/providers/openai_chat_options.go:16-26,28-41,51-57`
- Modify: `internal/providers/ai/openaicompat/types.go:5-14`
- Modify: `internal/providers/ai/openaicompat/config.go:30-42,61-73`
- Modify: `internal/providers/ai/openaicompat/chat.go:22-118,215,233-270`
- Modify: `internal/providers/ai/openaicompat/models.go:62-121`
- Test: `internal/providers/openai_chat_options_test.go`, `internal/providers/ai/openaicompat/request_controls_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/providers/openai_chat_options_test.go`:

```go
package providers

import "testing"

func TestPromptCacheKeyOnlyForOpenAIEndpoint(t *testing.T) {
	openai := ResolveOpenAIChatOptions(ProfileForModel("openai", TypeOpenAICompat, "gpt-5.4"), "https://api.openai.com/v1", "gpt-5.4")
	router := ResolveOpenAIChatOptions(ProfileForModel("openrouter", TypeOpenAICompat, "openai/gpt-5.4"), "https://openrouter.ai/api/v1", "openai/gpt-5.4")
	if !openai.PromptCacheKey || router.PromptCacheKey {
		t.Fatalf("prompt_cache_key openai=%v openrouter=%v, want true/false", openai.PromptCacheKey, router.PromptCacheKey)
	}
}
```

Create `internal/providers/ai/openaicompat/request_controls_test.go`:

```go
package openaicompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func captureChatServer(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRequestControlsReachTheWire(t *testing.T) {
	var bodies []map[string]any
	server := captureChatServer(t, &bodies)
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "controls-model"})
	if err != nil {
		t.Fatal(err)
	}
	tools := []providers.ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	messages := []providers.Message{{Role: "user", Content: "hello"}}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, Tools: tools, ToolChoice: providers.ToolChoiceNone, MaxOutputTokens: 3000, CacheKey: "session-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, Tools: tools, CacheKey: "session-1"}); err != nil {
		t.Fatal(err)
	}
	if bodies[0]["tool_choice"] != "none" || bodies[0]["max_tokens"] != float64(3000) {
		t.Fatalf("final-turn request=%v", bodies[0])
	}
	if _, ok := bodies[1]["tool_choice"]; ok || bodies[1]["max_tokens"] != float64(providers.DefaultMaxOutputTokens) {
		t.Fatalf("default request=%v", bodies[1])
	}
	if _, ok := bodies[0]["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key sent to a gateway not known to accept it: %v", bodies[0])
	}
}

func TestPromptCacheKeyUsesTheSessionKey(t *testing.T) {
	request := providers.Request{CacheKey: "session-1", Messages: []providers.Message{{Role: "user", Content: "hello"}}}
	if got := (&Runtime{model: "gpt-5.4", promptCacheKey: true}).chatPayload(context.Background(), request).PromptCacheKey; got != "session-1" {
		t.Fatalf("prompt_cache_key=%q, want session-1", got)
	}
}

func TestGenerateDropsARejectedOutputLimit(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if _, limited := body["max_tokens"]; limited {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid max_tokens value, the valid range of max_tokens is [1, 8192]"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "small-limit-model"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil || response.Text != "ok" || len(bodies) != 2 {
		t.Fatalf("response=%+v err=%v requests=%d", response, err, len(bodies))
	}
}

func TestListModelsRegistersOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/maxout-model","context_length":200000,"top_provider":{"max_completion_tokens":8000}}]}`))
	}))
	defer server.Close()
	if _, err := ListModels(context.Background(), Config{ProviderID: "maxout-router", APIKey: "test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if got := providers.ResolveModelMetadata("maxout-router", providers.TypeOpenAICompat, "vendor/maxout-model").MaxOutputTokens; got != 8000 {
		t.Fatalf("catalog max output=%d, want 8000", got)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/providers ./internal/providers/ai/openaicompat -run 'PromptCacheKey|RequestControls|RejectedOutputLimit|RegistersOutputLimit' -count=1`
Expected: FAIL at build with `openai.PromptCacheKey undefined` and `unknown field promptCacheKey in struct literal of type Runtime`.

- [ ] **Step 3a: Options**

In `internal/providers/openai_chat_options.go`:
- Add this as the last field of `OpenAIChatOptions`:
```go
	PromptCacheKey bool // send prompt_cache_key; only OpenAI's own endpoint is known to accept it
```
- Add this as the last field of `OpenAIChatRequestQuirks`:
```go
	RetryWithoutMaxTokens           bool
```
- In `ResolveOpenAIChatOptions`, before `return options`, add:
```go
	options.PromptCacheKey = openAICompatibleHost(baseURL, "api.openai.com")
```
- In `normalizeOpenAIChatRequestQuirks`, add `RetryWithoutMaxTokens: true,` to the returned literal.

- [ ] **Step 3b: Runtime and payload**

`internal/providers/ai/openaicompat/types.go`: in `chatCompletionRequest`, below `ReasoningEffort`, add:

```go
	PromptCacheKey      string                  `json:"prompt_cache_key,omitempty"`
```

`internal/providers/ai/openaicompat/config.go`:
- In `Runtime`, add `metadataID string` below `model string` and `promptCacheKey bool` below `useCompletionMax bool`.
- In the `New` literal, add `metadataID: firstNonEmptyString(cfg.ProviderID, cfg.CatalogID),` below `model: model,` and `promptCacheKey: chatOptions.PromptCacheKey,` below `useCompletionMax: ...`. `firstNonEmptyString` already exists in `local_context.go`.

`internal/providers/ai/openaicompat/chat.go`, `chatPayload`: replace

```go
	if r.maxOutputTokens > 0 {
		if r.useCompletionMax {
			payload.MaxCompletionTokens = &r.maxOutputTokens
		} else {
			payload.MaxTokens = &r.maxOutputTokens
		}
	}
	payload.Tools = encodeTools(request.Tools)
```

with

```go
	maxTokens := providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxOutputTokens, r.metadataID, providers.TypeOpenAICompat, r.model)
	if r.useCompletionMax {
		payload.MaxCompletionTokens = &maxTokens
	} else {
		payload.MaxTokens = &maxTokens
	}
	payload.Tools = encodeTools(request.Tools)
	if request.ToolChoice == providers.ToolChoiceNone && len(payload.Tools) > 0 {
		payload.ToolChoice = string(providers.ToolChoiceNone)
	}
	if r.promptCacheKey {
		payload.PromptCacheKey = strings.TrimSpace(request.CacheKey)
	}
```

The `ToolChoice string` field in `chatCompletionRequest` already exists but was unused until now.

- [ ] **Step 3c: Retry once without the limit**

In `Generate`, below `retriedWithReasoningContent := false`, add `retriedWithoutMaxTokens := false`. Directly before `if r.quirks.RetryAssistantReasoningContent && !retriedWithReasoningContent ...`, insert:

```go
			if r.quirks.RetryWithoutMaxTokens && !retriedWithoutMaxTokens && shouldRetryWithoutMaxTokens(payload, httpRes.StatusCode, resBody) {
				payload.MaxTokens = nil
				payload.MaxCompletionTokens = nil
				body, err = json.Marshal(payload)
				if err != nil {
					return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
				}
				retriedWithoutMaxTokens = true
				attempt = -1
				continue
			}
```

Above `func shouldRetryStatus`, add:

```go
// shouldRetryWithoutMaxTokens: a gateway that rejects the limit's value, not the
// field, still serves the request with its own default limit.
func shouldRetryWithoutMaxTokens(payload chatCompletionRequest, statusCode int, body []byte) bool {
	if (payload.MaxTokens == nil && payload.MaxCompletionTokens == nil) || statusCode < 400 || statusCode >= 500 {
		return false
	}
	text := strings.ToLower(decodeOpenAIError(statusCode, body) + "\n" + string(body))
	if !strings.Contains(text, "max_tokens") && !strings.Contains(text, "max_completion_tokens") {
		return false
	}
	for _, marker := range []string{"range", "exceed", "too large", "less than or equal", "at most", "maximum"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
```

The field-rename retry (`shouldRetryWithMaxCompletionTokens`) runs first, so "unsupported parameter" errors still switch fields instead of dropping the limit.

- [ ] **Step 3d: OpenRouter catalog limit**

In `models.go` (`ListModels`), add this as the last field of the anonymous `Data []struct` (after `Architecture`):

```go
			TopProvider struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
```

and in the `providers.ModelMetadataRegistration{...}` literal, below `ContextWindow: contextWindow,`, add:

```go
				MaxOutputTokens:     item.TopProvider.MaxCompletionTokens,
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers && go vet ./internal/providers/... && go test ./internal/providers ./internal/providers/ai/openaicompat -count=1`
Expected: both packages `ok`.

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/openai_chat_options.go internal/providers/openai_chat_options_test.go internal/providers/ai/openaicompat/types.go internal/providers/ai/openaicompat/config.go internal/providers/ai/openaicompat/chat.go internal/providers/ai/openaicompat/models.go internal/providers/ai/openaicompat/request_controls_test.go
git commit -m "feat(openaicompat): send tool_choice none, resolved output limit and OpenAI prompt_cache_key

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6b: openaicompat streamed replies report usage

In a streamed chat completion, usage is sent only when the request says `stream_options: {"include_usage": true}`. The final chunk then carries `choices: []` plus `usage`, and `decodeStream` already reads it. Today the TUI streams and never asks, so streamed replies record zero tokens in `run_steps`. The provider profile has no per-gateway parameter allow-list. For optional parameters it uses request quirks that send the parameter and, on a 4xx naming it, retry once without it (`RetryUnsupportedReasoningEffort`, `RetryMaxTokensField`). `stream_options` follows the same pattern with a new quirk `RetryWithoutStreamOptions`, enabled for every openai-compatible profile like the others. OpenAI, OpenRouter, DeepSeek and xAI accept the parameter; a gateway that rejects it costs one retry per request.

**Files:**
- Modify: `internal/providers/openai_chat_options.go` (`OpenAIChatRequestQuirks`, `normalizeOpenAIChatRequestQuirks`)
- Modify: `internal/providers/ai/openaicompat/types.go` (`chatCompletionRequest`)
- Modify: `internal/providers/ai/openaicompat/chat.go` (`Generate`, `chatPayload`)
- Test: `internal/providers/ai/openaicompat/stream_usage_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/providers/ai/openaicompat/stream_usage_test.go`. It uses `sseStream` from Task 5.

```go
package openaicompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestStreamedRequestAsksForUsage(t *testing.T) {
	var options []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		options = append(options, body["stream_options"])
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseStream(
			`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":2}}`,
		)))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "stream-model"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithTextStream(context.Background(), func(string) error { return nil })
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"include_usage": true}; len(options) != 1 || !mapsEqual(options[0], want) {
		t.Fatalf("stream_options=%v, want %v", options, want)
	}
	if response.Usage.PromptTokens != 12 || response.Usage.OutputTokens != 2 {
		t.Fatalf("streamed reply usage=%+v", response.Usage)
	}
}

func TestGenerateRetriesWithoutRejectedStreamOptions(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, asked := body["stream_options"]; asked {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unrecognized request argument supplied: stream_options"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseStream(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "strict-gateway"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithTextStream(context.Background(), func(string) error { return nil })
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil || response.Text != "ok" || calls != 2 {
		t.Fatalf("response=%+v err=%v calls=%d", response, err, calls)
	}
}

func mapsEqual(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for key, value := range want {
		if m[key] != value {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/providers/ai/openaicompat -run 'TestStreamedRequestAsksForUsage|TestGenerateRetriesWithoutRejectedStreamOptions' -count=1`
Expected: FAIL with `stream_options=[<nil>], want map[include_usage:true]` and `calls=1`.

- [ ] **Step 3: Implement**

`internal/providers/openai_chat_options.go`: add `RetryWithoutStreamOptions bool` as the last field of `OpenAIChatRequestQuirks`, and `RetryWithoutStreamOptions: true,` to the literal in `normalizeOpenAIChatRequestQuirks`.

`internal/providers/ai/openaicompat/types.go`: add this below `Stream` in `chatCompletionRequest`:

```go
	StreamOptions       *chatStreamOptions      `json:"stream_options,omitempty"`
```

and this after the struct:

```go
type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}
```

`internal/providers/ai/openaicompat/chat.go`:
- In `chatPayload`, replace
```go
	if providers.TextStreamFromContext(ctx) != nil {
		payload.Stream = true
	}
```
with
```go
	if providers.TextStreamFromContext(ctx) != nil {
		payload.Stream = true
		// Streamed chat completions report usage only when asked to.
		payload.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}
```
- In `Generate`, add `retriedWithoutStreamOptions := false` below `retriedWithoutMaxTokens := false`. Directly before the `if r.quirks.RetryWithoutMaxTokens && …` block from Task 6, insert:
```go
			if r.quirks.RetryWithoutStreamOptions && !retriedWithoutStreamOptions && shouldRetryWithoutStreamOptions(payload, httpRes.StatusCode, resBody) {
				payload.StreamOptions = nil
				body, err = json.Marshal(payload)
				if err != nil {
					return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
				}
				retriedWithoutStreamOptions = true
				attempt = -1
				continue
			}
```
- Above `// shouldRetryWithoutMaxTokens:` add:
```go
func shouldRetryWithoutStreamOptions(payload chatCompletionRequest, statusCode int, body []byte) bool {
	if payload.StreamOptions == nil || statusCode < 400 || statusCode >= 500 {
		return false
	}
	text := strings.ToLower(decodeOpenAIError(statusCode, body) + "\n" + string(body))
	return strings.Contains(text, "stream_options") || strings.Contains(text, "include_usage")
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers && go vet ./internal/providers/... && go test ./internal/providers/... -count=1`
Expected: all provider packages `ok`. This includes `TestGenerateAcceptsJSONWhenGatewayIgnoresStreamRequest` and Stage 0's `TestUsageIsNormalised`.

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/openai_chat_options.go internal/providers/ai/openaicompat/types.go internal/providers/ai/openaicompat/chat.go internal/providers/ai/openaicompat/stream_usage_test.go
git commit -m "feat(openaicompat): ask streamed completions to report usage

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Gemini finishReason

**Files:**
- Modify: `internal/providers/generation_errors.go:10-26`
- Modify: `internal/providers/ai/gemini/types.go:54-59`
- Modify: `internal/providers/ai/gemini/adapter.go:326-365`
- Test: `internal/providers/ai/gemini/finish_reason_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/ai/gemini/finish_reason_test.go`:

```go
package gemini

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func generateAgainst(t *testing.T, reply string) (providers.Response, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	return runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
}

func TestFinishReasonBecomesStopReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		want  providers.StopReason
		text  string
	}{
		{"stop", `{"candidates":[{"content":{"role":"model","parts":[{"text":"Done"}]},"finishReason":"STOP"}]}`, providers.StopEndTurn, "Done"},
		{"stop with call", `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{}}}]},"finishReason":"STOP"}]}`, providers.StopToolUse, ""},
		{"max tokens keeps text", `{"candidates":[{"content":{"role":"model","parts":[{"text":"Partial"}]},"finishReason":"MAX_TOKENS"}]}`, providers.StopMaxTokens, "Partial"},
		{"max tokens spent on thinking", `{"candidates":[{"content":{"role":"model"},"finishReason":"MAX_TOKENS"}]}`, providers.StopMaxTokens, ""},
		{"safety", `{"candidates":[{"content":{"role":"model"},"finishReason":"SAFETY"}]}`, providers.StopContentFilter, ""},
		{"prohibited", `{"candidates":[{"content":{"role":"model"},"finishReason":"PROHIBITED_CONTENT"}]}`, providers.StopContentFilter, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := generateAgainst(t, tc.reply)
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || response.Text != tc.text {
				t.Fatalf("response=%+v, want stop=%q text=%q", response, tc.want, tc.text)
			}
		})
	}
}

func TestMalformedFunctionCallIsRetryable(t *testing.T) {
	_, err := generateAgainst(t, `{"candidates":[{"content":{"role":"model"},"finishReason":"MALFORMED_FUNCTION_CALL"}]}`)
	if !errors.Is(err, providers.ErrMalformedToolCall) || !providers.IsRetryableGenerationError(err) {
		t.Fatalf("error=%v, want retryable ErrMalformedToolCall", err)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers/ai/gemini -run 'TestFinishReason|TestMalformedFunctionCall' -count=1`
Expected: FAIL at build with `undefined: providers.ErrMalformedToolCall`.

- [ ] **Step 3: Implement**

`internal/providers/generation_errors.go`: add to the `var (...)` block:

```go
	ErrMalformedToolCall  = errors.New("model produced a malformed tool call")
```

and in `IsRetryableGenerationError` add `errors.Is(err, ErrMalformedToolCall) ||` to the condition that already lists `ErrEmptyResponse` and `ErrIncompleteResponse`:

```go
	if errors.Is(err, ErrEmptyResponse) || errors.Is(err, ErrIncompleteResponse) || errors.Is(err, ErrMalformedToolCall) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
```

`internal/providers/ai/gemini/types.go`: in `generateContentResponse`, replace the candidate struct with:

```go
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason,omitempty"`
	} `json:"candidates"`
```

`internal/providers/ai/gemini/adapter.go` (`decodeGenerateResponse`):
- Directly before `var text strings.Builder`, insert:
```go
	candidate := payload.Candidates[0]
	stop, err := geminiStopReason(candidate.FinishReason)
	if err != nil {
		return providers.Response{}, err
	}
```
- Change `for i, part := range payload.Candidates[0].Content.Parts {` to `for i, part := range candidate.Content.Parts {`.
- Replace the tail from `reply := strings.TrimSpace(text.String())` to the end of the function with the code below. Keep the Stage 0 usage call exactly as it is.
```go
	reply := strings.TrimSpace(text.String())
	stop = providers.ResolveStopReason(stop, len(toolCalls))
	if reply == "" && len(toolCalls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("gemini: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{
		Text:       reply,
		Model:      r.model,
		Provider:   providers.TypeGemini,
		ToolCalls:  toolCalls,
		StopReason: stop,
		Usage:      geminiUsage(payload.UsageMetadata),
	}, nil
}

// geminiStopReason maps finishReason; a malformed or unexpected function call is
// a retryable generation failure, not a reply.
func geminiStopReason(reason string) (providers.StopReason, error) {
	switch reason {
	case "MAX_TOKENS":
		return providers.StopMaxTokens, nil
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		return providers.StopContentFilter, nil
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
		return "", fmt.Errorf("gemini: %s: %w", strings.ToLower(reason), providers.ErrMalformedToolCall)
	default:
		return providers.StopEndTurn, nil
	}
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers && go test ./internal/providers ./internal/providers/ai/gemini -count=1`
Expected: both `ok`.

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/generation_errors.go internal/providers/ai/gemini/types.go internal/providers/ai/gemini/adapter.go internal/providers/ai/gemini/finish_reason_test.go
git commit -m "feat(gemini): decode finishReason; malformed function calls are retryable

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Gemini toolConfig NONE, output limit, catalog limit

**Files:**
- Modify: `internal/providers/ai/gemini/types.go:5-10,69-76`
- Modify: `internal/providers/ai/gemini/adapter.go:18-21,40-48,60-71,121-127,210-236,367-389`
- Test: `internal/providers/ai/gemini/request_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/ai/gemini/request_test.go`:

```go
package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func capturePayloads(t *testing.T, requests ...providers.Request) []generateContentRequest {
	t.Helper()
	var payloads []generateContentRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload generateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		if _, err := runtime.Generate(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	return payloads
}

func TestToolChoiceNoneAndOutputLimitReachTheWire(t *testing.T) {
	tools := []providers.ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	messages := []providers.Message{{Role: "user", Content: "hello"}}
	payloads := capturePayloads(t,
		providers.Request{Messages: messages, Tools: tools, ToolChoice: providers.ToolChoiceNone},
		providers.Request{Messages: messages, Tools: tools, MaxOutputTokens: 5000},
	)
	final, normal := payloads[0], payloads[1]
	if final.ToolConfig == nil || final.ToolConfig.FunctionCallingConfig.Mode != "NONE" || len(final.Tools) != 1 {
		t.Fatalf("final turn must keep tools and set mode NONE: %+v", final)
	}
	if final.GenerationConfig.MaxOutputTokens != providers.DefaultMaxOutputTokens {
		t.Fatalf("default maxOutputTokens=%d", final.GenerationConfig.MaxOutputTokens)
	}
	if normal.ToolConfig != nil || normal.GenerationConfig.MaxOutputTokens != 5000 {
		t.Fatalf("normal turn=%+v", normal)
	}
}

func TestListModelsRegistersOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-maxout-test","supportedGenerationMethods":["generateContent"],"inputTokenLimit":1048576,"outputTokenLimit":8192}]}`))
	}))
	defer server.Close()
	if _, err := ListModels(context.Background(), Config{ProviderID: "gemini-maxout", APIKey: "test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	metadata := providers.ResolveModelMetadata("gemini-maxout", providers.TypeGemini, "models/gemini-maxout-test")
	if metadata.MaxOutputTokens != 8192 || metadata.ContextWindow != 1048576 {
		t.Fatalf("metadata=%+v", metadata)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers/ai/gemini -run 'TestToolChoiceNone|TestListModelsRegistersOutputLimit' -count=1`
Expected: FAIL at build with `final.ToolConfig undefined (type generateContentRequest has no field or method ToolConfig)`.

- [ ] **Step 3: Implement**

`types.go`: replace `generateContentRequest` with the version below and add the two types:

```go
type generateContentRequest struct {
	SystemInstruction *geminiContent    `json:"systemInstruction,omitempty"`
	Contents          []geminiContent   `json:"contents"`
	Tools             []geminiTool      `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig `json:"toolConfig,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig geminiFunctionCallingConfig `json:"functionCallingConfig"`
}

type geminiFunctionCallingConfig struct {
	Mode string `json:"mode"`
}
```

In `geminiModelsResponse`, below `InputTokenLimit`, add:

```go
		OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"`
```

`adapter.go`:
- Delete `const defaultMaxOutputTokens = 4096`, which is all that remains of the const block after Stage 0 Task 8.
- In `Runtime`, add `providerID string` below `model string`. In `New`'s literal, add `providerID: metadataProviderID(cfg),` below `model: model,`.
- In `normalizeConfig`, replace
```go
	maxOutputTokens := cfg.MaxOutputTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = defaultMaxOutputTokens
	}
```
with `maxOutputTokens := cfg.MaxOutputTokens`.
- Above `func modelResource`, add:
```go
func metadataProviderID(cfg Config) string {
	if id := strings.TrimSpace(cfg.ProviderID); id != "" {
		return id
	}
	return strings.TrimSpace(cfg.CatalogID)
}
```
- In `generatePayload`, set the limit:
```go
		GenerationConfig: &generationConfig{
			MaxOutputTokens: providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxOutputTokens, r.providerID, providers.TypeGemini, r.model),
		},
```
and replace the tools tail with:
```go
	functions := encodeTools(request.Tools)
	if len(functions) > 0 {
		payload.Tools = []geminiTool{{FunctionDeclarations: functions}}
		if request.ToolChoice == providers.ToolChoiceNone {
			payload.ToolConfig = &geminiToolConfig{FunctionCallingConfig: geminiFunctionCallingConfig{Mode: "NONE"}}
		}
	}
	return payload
}
```
- In `ListModels`, replace the two `providers.RegisterContextWindowTokens(...)` lines with:
```go
					metadata := providers.ModelMetadataRegistration{ContextWindow: item.InputTokenLimit, MaxOutputTokens: item.OutputTokenLimit}
					providers.RegisterModelMetadata(cfg.ProviderID, providers.TypeGemini, name, metadata)
					providers.RegisterModelMetadata(cfg.CatalogID, providers.TypeGemini, name, metadata)
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers/ai/gemini && go vet ./internal/providers/ai/gemini && go test ./internal/providers/ai/gemini -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/gemini`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/ai/gemini/types.go internal/providers/ai/gemini/adapter.go internal/providers/ai/gemini/request_test.go
git commit -m "feat(gemini): tool config NONE, resolved output limit, catalog output limit

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Gemini batched function responses, error results, thought signatures

**Files:**
- Modify: `internal/providers/ai/gemini/types.go:21-27`
- Modify: `internal/providers/ai/gemini/adapter.go:223-230,238-277,310,335-352`
- Test: `internal/providers/ai/gemini/replay_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/ai/gemini/replay_test.go`. It uses `capturePayloads` from Task 8.

```go
package gemini

import (
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestOneToolStepIsSentBackAsOneModelAndOneUserContent(t *testing.T) {
	payloads := capturePayloads(t, providers.Request{Messages: []providers.Message{
		{Role: "user", Content: "Compare a and b."},
		{Role: "assistant", Content: "Reading both files.", Reasoning: []providers.ReasoningBlock{{Signature: "sig-a"}}, ToolCalls: []providers.ToolCall{
			{ID: "g1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
			{ID: "g2", Name: "read", Arguments: json.RawMessage(`{"path":"b"}`)},
		}},
		{Role: "tool", ToolCallID: "g1", Content: "A"},
		{Role: "tool", ToolCallID: "g2", Content: "no such file", IsError: true},
	}})
	contents := payloads[0].Contents
	if len(contents) != 3 {
		t.Fatalf("contents=%+v, want user, model, user", contents)
	}
	model, results := contents[1], contents[2]
	if model.Role != "model" || len(model.Parts) != 3 || model.Parts[0].Text != "Reading both files." {
		t.Fatalf("model content=%+v", model)
	}
	if model.Parts[1].FunctionCall == nil || model.Parts[1].ThoughtSignature != "sig-a" || model.Parts[2].ThoughtSignature != "" {
		t.Fatalf("thought signature must ride on the first call only: %+v", model.Parts)
	}
	if results.Role != "user" || len(results.Parts) != 2 {
		t.Fatalf("function responses=%+v, want both in one content", results)
	}
	first, second := results.Parts[0].FunctionResponse, results.Parts[1].FunctionResponse
	if first == nil || first.Name != "read" || first.Response["content"] != "A" {
		t.Fatalf("first response=%+v", first)
	}
	if second == nil || second.Response["error"] != "no such file" {
		t.Fatalf("failed call must be reported as an error: %+v", second)
	}
}

func TestThoughtSignatureIsReturnedAsReasoning(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{"path":"a"}},"thoughtSignature":"sig-a"},{"functionCall":{"name":"read","args":{"path":"b"}}}]},"finishReason":"STOP"}]}`)
	response, err := (&Runtime{}).decodeGenerateResponse(providers.Request{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 2 || len(response.Reasoning) != 1 || response.Reasoning[0].Signature != "sig-a" {
		t.Fatalf("response=%+v", response)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers/ai/gemini -run 'TestOneToolStep|TestThoughtSignature' -count=1`
Expected: FAIL at build with `model.Parts[1].ThoughtSignature undefined (type geminiPart has no field or method ThoughtSignature)`.

- [ ] **Step 3: Implement**

`types.go`: in `geminiPart`, below `Thought bool`, add:

```go
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
```

`adapter.go`:
- In `generatePayload`, change `payload.Contents = append(payload.Contents, content)` to `payload.Contents = appendGeminiContent(payload.Contents, content)`.
- In `encodeMessage`, replace the `if len(message.ToolCalls) > 0 { ... }` block with:
```go
	if len(message.ToolCalls) > 0 {
		parts := make([]geminiPart, 0, len(message.ToolCalls)+1)
		if content := strings.TrimSpace(message.Content); content != "" {
			parts = append(parts, geminiPart{Text: content})
		}
		signature := thoughtSignature(message.Reasoning)
		for _, toolCall := range message.ToolCalls {
			name := strings.TrimSpace(toolCall.Name)
			if name == "" {
				continue
			}
			id := strings.TrimSpace(toolCall.ID)
			if id != "" {
				toolNames[id] = name
			}
			// Gemini signs the first function call of a step.
			parts = append(parts, geminiPart{
				FunctionCall: &geminiFunctionCall{
					Name: name,
					Args: rawObject(toolCall.Arguments),
				},
				ThoughtSignature: signature,
			})
			signature = ""
		}
		return geminiContent{Role: "model", Parts: parts}
	}
```
- In the tool-result branch of `encodeMessage`, replace the `return geminiContent{...FunctionResponse...}` with:
```go
		key := "content"
		if message.IsError {
			key = "error"
		}
		return geminiContent{
			Role: "user",
			Parts: []geminiPart{{
				FunctionResponse: &geminiFunctionResponse{
					Name:     name,
					Response: map[string]any{key: strings.TrimSpace(message.Content)},
				},
			}},
		}
```
- Above `func encodeTools`, add:
```go
// appendGeminiContent sends all function responses of one step in one content.
func appendGeminiContent(contents []geminiContent, next geminiContent) []geminiContent {
	if n := len(contents); n > 0 && functionResponsesOnly(contents[n-1]) && functionResponsesOnly(next) {
		contents[n-1].Parts = append(contents[n-1].Parts, next.Parts...)
		return contents
	}
	return append(contents, next)
}

func functionResponsesOnly(content geminiContent) bool {
	for _, part := range content.Parts {
		if part.FunctionResponse == nil {
			return false
		}
	}
	return len(content.Parts) > 0
}

func thoughtSignature(blocks []providers.ReasoningBlock) string {
	for _, block := range blocks {
		if block.Signature != "" {
			return block.Signature
		}
	}
	return ""
}
```
- In `decodeGenerateResponse`, add `var reasoning []providers.ReasoningBlock` below `var toolCalls []providers.ToolCall`. Inside `if part.FunctionCall != nil && ... {`, before `toolCalls = append(...)`, add:
```go
			if part.ThoughtSignature != "" && len(reasoning) == 0 {
				reasoning = []providers.ReasoningBlock{{Signature: part.ThoughtSignature}}
			}
```
and add `Reasoning:  reasoning,` to the returned `providers.Response` literal (below `ToolCalls:`).

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers/ai/gemini && go vet ./internal/providers/ai/gemini && go test ./internal/providers/ai/gemini -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/gemini`. The existing `TestRepeatedGeminiToolInLaterTurnGetsDistinctCallID` still passes.

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/ai/gemini/types.go internal/providers/ai/gemini/adapter.go internal/providers/ai/gemini/replay_test.go
git commit -m "feat(gemini): batch function responses, report failed calls as errors, round-trip thought signatures

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9b: Gemini streams generation over SSE

Gemini still calls `:generateContent`, which sends its headers only after the whole generation. Stage 0's 120 s idle timeout therefore never applies, and the TUI gets the whole reply in one delta. `:streamGenerateContent?alt=sse` sends one `GenerateContentResponse` per SSE event: text and function-call parts arrive in order, and the last chunk carries `finishReason` and the final `usageMetadata`. The stream has no terminal event of its own (unlike `[DONE]` or `message_stop`), so, consistent with the other SSE readers, **a stream that ends without a chunk carrying `finishReason` is `ErrIncompleteResponse`** (retryable). An `{"error": …}` chunk fails the call. A non-2xx status is still read whole and retried on 5xx before any output exists. `decodeGenerateResponse` is replaced by `decodeStream`; no other caller remains.

**Files:**
- Modify: `internal/providers/ai/gemini/types.go` (`generateContentResponse`)
- Modify: `internal/providers/ai/gemini/adapter.go` (`New` endpoint, `Generate`, `decodeGenerateResponse` → `decodeStream` + `reply`)
- Modify tests that decoded the JSON endpoint: `internal/providers/ai/gemini/adapter_test.go`, `usage_test.go` (Stage 0), `finish_reason_test.go` (Task 7), `request_test.go` (Task 8), `replay_test.go` (Task 9)
- Test: `internal/providers/ai/gemini/stream_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/ai/gemini/stream_test.go`:

```go
package gemini

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestGenerateStreamsDeltasUntilFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("request went to %s, want the SSE streaming endpoint", r.URL)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":2}}`,
		} {
			_, _ = w.Write([]byte("data: " + chunk + "\r\n\r\n"))
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	var preview strings.Builder
	ctx := providers.WithTextStream(context.Background(), func(delta string) error {
		preview.WriteString(delta)
		return nil
	})
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.String() != "Hello" || response.Text != "Hello" || response.StopReason != providers.StopEndTurn || response.Usage.PromptTokens != 12 {
		t.Fatalf("preview=%q response=%+v", preview.String(), response)
	}
}

func TestStreamIsIncompleteWithoutFinishReasonAndFailsOnErrorChunk(t *testing.T) {
	cut := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Partial"}]}}]}` + "\n\n"
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(cut)); !errors.Is(err, providers.ErrIncompleteResponse) {
		t.Fatalf("cut stream error=%v, want ErrIncompleteResponse", err)
	}
	failed := `data: {"error":{"code":429,"message":"quota exceeded"}}` + "\n\n"
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(failed)); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("error chunk=%v", err)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers/ai/gemini -run 'TestGenerateStreams|TestStreamIsIncomplete' -count=1`
Expected: FAIL at build with `(&Runtime{}).decodeStream undefined`.

- [ ] **Step 3: Implement**

`types.go`: add this as the last field of `generateContentResponse`:

```go
	Error         *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
```

`adapter.go`:
- In `New`, change the endpoint suffix `":generateContent"` to `":streamGenerateContent?alt=sse"`.
- In `Generate`, change `httpReq.Header.Set("Accept", "application/json")` to `httpReq.Header.Set("Accept", "text/event-stream")`. Replace everything from `resBody, err := io.ReadAll(httpRes.Body)` to the end of the loop body with the code below. The error body is read only for non-2xx, and the success body is streamed:
```go
		if httpRes.StatusCode < 200 || httpRes.StatusCode >= 300 {
			resBody, err := io.ReadAll(httpRes.Body)
			_ = httpRes.Body.Close()
			if err != nil {
				return providers.Response{}, fmt.Errorf("gemini: read response: %w", err)
			}
			if shouldRetryStatus(httpRes.StatusCode) && attempt < len(transientRetryBackoffs) {
				if err := waitForRetry(ctx, transientRetryBackoffs[attempt]); err != nil {
					return providers.Response{}, err
				}
				continue
			}
			return providers.Response{}, fmt.Errorf("gemini: %s", decodeGeminiError(httpRes.StatusCode, resBody))
		}
		response, err := r.decodeStream(ctx, httpRes.Body)
		_ = httpRes.Body.Close()
		return response, err
	}
}
```
The old `providers.StreamText(ctx, response.Text)` after decoding is gone, because `decodeStream` streams each text part as it arrives.
- Replace the head of `decodeGenerateResponse`, from its signature down to and including the `stop, err := geminiStopReason(candidate.FinishReason)` error check, with:
```go
// decodeStream reads streamGenerateContent SSE. The stream has no terminal event,
// so a reply is complete only once a chunk carries finishReason.
func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var parts []geminiPart
	var usage geminiUsageMetadata
	finishReason := ""
	err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		var chunk generateContentResponse
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("gemini: decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("gemini: %s", strings.TrimSpace(chunk.Error.Message))
		}
		if chunk.UsageMetadata != (geminiUsageMetadata{}) {
			usage = chunk.UsageMetadata
		}
		if len(chunk.Candidates) == 0 {
			return nil
		}
		candidate := chunk.Candidates[0]
		for _, part := range candidate.Content.Parts {
			parts = append(parts, part)
			if !part.Thought {
				if err := providers.StreamText(ctx, part.Text); err != nil {
					return err
				}
			}
		}
		if candidate.FinishReason != "" {
			finishReason = candidate.FinishReason
		}
		return nil
	})
	if err != nil {
		return providers.Response{}, err
	}
	if finishReason == "" {
		return providers.Response{}, fmt.Errorf("gemini: %w", providers.ErrIncompleteResponse)
	}
	return r.reply(parts, finishReason, usage)
}

func (r *Runtime) reply(parts []geminiPart, finishReason string, usage geminiUsageMetadata) (providers.Response, error) {
	stop, err := geminiStopReason(finishReason)
	if err != nil {
		return providers.Response{}, err
	}
```
- In the rest of that function (now `reply`), change `for i, part := range candidate.Content.Parts {` to `for i, part := range parts {`. Change the text condition `if !part.Thought && strings.TrimSpace(part.Text) != "" {` to `if !part.Thought {`, because streamed chunks may be whitespace-only pieces of one reply. Change `Usage: geminiUsage(payload.UsageMetadata),` to `Usage: geminiUsage(usage),`.

- [ ] **Step 4: Move the older tests to the stream**

- Replace `internal/providers/ai/gemini/adapter_test.go` entirely with:
```go
package gemini

import (
	"context"
	"strings"
	"testing"
)

func TestRepeatedGeminiToolInLaterTurnGetsDistinctCallID(t *testing.T) {
	runtime := &Runtime{}
	stream := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"inspect","args":{"path":"a"}}}]},"finishReason":"STOP"}]}` + "\n\n"
	first, err := runtime.decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || len(second.ToolCalls) != 1 || first.ToolCalls[0].ID == "" || first.ToolCalls[0].ID == second.ToolCalls[0].ID {
		t.Fatalf("tool IDs collide across turns: %#v %#v", first.ToolCalls, second.ToolCalls)
	}
}
```
- Replace Stage 0's `internal/providers/ai/gemini/usage_test.go` entirely with:
```go
package gemini

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageCountsThoughtsAsOutput(t *testing.T) {
	stream := `data: {"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":600,"candidatesTokenCount":100,"thoughtsTokenCount":40,"totalTokenCount":1140}}` + "\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	got := response.Usage
	if len(got.ProviderRaw) == 0 {
		t.Fatal("provider raw usage missing")
	}
	got.ProviderRaw = nil
	want := providers.Usage{PromptTokens: 1000, CacheReadTokens: 600, OutputTokens: 140, ReasoningTokens: 40}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage=%+v, want %+v", got, want)
	}
}
```
- `finish_reason_test.go` (`generateAgainst`): the server writes SSE:
```go
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + reply + "\n\n"))
```
- `request_test.go` (`capturePayloads`): the server reply becomes:
```go
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}` + "\n\n"))
```
- `replay_test.go` (`TestThoughtSignatureIsReturnedAsReasoning`): add `"context"` and `"strings"` to the imports and decode through the stream:
```go
	stream := `data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read","args":{"path":"a"}},"thoughtSignature":"sig-a"},{"functionCall":{"name":"read","args":{"path":"b"}}}]},"finishReason":"STOP"}]}` + "\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers/ai/gemini && go vet ./internal/providers/ai/gemini && go test ./internal/providers/ai/gemini -count=1 && grep -rn 'decodeGenerateResponse\|:generateContent"' internal/providers/ai/gemini`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/gemini`; the grep prints nothing.

- [ ] **Step 6: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/ai/gemini/types.go internal/providers/ai/gemini/adapter.go internal/providers/ai/gemini/adapter_test.go internal/providers/ai/gemini/usage_test.go internal/providers/ai/gemini/finish_reason_test.go internal/providers/ai/gemini/request_test.go internal/providers/ai/gemini/replay_test.go internal/providers/ai/gemini/stream_test.go
git commit -m "feat(gemini): stream generation over SSE so the idle timeout applies

A stream is complete only when a chunk carries finishReason; a cut
stream is a retryable ErrIncompleteResponse.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Codex stop reasons, tool_choice, prompt_cache_key, no output limit

**Files:**
- Modify: `internal/providers/ai/openaicodex/runtime.go:19-29,31-39,65-73,127-138,216-231`
- Modify: `internal/providers/ai/openaicodex/response.go:24-73,75-113`
- Modify: `internal/providers/ai/openaicodex/response_test.go:62,99`
- Modify: `internal/providers/factory/factory.go:70-79`
- Test: `internal/providers/ai/openaicodex/stop_reason_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/providers/ai/openaicodex/stop_reason_test.go`:

```go
package openaicodex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestTerminalStatusBecomesStopReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event string
		want  providers.StopReason
		text  string
		calls int
	}{
		{"completed text", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Done"}]}]}}`, providers.StopEndTurn, "Done", 0},
		{"completed tools", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","call_id":"a","name":"read","arguments":"{}"}]}}`, providers.StopToolUse, "", 1},
		{"output limit keeps text", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"Partial"}]}]}}`, providers.StopMaxTokens, "Partial", 0},
		{"output limit drops cut call", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","call_id":"a","name":"read","arguments":"{}"},{"type":"function_call","call_id":"b","name":"read","arguments":"{\"pa"}]}}`, providers.StopMaxTokens, "", 1},
		{"output limit spent on reasoning", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, providers.StopMaxTokens, "", 0},
		{"content filter", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"content_filter"}}}`, providers.StopContentFilter, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader("data: "+tc.event+"\n\n"))
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || response.Text != tc.text || len(response.ToolCalls) != tc.calls {
				t.Fatalf("response=%+v, want stop=%q text=%q calls=%d", response, tc.want, tc.text, tc.calls)
			}
		})
	}
}

func TestPayloadCarriesCacheKeyAndToolChoiceButNoOutputLimit(t *testing.T) {
	payload := (&Runtime{model: "gpt-5.4"}).responsesPayload(providers.Request{
		CacheKey:        "session-1",
		ToolChoice:      providers.ToolChoiceNone,
		MaxOutputTokens: 4000,
		Tools:           []providers.ToolDefinition{{Name: "read"}},
		Messages:        []providers.Message{{Role: "user", Content: "hello"}},
	})
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["prompt_cache_key"] != "session-1" || wire["tool_choice"] != "none" {
		t.Fatalf("wire=%s", raw)
	}
	if _, sent := wire["max_output_tokens"]; sent {
		t.Fatalf("max_output_tokens sent to the Codex backend: %s", raw)
	}
}
```

In `internal/providers/ai/openaicodex/response_test.go`:
- In `TestStreamRejectsMissingCompletionAndTerminalFailures`, replace the `"truncated"` case (baseline line 62) with:
```go
		{"incomplete for another reason", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"interrupted"}}}`, "interrupted", false},
```
- In `TestResponseDecodingHandlesRefusalAndEmptyOutput`, change the assertion line to:
```go
	if jsonResponse.Text != "Cannot help with that." || streamResponse.Text != jsonResponse.Text || jsonResponse.StopReason != providers.StopRefusal {
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/providers/ai/openaicodex -count=1`
Expected: FAIL. `TestTerminalStatusBecomesStopReason/output_limit_keeps_text` fails with `openai-codex: response incomplete: max_output_tokens`, the payload test prints `wire=…` without `prompt_cache_key`, and the refusal test fails on `StopReason`.

- [ ] **Step 3a: Remove the output limit, add tool_choice and cache key**

`runtime.go`:
- Delete `MaxOutputTokens int64` from `Config`, `maxOutputTokens int64` from `Runtime`, and `maxOutputTokens: cfg.MaxOutputTokens,` from the `New` literal.
- In `responsesRequest`, replace `MaxOutputTokens *int64 \`json:"max_output_tokens,omitempty"\`` with `ToolChoice string \`json:"tool_choice,omitempty"\``, and add `PromptCacheKey string \`json:"prompt_cache_key,omitempty"\`` below `Include`.
- Replace the head of `responsesPayload`, from its signature through the removed `maxOutputTokens` block, with:
```go
// responsesPayload never sends max_output_tokens: the ChatGPT Codex backend
// rejects the parameter, so the model's own limit applies.
func (r *Runtime) responsesPayload(request providers.Request) responsesRequest {
	payload := responsesRequest{
		Model:             r.model,
		Input:             make([]responsesItem, 0, len(request.Messages)),
		Tools:             encodeResponsesTools(request.Tools),
		ParallelToolCalls: r.capabilities.ParallelToolCalls,
		Store:             false,
		PromptCacheKey:    strings.TrimSpace(request.CacheKey),
		Stream:            true,
	}
	payload.Instructions = combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions)
	if request.ToolChoice == providers.ToolChoiceNone && len(payload.Tools) > 0 {
		payload.ToolChoice = string(providers.ToolChoiceNone)
	}
```
(the `reasoningEffort` block and the input loop stay as they are).

`internal/providers/factory/factory.go`: in the `providers.TypeOpenAICodex` case, delete the line `MaxOutputTokens: cfg.MaxOutputTokens,`.

- [ ] **Step 3b: Stop reasons**

`response.go` (`completedResponse`): replace the two status checks

```go
	if response.Status != "" && response.Status != "completed" {
		...
	}
	if response.IncompleteDetails != nil {
		...
	}
```

with

```go
	stop, err := responsesStopReason(response)
	if err != nil {
		return providers.Response{}, err
	}
```

Add `refused := false` below `var calls []providers.ToolCall`. In the `case "refusal":` branch, add `refused = true` before `text.WriteString(part.Refusal)`. Replace the `case "function_call":` body with:

```go
			name := strings.TrimSpace(item.Name)
			id := strings.TrimSpace(item.CallID)
			arguments := responsesToolArguments(item.Arguments)
			if stop == providers.StopMaxTokens && (name == "" || id == "" || !json.Valid(arguments)) {
				continue // cut off by the output limit
			}
			if name == "" || id == "" {
				return providers.Response{}, fmt.Errorf("openai-codex: function call is missing name or call_id")
			}
			if !json.Valid(arguments) {
				return providers.Response{}, fmt.Errorf("openai-codex: invalid arguments for tool %q", name)
			}
			calls = append(calls, providers.ToolCall{ID: id, Name: name, Arguments: arguments})
```

Replace the tail after the loop with:

```go
	if refused && stop == providers.StopEndTurn && len(calls) == 0 {
		stop = providers.StopRefusal
	}
	stop = providers.ResolveStopReason(stop, len(calls))
	text := strings.Join(texts, "\n\n")
	if text == "" && len(calls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("openai-codex: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{Text: text, ToolCalls: calls, Model: r.model, Provider: providers.TypeOpenAICodex, StopReason: stop, Usage: response.Usage.toProviderUsage()}, nil
}

// responsesStopReason maps the terminal status; an incomplete response stays
// usable only when the output limit or a content filter ended it.
func responsesStopReason(response responsesResponse) (providers.StopReason, error) {
	reason := ""
	if response.IncompleteDetails != nil {
		reason = response.IncompleteDetails.Reason
	}
	switch {
	case response.Status != "" && response.Status != "completed" && response.Status != "incomplete":
		if reason != "" {
			return "", fmt.Errorf("openai-codex: response %s: %s", response.Status, reason)
		}
		return "", fmt.Errorf("openai-codex: response %s", response.Status)
	case response.Status != "incomplete" && response.IncompleteDetails == nil:
		return providers.StopEndTurn, nil
	case reason == "max_output_tokens":
		return providers.StopMaxTokens, nil
	case reason == "content_filter":
		return providers.StopContentFilter, nil
	default:
		return "", fmt.Errorf("openai-codex: incomplete response: %s", firstNonEmpty(reason, "no reason given"))
	}
}
```

(`response.Usage.toProviderUsage()` is the Stage 0 method; Stage 0 Task 7 rewrites only its body.) In `decodeStream`, replace the `case "response.failed", "response.incomplete", "response.cancelled":` branch with:

```go
		case "response.failed", "response.cancelled":
			// Some gateways omit response.status on terminal failure events.
			chunk.Response.Status = strings.TrimPrefix(firstNonEmpty(chunk.Type, event.Type), "response.")
			_, err := r.completedResponse(chunk.Response)
			return err
		case "response.incomplete":
			final = chunk.Response
			final.Status = "incomplete"
			completed = true
			return providers.ErrSSEComplete
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers/ai/openaicodex internal/providers/factory && go vet ./internal/providers/... && go test ./internal/providers/ai/openaicodex -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/ai/openaicodex/runtime.go internal/providers/ai/openaicodex/response.go internal/providers/ai/openaicodex/response_test.go internal/providers/ai/openaicodex/stop_reason_test.go internal/providers/factory/factory.go
git commit -m "feat(openaicodex): map incomplete responses to stop reasons, send tool_choice and prompt_cache_key

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Codex encrypted reasoning round trip

**Files:**
- Modify: `internal/providers/ai/openaicodex/runtime.go:144-152,194-202,229-231,255-266`
- Modify: `internal/providers/ai/openaicodex/response.go` (`completedResponse`)
- Test: `internal/providers/ai/openaicodex/reasoning_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/ai/openaicodex/reasoning_test.go`:

```go
package openaicodex

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestEncryptedReasoningIsRequestedAndReturned(t *testing.T) {
	runtime := &Runtime{model: "gpt-5.4", reasoningEffort: "medium"}
	payload := runtime.responsesPayload(providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
	if !slices.Contains(payload.Include, "reasoning.encrypted_content") {
		t.Fatalf("include=%v", payload.Include)
	}
	response, err := runtime.decodeResponse([]byte(`{"status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plan"}],"encrypted_content":"enc-1"},{"type":"function_call","call_id":"a","name":"read","arguments":"{}"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []providers.ReasoningBlock{{Text: "Plan", RedactedData: "enc-1"}}
	if !slices.Equal(response.Reasoning, want) {
		t.Fatalf("reasoning=%+v, want %+v", response.Reasoning, want)
	}
}

func TestReasoningIsReplayedBeforeCommentaryAndCalls(t *testing.T) {
	payload := (&Runtime{model: "gpt-5.4"}).responsesPayload(providers.Request{Messages: []providers.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "Checking.", Reasoning: []providers.ReasoningBlock{{RedactedData: "enc-1"}}, ToolCalls: []providers.ToolCall{{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolCallID: "a", Content: "file"},
	}})
	raw, err := json.Marshal(payload.Input)
	if err != nil {
		t.Fatal(err)
	}
	var input []map[string]any
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, item := range input {
		types = append(types, item["type"].(string))
	}
	want := []string{"message", "reasoning", "message", "function_call", "function_call_output"}
	if !slices.Equal(types, want) {
		t.Fatalf("input=%s, want item types %v", raw, want)
	}
	reasoning := input[1]
	if reasoning["encrypted_content"] != "enc-1" || reasoning["id"] != nil {
		t.Fatalf("reasoning item=%v", reasoning)
	}
	if summary, ok := reasoning["summary"].([]any); !ok || len(summary) != 0 {
		t.Fatalf("reasoning item needs an empty summary: %v", reasoning)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers/ai/openaicodex -run 'TestEncryptedReasoning|TestReasoningIsReplayed' -count=1`
Expected: FAIL with `include=[]` and `input=… want item types [message reasoning message function_call function_call_output]`.

- [ ] **Step 3: Implement**

`runtime.go`:
- Replace `responsesItem` with:
```go
type responsesItem struct {
	Type      string                 `json:"type,omitempty"`
	Role      string                 `json:"role,omitempty"`
	Content   []responsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
	Output    string                 `json:"output,omitempty"`
	// A replayed reasoning item must carry summary, even when it is empty.
	Summary          *[]responsesSummaryPart `json:"summary,omitempty"`
	EncryptedContent string                  `json:"encrypted_content,omitempty"`
}

type responsesSummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
```
- Replace `responsesOutputItem` with:
```go
type responsesOutputItem struct {
	ID               string                 `json:"id,omitempty"`
	Type             string                 `json:"type,omitempty"`
	Role             string                 `json:"role,omitempty"`
	Content          []responsesContentPart `json:"content,omitempty"`
	CallID           string                 `json:"call_id,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Arguments        string                 `json:"arguments,omitempty"`
	Summary          []responsesSummaryPart `json:"summary,omitempty"`
	EncryptedContent string                 `json:"encrypted_content,omitempty"`
}
```
- In `responsesPayload`, inside `if r.reasoningEffort != "" && ... {`, add below the `payload.Reasoning = ...` line:
```go
		payload.Include = []string{"reasoning.encrypted_content"}
```
- In `responsesItemsFromMessage`, `case "assistant":`, change `items := []responsesItem(nil)` to `items := responsesReasoningItems(message.Reasoning)`.
- Above `func responsesContentParts`, add:
```go
// responsesReasoningItems replays encrypted reasoning; with store=false the server
// kept nothing, so the item is rebuilt without an id.
func responsesReasoningItems(blocks []providers.ReasoningBlock) []responsesItem {
	var items []responsesItem
	for _, block := range blocks {
		if block.RedactedData == "" {
			continue
		}
		summary := []responsesSummaryPart{}
		if block.Text != "" {
			summary = append(summary, responsesSummaryPart{Type: "summary_text", Text: block.Text})
		}
		items = append(items, responsesItem{Type: "reasoning", Summary: &summary, EncryptedContent: block.RedactedData})
	}
	return items
}
```

`response.go` (`completedResponse`): add `var reasoning []providers.ReasoningBlock` below `var calls []providers.ToolCall`, and a first case in the output switch:

```go
		case "reasoning":
			if item.EncryptedContent != "" {
				reasoning = append(reasoning, providers.ReasoningBlock{Text: reasoningSummary(item.Summary), RedactedData: item.EncryptedContent})
			}
```

Add `Reasoning: reasoning,` to the returned `providers.Response` literal, and append:

```go
func reasoningSummary(parts []responsesSummaryPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n")
}
```

Core (Task 4) already sends the step's reply text and reasoning in the same message as the calls, so the reasoning item comes before the commentary without any reordering.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers/ai/openaicodex && go vet ./internal/providers/ai/openaicodex && go test ./internal/providers/ai/openaicodex -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/ai/openaicodex/runtime.go internal/providers/ai/openaicodex/response.go internal/providers/ai/openaicodex/reasoning_test.go
git commit -m "feat(openaicodex): request encrypted reasoning and send it back between steps

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Anthropic stop_reason for text replies and output limit

**Files:**
- Modify: `internal/providers/ai/anthropiccompat/adapter.go:17-21,34-42,54-62,105-122,135-138,209-220,266-272,296-309,346-349,368-417`
- Test: `internal/providers/ai/anthropiccompat/stop_reason_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/providers/ai/anthropiccompat/stop_reason_test.go`:

```go
package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestTextReplyCarriesStopReasonAndOutputLimit(t *testing.T) {
	var limits []float64
	replies := []string{
		`{"content":[{"type":"text","text":"Partial"}],"stop_reason":"max_tokens"}`,
		`{"content":[{"type":"text","text":"Done"}],"stop_reason":"end_turn"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		limits = append(limits, body["max_tokens"].(float64))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(replies[len(limits)-1]))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	messages := []providers.Message{{Role: "user", Content: "hello"}}
	cut, err := runtime.Generate(context.Background(), providers.Request{Messages: messages})
	if err != nil || cut.StopReason != providers.StopMaxTokens || cut.Text != "Partial" {
		t.Fatalf("response=%+v err=%v", cut, err)
	}
	done, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, MaxOutputTokens: 2000})
	if err != nil || done.StopReason != providers.StopEndTurn {
		t.Fatalf("response=%+v err=%v", done, err)
	}
	if limits[0] != float64(providers.DefaultMaxOutputTokens) || limits[1] != 2000 {
		t.Fatalf("max_tokens sent=%v", limits)
	}
}

func TestStreamedRefusalIsAValidEmptyReply(t *testing.T) {
	stream := "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil || response.StopReason != providers.StopRefusal {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestListModelsRegistersOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-maxout-test","max_input_tokens":200000,"max_tokens":8192}]}`))
	}))
	defer server.Close()
	if _, err := ListModels(context.Background(), Config{ProviderID: "anthropic-maxout", APIKey: "test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if got := providers.ResolveModelMetadata("anthropic-maxout", providers.TypeAnthropic, "claude-maxout-test").MaxOutputTokens; got != 8192 {
		t.Fatalf("catalog max output=%d, want 8192", got)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/providers/ai/anthropiccompat -count=1`
Expected: FAIL. You get `response=… StopReason:"" …`, a failed stream with `empty assistant reply`, and `catalog max output=0`.

- [ ] **Step 3: Implement**

`adapter.go`:
- Delete `defaultMaxTokens = 4096` from the const block.
- In `Runtime`, add `providerID string` below `model string`. In `New`, add `providerID: metadataProviderID(cfg),` below `model: model,`.
- In `normalizeConfig`, replace the `maxTokens := cfg.MaxOutputTokens` / `if maxTokens <= 0 { maxTokens = defaultMaxTokens }` block with `maxTokens := cfg.MaxOutputTokens`.
- In `Generate`, change `MaxTokens: r.maxTokens,` to:
```go
		MaxTokens: providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxTokens, r.providerID, providers.TypeAnthropic, r.model),
```
- In `anthropicResponse`, add `StopReason string \`json:"stop_reason,omitempty"\`` above `Usage`.
- In `anthropicStreamDelta.Delta`, add `StopReason string \`json:"stop_reason"\``.
- In `ListModels`, add `MaxTokens int \`json:"max_tokens"\`` to the item struct and replace the two `RegisterContextWindowTokens` lines with:
```go
			metadata := providers.ModelMetadataRegistration{ContextWindow: item.MaxInputTokens, MaxOutputTokens: item.MaxTokens}
			providers.RegisterModelMetadata(cfg.ProviderID, providers.TypeAnthropic, id, metadata)
			providers.RegisterModelMetadata(cfg.CatalogID, providers.TypeAnthropic, id, metadata)
```
- In the non-streaming tail of `Generate`, replace from `reply := strings.TrimSpace(text.String())` to the end of the function with the code below (keep the Stage 0 usage call):
```go
	reply := strings.TrimSpace(text.String())
	stop := providers.ResolveStopReason(anthropicStopReason(response.StopReason), 0)
	if reply == "" && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("anthropic: %w", providers.ErrEmptyResponse)
	}

	return providers.Response{
		Text:       reply,
		Model:      r.model,
		Provider:   providers.TypeAnthropic,
		StopReason: stop,
		Usage:      anthropicUsage(response.Usage),
	}, nil
}

func anthropicStopReason(reason string) providers.StopReason {
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return providers.StopMaxTokens
	case "refusal":
		return providers.StopRefusal
	case "tool_use":
		return providers.StopToolUse
	default:
		return providers.StopEndTurn
	}
}

func metadataProviderID(cfg Config) string {
	if id := strings.TrimSpace(cfg.ProviderID); id != "" {
		return id
	}
	return strings.TrimSpace(cfg.CatalogID)
}
```
- In `decodeStream`, add `stopReason := ""` below `var usage anthropicUsagePayload`, which Stage 0 Task 7 added. Stage 0 also added a `switch event.Type { case "message_start": … case "message_delta": usage = mergeAnthropicUsage(usage, chunk.Usage) }` right after the `message_stop` block. Extend its `message_delta` case so the event updates **both** usage and the stop reason. Do not add an early `return`, because that would skip the usage merge:
```go
		case "message_delta":
			usage = mergeAnthropicUsage(usage, chunk.Usage)
			if chunk.Delta.StopReason != "" {
				stopReason = chunk.Delta.StopReason
			}
```
Then replace the tail from `reply := strings.TrimSpace(text.String())` with:
```go
	reply := strings.TrimSpace(text.String())
	stop := providers.ResolveStopReason(anthropicStopReason(stopReason), 0)
	if reply == "" && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("anthropic: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{
		Text:       reply,
		Model:      r.model,
		Provider:   providers.TypeAnthropic,
		StopReason: stop,
		Usage:      anthropicUsage(usage),
	}, nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers/ai/anthropiccompat && go vet ./internal/providers/ai/anthropiccompat && go test ./internal/providers/ai/anthropiccompat -count=1`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/anthropiccompat`

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/ai/anthropiccompat/adapter.go internal/providers/ai/anthropiccompat/stop_reason_test.go
git commit -m "feat(anthropic): map stop_reason for text replies and resolve the output limit

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Delete the unused reasoning fields

After Task 4, `ReasoningBlock` → `ReasoningPart{Text, Signature, RedactedData}` carries all provider reasoning. Nothing writes `ReasoningPart.ThoughtSignature`, `ToolID` or `ResponsesData`; the only readers are the TUI surface adapter and four surface methods that are never called. `ModelCapabilities.ThoughtSignatures` is only ever set to `false`. All of this is dead code, and this task deletes it.

**Persisted data:** old `messages.parts` rows may still contain the JSON keys `thought_signature`, `tool_id` and `responses_data` inside a reasoning part. `encoding/json` ignores unknown keys (the repo never uses `DisallowUnknownFields`), so those rows still decode; the keys are simply dropped. No migration is needed. The test below pins that behaviour.

**Files:**
- Modify: `internal/transcript/message.go` (`type ReasoningPart struct`; baseline `internal/core/types_message.go:64-70`)
- Modify: `clients/terminal/chat/viewmodel/surface_adapter.go:34-42`
- Modify: `clients/terminal/ui/surface/message/message.go:3-10,36-44,208-276`
- Modify: `internal/providers/model_capabilities.go:21,100`
- Modify: `internal/providers/model_metadata.go:249`
- Test: `internal/transcript/reasoning_part_test.go`

- [ ] **Step 1: Write the regression test**

Create `internal/transcript/reasoning_part_test.go`:

```go
package transcript

import (
	"encoding/json"
	"testing"
)

func TestLegacyReasoningKeysAreIgnoredOnDecode(t *testing.T) {
	raw := []byte(`{"kind":"reasoning","reasoning":{"text":"plan","signature":"sig","thought_signature":"old","tool_id":"call-1","responses_data":{"id":"rs_1"}}}`)
	var part MessagePart
	if err := json.Unmarshal(raw, &part); err != nil {
		t.Fatal(err)
	}
	if part.Kind != MessagePartKindReasoning || part.Reasoning == nil || part.Reasoning.Text != "plan" || part.Reasoning.Signature != "sig" {
		t.Fatalf("part=%+v", part)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/transcript -run TestLegacyReasoningKeysAreIgnoredOnDecode -count=1`
Expected: PASS. This test is a regression guard for old rows, not a red test: it must still pass after the fields are gone.

- [ ] **Step 3: List every reference**

Run: `grep -rn 'ThoughtSignature\|ResponsesData\|AppendThoughtSignature\|SetReasoningResponsesData\|AppendReasoningSignature\|AppendReasoningContent\|thought_signature\|responses_data' --include='*.go' --include='*.swift' --include='*.ts' . | grep -v '^./docs/' | grep -v 'internal/providers/ai/gemini/'`
Expected (line numbers are the baseline's and may drift), and nothing else. Gemini's wire field `geminiPart.ThoughtSignature` is legitimate and excluded:
```
internal/providers/model_capabilities.go:21:	ThoughtSignatures  bool
internal/providers/model_capabilities.go:100:		capabilities.ThoughtSignatures = false
internal/providers/model_metadata.go:249:		capabilities.ThoughtSignatures = false
internal/transcript/message.go: ThoughtSignature string `json:"thought_signature,omitempty"`
internal/transcript/message.go: ResponsesData    json.RawMessage `json:"responses_data,omitempty"`
clients/terminal/ui/surface/message/message.go: ThoughtSignature / ResponsesData fields and the four methods
clients/terminal/chat/viewmodel/surface_adapter.go:37-39
```
`ToolID` is a common name elsewhere (`tools.Result`, approvals). Delete only the `ToolID` fields of `transcript.ReasoningPart` and `surfacemessage.ReasoningContent`.

- [ ] **Step 4: Delete**

1. In `internal/transcript/message.go`, `ReasoningPart` becomes exactly:
```go
type ReasoningPart struct {
	Text         string `json:"text"`
	Signature    string `json:"signature,omitempty"`
	RedactedData string `json:"redacted_data,omitempty"`
}
```
Keep the `encoding/json` import, which other part types still use (`ToolResultPart.Metadata`, `FinishPart.Details`).

2. `clients/terminal/chat/viewmodel/surface_adapter.go`: in the `MessagePartKindReasoning` case, delete these three lines from the `surfacemessage.ReasoningContent{...}` literal:
```go
					ThoughtSignature: part.Reasoning.ThoughtSignature,
					ToolID:           part.Reasoning.ToolID,
					ResponsesData:    append(json.RawMessage(nil), part.Reasoning.ResponsesData...),
```
`encoding/json` is still used elsewhere in the file (`toJSONString`, `decodePermissionParams`).

3. `clients/terminal/ui/surface/message/message.go`:
   - `ReasoningContent` becomes exactly:
```go
type ReasoningContent struct {
	Thinking   string `json:"thinking"`
	Signature  string `json:"signature"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}
```
   - Delete the methods `AppendReasoningContent`, `AppendThoughtSignature`, `AppendReasoningSignature` and `SetReasoningResponsesData` (baseline lines 208-276). None of them has a caller anywhere in the repo; check with `grep -rn 'AppendReasoningContent\|AppendThoughtSignature\|AppendReasoningSignature\|SetReasoningResponsesData' --include='*.go' .`, which must list only their definitions.
   - Remove the now-unused `"encoding/json"` import. `"time"` stays, because `FinishThinking` uses it.

4. `internal/providers/model_capabilities.go`: delete the field `ThoughtSignatures  bool` from `ModelCapabilities` and the line `capabilities.ThoughtSignatures = false` in `runtimeCapabilitiesFromProvider`.

5. `internal/providers/model_metadata.go`: delete `capabilities.ThoughtSignatures = false` in `applyCachedCapabilities`.

- [ ] **Step 5: Verify**

Run: `gofmt -w internal clients && go build ./... && go vet ./... && go test ./... -count=1 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

Run the grep from Step 3 again.
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/transcript/message.go internal/transcript/reasoning_part_test.go clients/terminal/chat/viewmodel/surface_adapter.go clients/terminal/ui/surface/message/message.go internal/providers/model_capabilities.go internal/providers/model_metadata.go
git commit -m "refactor: delete unused reasoning fields superseded by the reasoning carrier

Old rows that still carry thought_signature, tool_id or responses_data decode
unchanged; the keys are ignored.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: Stage verification

**Files:** none changed.

- [ ] **Step 1: Static checks**

Run: `gofmt -l internal/ && go vet ./...`
Expected: no output.

- [ ] **Step 2: Whole suite, uncached**

Run: `go test ./... -count=1 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 3: No leftovers**

Run: `grep -rn 'validateFinishReason\|batchAdjacentToolCallMessages\|attachReasoningToToolCallMessage\|defaultMaxOutputTokens\|defaultMaxTokens' internal/providers internal/core; grep -rn 'MaxOutputTokens' internal/providers/ai/openaicodex --include='*.go' | grep -v _test.go`
Expected: no output.

- [ ] **Step 4: Build binaries**

Run: `go build -o ./bin/matrixclaw ./cmd/matrixclaw && go build -o ./bin/matrixclawd ./cmd/matrixclawd`
Expected: exit code 0.

- [ ] **Step 5: Manual check on the test stand (spec: every stage)**

Deploy the stand the usual way; the stand procedure is not in the repo, so ask the owner if unsure. Then run one multi-tool task through the TUI with each configured provider (an OpenAI-compatible one, Gemini, Codex). Confirm the following:
- the run completes with no `max_tokens`/`length` errors on normal replies;
- a turn with several parallel tool calls continues correctly. Gemini 3 no longer answers `400 … thought signature`, and Codex keeps reasoning between steps (usage shows `reasoning_tokens` without an error);
- Gemini replies appear in the TUI incrementally (SSE), not in one piece;
- `run_steps.stop_reason` holds `tool_use` for tool steps, `end_turn` for the final reply and `compact` for summaries, and streamed openaicompat steps have non-zero `prompt_tokens` (`sqlite3 <db> 'select step, stop_reason from run_steps order by rowid desc limit 5'`).

No commit in this task.

---

## Self-review

**1. Spec coverage (§5 Providers + contracts "Stage 1 provides" + amendments):**
- `MaxOutputTokens` on Request, resolution request → config → catalog → 16k: Tasks 1, 2 and each adapter (6, 8, 12). The catalog value is capped at the default, as explained in Design decision 3. Codex is exempt (decision 4).
- `ToolChoice auto|none`: Task 1; openaicompat Task 6, Gemini Task 8, Codex Task 10; Anthropic in 1b (tools disabled).
- `StopReason` on Response with typed constants: Task 1; mapping in Tasks 5, 7, 10, 12.
- `CacheKey` = session id, set by core: Task 3; `prompt_cache_key` in Tasks 6 and 10; OpenRouter `cache_control` deferred (decision 5); Anthropic in 1b.
- openaicompat `length` → max_tokens, not an error: Task 5.
- Gemini finishReason table, MALFORMED retryable, batched functionResponses, thought signatures, mode NONE: Tasks 7, 8, 9.
- Codex incomplete(max_output_tokens) → max_tokens, `reasoning.encrypted_content` include + replay: Tasks 10, 11.
- anthropiccompat compiles and maps stop_reason for text replies: Task 12.
- Old loop keeps behaviour (max_tokens → same error as today): Task 3. content_filter too, since openaicompat errored on it before.
- Amendments: `ReasoningBlock` carrier and `transcript.ReasoningPart.RedactedData` (Tasks 1, 4, 9, 11); `Message.IsError` (Tasks 1, 4, 9); one assistant message per response (Task 4); `CacheKey` (Task 3).
- No dead code (owner rule): Task 13 deletes `ReasoningPart.ThoughtSignature`/`ToolID`/`ResponsesData`, the four uncalled TUI surface methods and `ModelCapabilities.ThoughtSignatures`. Old rows that still carry those JSON keys decode unchanged.
- Coordinator follow-ups: streamed openaicompat usage via `stream_options` (Task 6b); Gemini on the SSE endpoint with finishReason-terminated streams, so Stage 0's idle timeout applies (Task 9b); `run_steps.stop_reason` from `Response.StopReason` without touching the `compact` marker (Task 3).
- Idle timeouts and normalised usage are Stage 0; this plan builds on `providers.NewHTTPClient` and the normalised `Usage` names.
- Per-provider tests against local servers: stop reasons, max_tokens, tool_choice none (every adapter); Gemini batched responses (Task 9). Usage normalisation and idle-timeout tests belong to Stage 0.

**2. Placeholder scan:** No TBD/TODO. Code that Stage 0 creates is referenced by the names in the Stage 0 plan (`recordRunStep`, `generationStopReason`, `providers.NewHTTPClient`, the Gemini `usage_test.go`). `ReasoningPart` lives in `internal/transcript/message.go` (Stage 0 Task 1), which Tasks 4 and 13 edit.

**3. Type consistency:** `StopReason`, `ToolChoice`, `ReasoningBlock{Text, Signature, RedactedData}`, `Message.Reasoning`, `Message.IsError`, `Response.Reasoning`, `Request.MaxOutputTokens int`, `Request.CacheKey`, `DefaultMaxOutputTokens int64`, `ResolveMaxOutputTokens(int, int64, string, string, string) int64`, `ResolveStopReason(StopReason, int)`, `ErrMalformedToolCall`, `OpenAIChatOptions.PromptCacheKey` and `RetryWithoutMaxTokens` are used with the same names and types in every task. Test helpers (`sseStream`, `captureChatServer`, `generateAgainst`, `capturePayloads`, `stepCallMessage`, `stepResultMessage`) are unique within their packages. `capturePayloads` is defined in Task 8 before Task 9 uses it. Task 13's final `ReasoningPart` keeps exactly the three fields Tasks 1 and 4 use (`Text`, `Signature`, `RedactedData`).

**Verification note:** Tasks 1, 2, 4-6b and 7-13, including every test above, were applied together to a scratch copy of baseline `6176876`, with core in its pre-Stage-0 form (`Message` rather than `transcript.Message`). `go build ./...`, `go vet ./...` and `go test ./...` all passed there. The run-step assertion (Task 3) depends on Stage 0 and could not be run.
