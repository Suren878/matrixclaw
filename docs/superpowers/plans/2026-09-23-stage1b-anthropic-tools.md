# Stage 1b: Native Anthropic Tool Use Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the native Anthropic adapter (`internal/providers/ai/anthropiccompat`) real tool use:
- `tool_use` / `tool_result` blocks, with the results of one step in one user message;
- `tool_choice: {type: none}`;
- a `cache_control` breakpoint on the tool definitions (system and message breakpoints follow in Stage 3);
- thinking blocks sent back unchanged;
- streamed tool-call assembly.

After that, enable tool calling for Anthropic by default.

**Architecture:** This builds directly on Stage 1. Core already sends each tool step as one `providers.Message`, carrying the reply text, `Reasoning []ReasoningBlock` and every call. The results follow as `tool` messages with `IsError`, and `Request.CacheKey` is the session id. The adapter already maps `stop_reason` for text replies (`anthropicStopReason`, `providers.ResolveStopReason`, `StopReason.AllowsEmptyReply`), resolves `max_tokens` (`providers.ResolveMaxOutputTokens`) and merges streamed usage (`mergeAnthropicUsage`).

Stage 1b replaces the adapter's text-only request and response code with:
- a wire-types file;
- a request encoder;
- one `assembleResponse` shared by the JSON and SSE paths;
- a block-assembling SSE decoder.

Stage 1b then deletes the `ToolUseDisabled` default and adds `ToolCalling` to Anthropic's catalog capabilities. Tool use goes live only in the last code task, so every commit leaves the app working.

**Tech Stack:** Go 1.26, `net/http/httptest`, Anthropic Messages API (`anthropic-version: 2023-06-01`), SSE via `providers.ScanSSE`.

---

## Ground rules

- **Stage 0 and Stage 1 are merged before you start.** This plan uses their final code:
  - Stage 1 plan `docs/superpowers/plans/2026-09-23-stage1-provider-contract.md`, Tasks 1, 3, 4 and 12.
  - Stage 0 plan `docs/superpowers/plans/2026-09-23-stage0-foundations.md`, Task 7 (`anthropicUsage`, `mergeAnthropicUsage`) and Task 8 (`providers.NewHTTPClient`).
- Find each edit by the quoted code. Line numbers moved in Stages 0 and 1.
- Work in `main`. Stage only the files each task lists and never run `git add -A`, because another session may be editing the repo. Every commit must pass `go build ./... && go test ./...`.
- Owner rules:
  - Delete replaced code (no shims or aliases).
  - Keep doc comments to 4 lines or fewer, with no history in them.
  - Test only observable behaviour.
- Run `gofmt -w` on every Go file you touch before running tests.
- Existing tests in `internal/providers/ai/anthropiccompat` that must stay green without edits:
  - Stage 0 `usage_test.go` (`TestUsageCountsCachedInputAsPrompt`).
  - Stage 1 `stop_reason_test.go` (`TestTextReplyCarriesStopReasonAndOutputLimit`, `TestStreamedRefusalIsAValidEmptyReply`, `TestListModelsRegistersOutputLimit`).
  - `stream_test.go` is replaced in Task 2.

## Design decisions

1. **Turn shape.** The Messages API needs alternating turns, thinking first in an assistant turn, and `tool_result` blocks first in a user turn. The encoder therefore does three things:
   - Maps a `tool` message to a `tool_result` block in a user turn.
   - Merges consecutive same-role messages into one turn. This is how the results of one step (and any user text after them) become one user message.
   - Orders the blocks of each turn: thinking and redacted thinking first in an assistant turn, `tool_result` first in a user turn.
2. **Thinking replay.** Only blocks that carry a `Signature` (→ `thinking`) or `RedactedData` (→ `redacted_thinking`) are sent back. The `thinking` key is always present, even when empty (`display: "omitted"`). Unsigned reasoning comes from other providers (`ReasoningContent`) and is never sent, because Anthropic cannot verify it. The adapter never *requests* thinking; blocks are round-tripped when a model or gateway returns them.
3. **Tool ids and input.** Ids from other providers (e.g. Kimi `functions.ls:0`) are mapped onto `^[a-zA-Z0-9_-]+$` consistently on both `tool_use` and `tool_result`. Non-object or invalid arguments are sent as `{}`, and the paired error result explains what went wrong.
4. **Stop reasons.** `assembleResponse` maps the wire value with Stage 1's `anthropicStopReason`, then calls `providers.ResolveStopReason(stop, len(calls))`, and treats a reply with no text and no calls as `ErrEmptyResponse` unless `stop.AllowsEmptyReply()`. A `tool_use` block whose input is not valid JSON is handled in two ways:
   - At `max_tokens` it is dropped, so Stage 2 can raise the limit and retry.
   - Otherwise it is `providers.ErrMalformedToolCall`, which is retryable.
5. **Cache breakpoint (owner decision).** Stage 1b marks **only the last tool definition** with `cache_control: {type: ephemeral}`, and only when `Request.CacheKey != ""` (session requests; one-off calls such as titles pay no 1.25× write). The tools are the start of the cached prefix and do not change between turns, so every later request of the session within the cache lifetime reads them back. The system prompt still changes every turn until Stage 3, so a breakpoint on the system block or on a message would write the cache at 1.25× input price and rarely be read. System and message breakpoints are therefore deferred to Stage 3, together with Stage 1's deferred OpenRouter `cache_control` (Stage 1 plan, Design decision 5). This deliberately narrows spec §3 and the contract's "cache_control on system+tools and on the last message" for this stage.
6. **Out of scope:** the `thinking` request parameter and effort, the interleaved-thinking beta, server tools (`pause_turn` stays `end_turn`, as Stage 1 maps it), and engine reactions to stop reasons (Stage 2).

## File Structure

| File | Responsibility |
|---|---|
| `internal/providers/ai/anthropiccompat/wire.go` (create) | Messages API JSON types (`anthropicRequest`, `anthropicMessage`, `anthropicBlock`, `anthropicTool`, `anthropicToolChoice`, `anthropicCacheControl`, `anthropicImageSource`, `anthropicResponse`). |
| `internal/providers/ai/anthropiccompat/encode.go` (create) | `encodeRequest`: system, tools, tool choice, turns, images, thinking replay, tools cache breakpoint. |
| `internal/providers/ai/anthropiccompat/decode.go` (create) | `decodeResponse`, `assembleResponse`, `decodeToolUse`. |
| `internal/providers/ai/anthropiccompat/stream.go` (create) | `decodeStream` with per-index block assembly. |
| `internal/providers/ai/anthropiccompat/adapter.go` (modify) | `Generate` uses the new files; `Config.ToolUseMode`; delete the replaced text-only code. |
| `internal/providers/ai/anthropiccompat/adapter_test.go` (create) | httptest JSON tests. |
| `internal/providers/ai/anthropiccompat/stream_test.go` (replace) | SSE tests. |
| `internal/providers/factory/factory.go` (modify) | Pass `ToolUseMode` to Anthropic. |
| `internal/providers/request_normalize.go`, `provider_anthropic.go`, `catalog.go` (modify) | Anthropic defaults to native tool calling. |
| `internal/providers/provider_profile_test.go` (create) | Profile test. |

---

### Task 1: Anthropic request encoding and JSON tool responses

Tool use stays off by default after this task. The adapter uses tools only when `Config.ToolUseMode` is `native`, which the test helper sets. Production traffic stays text-only until Task 3.

**Files:**
- Create: `internal/providers/ai/anthropiccompat/wire.go`
- Create: `internal/providers/ai/anthropiccompat/encode.go`
- Create: `internal/providers/ai/anthropiccompat/decode.go`
- Modify: `internal/providers/ai/anthropiccompat/adapter.go`
- Modify: `internal/providers/factory/factory.go`
- Test: `internal/providers/ai/anthropiccompat/adapter_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/providers/ai/anthropiccompat/adapter_test.go`:

```go
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var testTools = []providers.ToolDefinition{
	{Name: "ls", Description: "List a directory", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
	{Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
}

// startServer answers the i-th request with replies[i] and records request bodies.
func startServer(t *testing.T, replies ...string) (*httptest.Server, func(int) []byte) {
	t.Helper()
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		mu.Lock()
		index := len(bodies)
		bodies = append(bodies, body)
		mu.Unlock()
		if index >= len(replies) {
			http.Error(w, `{"error":{"message":"unexpected request"}}`, http.StatusInternalServerError)
			return
		}
		if strings.HasPrefix(replies[index], "event:") {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = io.WriteString(w, replies[index])
	}))
	t.Cleanup(server.Close)
	return server, func(i int) []byte {
		mu.Lock()
		defer mu.Unlock()
		if i >= len(bodies) {
			t.Fatalf("request %d was not sent (%d sent)", i, len(bodies))
		}
		return bodies[i]
	}
}

func newTestRuntime(t *testing.T, replies ...string) (providers.Runtime, func(int) []byte) {
	t.Helper()
	server, body := startServer(t, replies...)
	runtime, err := New(context.Background(), Config{
		APIKey: "test-key", BaseURL: server.URL, Model: "claude-test", HTTPClient: server.Client(),
		ToolUseMode: providers.ToolUseNative,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, body
}

func decodeSent(t *testing.T, body []byte) anthropicRequest {
	t.Helper()
	var request anthropicRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode sent request: %v\n%s", err, body)
	}
	return request
}

func blockTypes(blocks []anthropicBlock) string {
	types := make([]string, 0, len(blocks))
	for _, block := range blocks {
		types = append(types, block.Type)
	}
	return strings.Join(types, ",")
}

func textReply(text string) string {
	return `{"content":[{"type":"text","text":"` + text + `"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
}

func TestToolRoundTripSendsThinkingToolUseAndGroupedResults(t *testing.T) {
	runtime, sent := newTestRuntime(t,
		`{"content":[{"type":"thinking","thinking":"Need both.","signature":"sig-1"},{"type":"redacted_thinking","data":"opaque"},{"type":"text","text":"Checking."},`+
			`{"type":"tool_use","id":"toolu_1","name":"ls","input":{"path":"."}},{"type":"tool_use","id":"toolu_2","name":"read","input":{"path":"go.mod"}}],`+
			`"stop_reason":"tool_use","usage":{"input_tokens":10,"cache_creation_input_tokens":100,"output_tokens":20}}`,
		`{"content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn","usage":{"input_tokens":30,"cache_read_input_tokens":110,"output_tokens":5}}`,
	)
	user := providers.Message{Role: "user", Content: "Inspect the module"}
	first, err := runtime.Generate(context.Background(), providers.Request{SystemPrompt: "You are terse.", Messages: []providers.Message{user}, Tools: testTools})
	if err != nil {
		t.Fatal(err)
	}
	if first.StopReason != providers.StopToolUse || first.Text != "Checking." || len(first.ToolCalls) != 2 {
		t.Fatalf("first response = %#v", first)
	}
	if call := first.ToolCalls[1]; call.ID != "toolu_2" || call.Name != "read" || string(call.Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("second tool call = %#v", call)
	}
	wantReasoning := []providers.ReasoningBlock{{Text: "Need both.", Signature: "sig-1"}, {RedactedData: "opaque"}}
	if len(first.Reasoning) != 2 || first.Reasoning[0] != wantReasoning[0] || first.Reasoning[1] != wantReasoning[1] {
		t.Fatalf("reasoning = %#v", first.Reasoning)
	}
	if first.Usage.PromptTokens != 110 || first.Usage.CacheWriteTokens != 100 || first.Usage.OutputTokens != 20 {
		t.Fatalf("usage = %#v", first.Usage)
	}
	firstSent := decodeSent(t, sent(0))
	if len(firstSent.Tools) != 2 || firstSent.ToolChoice != nil || len(firstSent.System) != 1 || firstSent.System[0].Text != "You are terse." {
		t.Fatalf("first request = %s", sent(0))
	}

	// Stage 1 core replays a tool step as one assistant message followed by its results.
	second, err := runtime.Generate(context.Background(), providers.Request{
		SystemPrompt: "You are terse.",
		Tools:        testTools,
		Messages: []providers.Message{
			user,
			{Role: "assistant", Content: first.Text, Reasoning: first.Reasoning, ToolCalls: first.ToolCalls},
			{Role: "tool", ToolCallID: "toolu_1", Content: "main.go"},
			{Role: "tool", ToolCallID: "toolu_2", Content: "open go.mod: permission denied", IsError: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Text != "Done." || second.StopReason != providers.StopEndTurn || second.Usage.CacheReadTokens != 110 || second.Usage.PromptTokens != 140 {
		t.Fatalf("second response = %#v", second)
	}
	request := decodeSent(t, sent(1))
	if len(request.Messages) != 3 {
		t.Fatalf("messages = %s", sent(1))
	}
	assistant := request.Messages[1]
	if assistant.Role != "assistant" || blockTypes(assistant.Content) != "thinking,redacted_thinking,text,tool_use,tool_use" {
		t.Fatalf("assistant turn = %s", sent(1))
	}
	if thinking := assistant.Content[0]; thinking.Thinking == nil || *thinking.Thinking != "Need both." || thinking.Signature != "sig-1" {
		t.Fatalf("thinking block = %#v", thinking)
	}
	if redacted := assistant.Content[1]; redacted.Data != "opaque" {
		t.Fatalf("redacted block = %#v", redacted)
	}
	if use := assistant.Content[4]; use.ID != "toolu_2" || use.Name != "read" || string(use.Input) != `{"path":"go.mod"}` {
		t.Fatalf("tool_use block = %#v", use)
	}
	results := request.Messages[2]
	if results.Role != "user" || blockTypes(results.Content) != "tool_result,tool_result" {
		t.Fatalf("results turn = %s", sent(1))
	}
	if results.Content[0].ToolUseID != "toolu_1" || results.Content[0].IsError || results.Content[0].Content != "main.go" {
		t.Fatalf("first result = %#v", results.Content[0])
	}
	if results.Content[1].ToolUseID != "toolu_2" || !results.Content[1].IsError || results.Content[1].Content != "open go.mod: permission denied" {
		t.Fatalf("second result = %#v", results.Content[1])
	}
}

func TestEmptyThinkingKeepsItsKeyOnReplay(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"))
	_, err := runtime.Generate(context.Background(), providers.Request{Tools: testTools, Messages: []providers.Message{
		{Role: "user", Content: "List"},
		{Role: "assistant", Reasoning: []providers.ReasoningBlock{{Signature: "sig-omitted"}}, ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sent(0)), `{"type":"thinking","thinking":"","signature":"sig-omitted"}`) {
		t.Fatalf("empty thinking block not replayed verbatim: %s", sent(0))
	}
}

func TestMessagesMergeIntoValidTurns(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"))
	_, err := runtime.Generate(context.Background(), providers.Request{
		Tools: testTools,
		Messages: []providers.Message{
			{Role: "user", Content: "List"},
			{Role: "assistant", Reasoning: []providers.ReasoningBlock{{Text: "unsigned text from another provider"}}, ToolCalls: []providers.ToolCall{
				{ID: "functions.ls:0", Name: "ls", Arguments: json.RawMessage(`not json`)},
			}},
			{Role: "tool", ToolCallID: "functions.ls:0", Content: "a.go"},
			{Role: "user", Content: "Also check README"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := decodeSent(t, sent(0))
	if len(request.Messages) != 3 {
		t.Fatalf("messages = %s", sent(0))
	}
	assistant := request.Messages[1].Content
	if blockTypes(assistant) != "tool_use" || assistant[0].ID != "functions_ls_0" || string(assistant[0].Input) != `{}` {
		t.Fatalf("assistant turn = %s", sent(0))
	}
	user := request.Messages[2].Content
	if blockTypes(user) != "tool_result,text" || user[0].ToolUseID != "functions_ls_0" || user[1].Text != "Also check README" {
		t.Fatalf("user turn = %s", sent(0))
	}
}

func TestToolChoiceNoneKeepsToolsDefined(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("Summary."), textReply("Plain."))
	messages := []providers.Message{{Role: "user", Content: "Wrap up"}}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, Tools: testTools, ToolChoice: providers.ToolChoiceNone}); err != nil {
		t.Fatal(err)
	}
	withTools := decodeSent(t, sent(0))
	if withTools.ToolChoice == nil || withTools.ToolChoice.Type != "none" || len(withTools.Tools) != 2 {
		t.Fatalf("request = %s", sent(0))
	}
	if _, err := runtime.Generate(context.Background(), providers.Request{Messages: messages, ToolChoice: providers.ToolChoiceNone}); err != nil {
		t.Fatal(err)
	}
	if withoutTools := decodeSent(t, sent(1)); withoutTools.ToolChoice != nil || len(withoutTools.Tools) != 0 {
		t.Fatalf("request = %s", sent(1))
	}
}

func TestCacheBreakpointMarksOnlyTheTools(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("ok"), textReply("ok"))
	request := providers.Request{
		CacheKey:     "session-1",
		SystemPrompt: "System rules.",
		Tools:        testTools,
		Messages: []providers.Message{
			{Role: "user", Content: "Start"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
		},
	}
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(sent(0)), `"cache_control"`); got != 1 {
		t.Fatalf("cache_control count = %d, want 1 (tools only): %s", got, sent(0))
	}
	body := decodeSent(t, sent(0))
	if control := body.Tools[1].CacheControl; control == nil || control.Type != "ephemeral" || body.Tools[0].CacheControl != nil {
		t.Fatalf("tools breakpoint wrong: %s", sent(0))
	}
	request.CacheKey = ""
	if _, err := runtime.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sent(1)), "cache_control") {
		t.Fatalf("request without cache key has breakpoints: %s", sent(1))
	}
}

func TestUserImagesBecomeBase64Blocks(t *testing.T) {
	runtime, sent := newTestRuntime(t, textReply("A cat."))
	_, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{
		Role: "user", Content: "What is this?",
		Images: []providers.ImageContent{{MIMEType: "image/png; charset=binary", DataBase64: "iVBORw0KGgo="}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	content := decodeSent(t, sent(0)).Messages[0].Content
	if blockTypes(content) != "image,text" {
		t.Fatalf("content = %s", sent(0))
	}
	if source := content[0].Source; source == nil || source.Type != "base64" || source.MediaType != "image/png" || source.Data != "iVBORw0KGgo=" {
		t.Fatalf("image source = %#v", content[0].Source)
	}
}

func TestStopReasonsWithThinkingAndTools(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		want        providers.StopReason
		calls       int
	}{
		{"output limit spent on thinking", `{"content":[{"type":"thinking","thinking":"long","signature":"s"}],"stop_reason":"max_tokens"}`, providers.StopMaxTokens, 0},
		{"context window exceeded", `{"content":[{"type":"text","text":"Cut"}],"stop_reason":"model_context_window_exceeded"}`, providers.StopMaxTokens, 0},
		{"end_turn with tool calls", `{"content":[{"type":"tool_use","id":"toolu_1","name":"ls","input":{}}],"stop_reason":"end_turn"}`, providers.StopToolUse, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, _ := newTestRuntime(t, tc.reply)
			response, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "Go"}}, Tools: testTools})
			if err != nil {
				t.Fatal(err)
			}
			if response.StopReason != tc.want || len(response.ToolCalls) != tc.calls {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestToolUseWithoutNameIsAMalformedCall(t *testing.T) {
	runtime, _ := newTestRuntime(t, `{"content":[{"type":"tool_use","id":"toolu_1","input":{}}],"stop_reason":"tool_use"}`)
	_, err := runtime.Generate(context.Background(), providers.Request{Messages: []providers.Message{{Role: "user", Content: "Go"}}, Tools: testTools})
	if !errors.Is(err, providers.ErrMalformedToolCall) {
		t.Fatalf("error = %v, want ErrMalformedToolCall", err)
	}
}

func TestDisabledToolUseModeStripsToolsAndToolTurns(t *testing.T) {
	server, sent := startServer(t, textReply("Plain."))
	runtime, err := New(context.Background(), Config{
		APIKey: "test-key", BaseURL: server.URL, Model: "claude-test", HTTPClient: server.Client(),
		ToolUseMode: providers.ToolUseDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Generate(context.Background(), providers.Request{
		Tools: testTools,
		Messages: []providers.Message{
			{Role: "user", Content: "Inspect"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "toolu_1", Name: "ls", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "a.go"},
			{Role: "user", Content: "continue"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(sent(0))
	for _, forbidden := range []string{`"tools"`, "tool_use", "tool_result"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("disabled tool use still sends %s: %s", forbidden, body)
		}
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/providers/ai/anthropiccompat -count=1`
Expected: FAIL at build, with `unknown field ToolUseMode in struct literal of type Config` and `undefined: anthropicBlock`.

- [ ] **Step 3: Create the wire types**

Create `internal/providers/ai/anthropiccompat/wire.go`. `anthropicRequest`, `anthropicMessage` and `anthropicResponse` move here from `adapter.go` in a new shape (Step 6 deletes the old definitions):

```go
package anthropic

import "encoding/json"

type anthropicRequest struct {
	Model      string               `json:"model"`
	MaxTokens  int64                `json:"max_tokens"`
	System     []anthropicBlock     `json:"system,omitempty"`
	Messages   []anthropicMessage   `json:"messages"`
	Tools      []anthropicTool      `json:"tools,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
	Stream     bool                 `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

// anthropicBlock is any Messages API content block, sent or received.
// Thinking is a pointer so a thinking block always carries the key, even empty.
type anthropicBlock struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	Thinking  *string               `json:"thinking,omitempty"`
	Signature string                `json:"signature,omitempty"`
	Data      string                `json:"data,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	Content   string                `json:"content,omitempty"`
	IsError   bool                  `json:"is_error,omitempty"`
	Source    *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
}

type anthropicResponse struct {
	Content    []anthropicBlock      `json:"content"`
	StopReason string                `json:"stop_reason,omitempty"`
	Usage      anthropicUsagePayload `json:"usage"`
}
```

- [ ] **Step 4: Create the request encoder**

Create `internal/providers/ai/anthropiccompat/encode.go`:

```go
package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var emptyToolSchema = json.RawMessage(`{"type":"object","properties":{}}`)

// encodeRequest fills system, tools, tool choice and messages of payload.
// The tools cache breakpoint is set only for session requests (non-empty CacheKey).
func encodeRequest(payload *anthropicRequest, request providers.Request) error {
	if system := combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions); system != "" {
		payload.System = []anthropicBlock{{Type: "text", Text: system}}
	}
	payload.Tools = encodeTools(request.Tools)
	if len(payload.Tools) > 0 && request.ToolChoice == providers.ToolChoiceNone {
		payload.ToolChoice = &anthropicToolChoice{Type: "none"}
	}
	payload.Messages = encodeMessages(request.Messages)
	if len(payload.Messages) == 0 {
		return errors.New("anthropic: no messages")
	}
	if strings.TrimSpace(request.CacheKey) != "" {
		markToolsCacheBreakpoint(payload.Tools)
	}
	return nil
}

func combinedSystemPrompt(systemPrompt string, customInstructions string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	customInstructions = strings.TrimSpace(customInstructions)
	if customInstructions == "" {
		return systemPrompt
	}
	block := "User custom instructions:\n" + customInstructions
	if systemPrompt == "" {
		return block
	}
	return systemPrompt + "\n\n" + block
}

func encodeTools(tools []providers.ToolDefinition) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, tool := range tools {
		schema := json.RawMessage(bytes.TrimSpace(tool.InputSchema))
		if len(schema) == 0 {
			schema = emptyToolSchema
		}
		out = append(out, anthropicTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	return out
}

// encodeMessages merges consecutive same-role messages into one turn, so the
// results of one tool step and any user text after them form one user turn.
func encodeMessages(messages []providers.Message) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(messages))
	for _, message := range messages {
		role, blocks := encodeMessage(message)
		if len(blocks) == 0 {
			continue
		}
		if last := len(out) - 1; last >= 0 && out[last].Role == role {
			out[last].Content = orderTurnBlocks(role, append(out[last].Content, blocks...))
			continue
		}
		out = append(out, anthropicMessage{Role: role, Content: orderTurnBlocks(role, blocks)})
	}
	return out
}

func encodeMessage(message providers.Message) (string, []anthropicBlock) {
	switch strings.ToLower(strings.TrimSpace(message.Role)) {
	case "assistant":
		return "assistant", assistantBlocks(message)
	case "tool":
		if id := sanitizeToolID(message.ToolCallID); id != "" {
			return "user", []anthropicBlock{toolResultBlock(id, message)}
		}
	}
	return "user", userBlocks(message)
}

// assistantBlocks replays only signed or redacted reasoning; unsigned reasoning
// from other providers cannot be verified by Anthropic and is not sent.
func assistantBlocks(message providers.Message) []anthropicBlock {
	text := strings.TrimSpace(message.Content)
	if text == "" && len(message.ToolCalls) == 0 {
		return nil
	}
	blocks := make([]anthropicBlock, 0, len(message.Reasoning)+1+len(message.ToolCalls))
	for _, reasoning := range message.Reasoning {
		switch {
		case reasoning.RedactedData != "":
			blocks = append(blocks, anthropicBlock{Type: "redacted_thinking", Data: reasoning.RedactedData})
		case reasoning.Signature != "":
			thinking := reasoning.Text
			blocks = append(blocks, anthropicBlock{Type: "thinking", Thinking: &thinking, Signature: reasoning.Signature})
		}
	}
	if text != "" {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: text})
	}
	for _, call := range message.ToolCalls {
		blocks = append(blocks, anthropicBlock{
			Type:  "tool_use",
			ID:    sanitizeToolID(call.ID),
			Name:  strings.TrimSpace(call.Name),
			Input: toolUseInput(call.Arguments),
		})
	}
	return blocks
}

func userBlocks(message providers.Message) []anthropicBlock {
	blocks := make([]anthropicBlock, 0, len(message.Images)+1)
	for _, image := range message.Images {
		data := strings.TrimSpace(image.DataBase64)
		if data == "" {
			continue
		}
		blocks = append(blocks, anthropicBlock{
			Type:   "image",
			Source: &anthropicImageSource{Type: "base64", MediaType: imageMediaType(image.MIMEType), Data: data},
		})
	}
	if text := strings.TrimSpace(message.Content); text != "" {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: text})
	}
	return blocks
}

func toolResultBlock(toolUseID string, message providers.Message) anthropicBlock {
	content := strings.TrimSpace(message.Content)
	if content == "" {
		content = "(empty result)"
	}
	return anthropicBlock{Type: "tool_result", ToolUseID: toolUseID, Content: content, IsError: message.IsError}
}

// orderTurnBlocks puts thinking first in an assistant turn and tool results
// first in a user turn, as the Messages API requires.
func orderTurnBlocks(role string, blocks []anthropicBlock) []anthropicBlock {
	leads := func(block anthropicBlock) bool {
		if role == "assistant" {
			return block.Type == "thinking" || block.Type == "redacted_thinking"
		}
		return block.Type == "tool_result"
	}
	ordered := make([]anthropicBlock, 0, len(blocks))
	for _, block := range blocks {
		if leads(block) {
			ordered = append(ordered, block)
		}
	}
	for _, block := range blocks {
		if !leads(block) {
			ordered = append(ordered, block)
		}
	}
	return ordered
}

// toolUseInput returns the call arguments as a JSON object; anything else is
// sent as {} because the Messages API rejects non-object input.
func toolUseInput(arguments json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(arguments)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(trimmed)
}

// sanitizeToolID maps ids from other providers onto Anthropic's ^[a-zA-Z0-9_-]+$.
func sanitizeToolID(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.TrimSpace(id))
}

func imageMediaType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = strings.TrimSpace(mimeType[:index])
	}
	if mimeType == "" {
		return "image/jpeg"
	}
	return mimeType
}

// markToolsCacheBreakpoint caches the tool definitions: they open the cached
// prefix and stay the same across the turns of a session.
func markToolsCacheBreakpoint(tools []anthropicTool) {
	if last := len(tools) - 1; last >= 0 {
		tools[last].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
}
```

- [ ] **Step 5: Create the response assembly**

Create `internal/providers/ai/anthropiccompat/decode.go`. It uses Stage 1's `anthropicStopReason` (in `adapter.go`) and Stage 0's `anthropicUsage`:

```go
package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (r *Runtime) decodeResponse(body []byte) (providers.Response, error) {
	var response anthropicResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	return r.assembleResponse(response.Content, response.StopReason, response.Usage)
}

// assembleResponse turns final content blocks into a provider response. A tool
// call cut by the output limit is dropped; any other malformed call is an error.
func (r *Runtime) assembleResponse(blocks []anthropicBlock, rawStopReason string, usage anthropicUsagePayload) (providers.Response, error) {
	stop := anthropicStopReason(rawStopReason)
	var text strings.Builder
	var calls []providers.ToolCall
	var reasoning []providers.ReasoningBlock
	for _, block := range blocks {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "thinking":
			thinking := ""
			if block.Thinking != nil {
				thinking = *block.Thinking
			}
			reasoning = append(reasoning, providers.ReasoningBlock{Text: thinking, Signature: block.Signature})
		case "redacted_thinking":
			reasoning = append(reasoning, providers.ReasoningBlock{RedactedData: block.Data})
		case "tool_use":
			call, err := decodeToolUse(block)
			if err != nil {
				if stop == providers.StopMaxTokens {
					continue
				}
				return providers.Response{}, err
			}
			calls = append(calls, call)
		}
	}
	stop = providers.ResolveStopReason(stop, len(calls))
	reply := strings.TrimSpace(text.String())
	if reply == "" && len(calls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("anthropic: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{
		Text:       reply,
		Model:      r.model,
		Provider:   providers.TypeAnthropic,
		Reasoning:  reasoning,
		ToolCalls:  calls,
		StopReason: stop,
		Usage:      anthropicUsage(usage),
	}, nil
}

func decodeToolUse(block anthropicBlock) (providers.ToolCall, error) {
	name := strings.TrimSpace(block.Name)
	if name == "" {
		return providers.ToolCall{}, fmt.Errorf("anthropic: tool call %q has no name: %w", block.ID, providers.ErrMalformedToolCall)
	}
	input := bytes.TrimSpace(block.Input)
	if len(input) == 0 {
		input = []byte(`{}`)
	}
	if !json.Valid(input) {
		return providers.ToolCall{}, fmt.Errorf("anthropic: tool call %q has malformed input JSON: %w", name, providers.ErrMalformedToolCall)
	}
	return providers.ToolCall{ID: strings.TrimSpace(block.ID), Name: name, Arguments: json.RawMessage(input)}, nil
}
```

- [ ] **Step 6: Wire `Generate`, `Config` and the factory**

In `internal/providers/ai/anthropiccompat/adapter.go`:

1. In `type Config struct`, add this field below `MaxOutputTokens int64`:

```go
	ToolUseMode     providers.ToolUseMode
```

2. In `New`, in the returned `&Runtime{...}` literal, change `profile:      providerProfile.RuntimeProfile,` to:

```go
		profile:      providerProfile.RuntimeProfileWithOverrides(providers.RuntimeProfile{ToolUseMode: cfg.ToolUseMode}),
```

3. In `Generate`, delete:

```go
	if unsupported := unsupportedToolUse(request); unsupported != "" {
		return providers.Response{}, fmt.Errorf("anthropic: tool use disabled by runtime profile; unsupported %s present", unsupported)
	}

```

4. In `Generate`, replace:

```go
	payload := anthropicRequest{
		Model:     r.model,
		MaxTokens: providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxTokens, r.providerID, providers.TypeAnthropic, r.model),
		System:    combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions),
		Messages:  make([]anthropicMessage, 0, len(request.Messages)),
	}

	for _, message := range request.Messages {
		if content := strings.TrimSpace(message.Content); content != "" {
			payload.Messages = append(payload.Messages, anthropicMessage{
				Role:    normalizeAnthropicRole(message.Role),
				Content: content,
			})
		}
	}

	if len(payload.Messages) == 0 {
		return providers.Response{}, errors.New("anthropic: no messages")
	}
```

with:

```go
	payload := anthropicRequest{
		Model:     r.model,
		MaxTokens: providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxTokens, r.providerID, providers.TypeAnthropic, r.model),
	}
	if err := encodeRequest(&payload, request); err != nil {
		return providers.Response{}, err
	}
```

5. In the non-streaming tail of `Generate`, replace everything from `var response anthropicResponse` to the closing brace of `Generate`. That is the JSON decode, the `var text strings.Builder` loop, Stage 1's `reply` / `stop` / `ErrEmptyResponse` check and the returned `providers.Response`. The replacement is:

```go
	return r.decodeResponse(resBody)
}
```

6. Delete these declarations from `adapter.go`:
   - `func unsupportedToolUse`
   - `func combinedSystemPrompt` (now in `encode.go`)
   - `type anthropicRequest`, `type anthropicMessage` and `type anthropicResponse` (now in `wire.go`)
   - `func normalizeAnthropicRole`

   Keep `anthropicStopReason`, `metadataProviderID`, `anthropicUsagePayload`, `anthropicUsage`, `mergeAnthropicUsage`, the error helpers, `ListModels` and `normalizeConfig`. Also keep `anthropicStreamDelta` and `decodeStream`; Task 2 replaces them.

In `internal/providers/factory/factory.go`, in `case providers.TypeAnthropic:`, add below `MaxOutputTokens: cfg.MaxOutputTokens,`:

```go
			ToolUseMode:     cfg.ToolUseMode,
```

- [ ] **Step 7: Build**

Run: `gofmt -w internal/providers/ai/anthropiccompat internal/providers/factory && $(go env GOPATH)/bin/goimports -w internal/providers/ai/anthropiccompat/adapter.go && go build ./...`
Expected: no output. `goimports` removes imports in `adapter.go` that are no longer used.

- [ ] **Step 8: Run the tests and confirm they pass**

Run: `go test ./internal/providers/ai/anthropiccompat -count=1 -v`
Expected: every test PASSes, including Stage 0's `TestUsageCountsCachedInputAsPrompt` and Stage 1's `TestTextReplyCarriesStopReasonAndOutputLimit`, `TestStreamedRefusalIsAValidEmptyReply`, `TestListModelsRegistersOutputLimit` and the existing `TestStreamDoesNotCompleteBeforeMessageStop`. The run ends with `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/anthropiccompat`.

- [ ] **Step 9: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 10: Commit**

```bash
git add internal/providers/ai/anthropiccompat/wire.go internal/providers/ai/anthropiccompat/encode.go internal/providers/ai/anthropiccompat/decode.go internal/providers/ai/anthropiccompat/adapter.go internal/providers/ai/anthropiccompat/adapter_test.go internal/providers/factory/factory.go
git commit -m "feat(anthropic): encode tool_use/tool_result, tool_choice, thinking replay and a tools cache breakpoint

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Streamed tool calls, thinking and signatures

The SSE decoder collects content blocks by `index`:
- text deltas are streamed as they arrive;
- `input_json_delta` fragments are joined into the tool input;
- `thinking_delta` and `signature_delta` are collected.

Usage comes from `message_start`, then from the cumulative `message_delta` counts (Stage 0's `mergeAnthropicUsage`). The stop reason comes from `message_delta`. The stream is complete only after `message_stop`; this is the 2026-09-17 terminal-event rule. The final blocks go through `assembleResponse`, as in the JSON path.

**Files:**
- Create: `internal/providers/ai/anthropiccompat/stream.go`
- Modify: `internal/providers/ai/anthropiccompat/adapter.go` (delete `anthropicStreamDelta` and `decodeStream`)
- Replace: `internal/providers/ai/anthropiccompat/stream_test.go`

- [ ] **Step 1: Write the failing tests**

Replace the full contents of `internal/providers/ai/anthropiccompat/stream_test.go` with:

```go
package anthropic

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func frame(eventType string, data string) string {
	return "event: " + eventType + "\ndata: " + data + "\n\n"
}

func toolStream(partialJSON string, stopReason string) string {
	return frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5,"output_tokens":1}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me read"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_a","name":"read","input":{}}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":`+partialJSON+`}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":1}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+stopReason+`","stop_sequence":null},"usage":{"output_tokens":9}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
}

func TestStreamAssemblesThinkingTextAndParallelToolCalls(t *testing.T) {
	stream := frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":100,"output_tokens":1}}}`) +
		frame("ping", `{"type":"ping"}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Read both "}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"files."}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-stream"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Reading."}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":1}`) +
		frame("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_a","name":"read","input":{}}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":2}`) +
		frame("content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_b","name":"ls","input":{}}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":3}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":42}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	var deltas []string
	ctx := providers.WithTextStream(context.Background(), func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	response, err := (&Runtime{model: "claude-test"}).decodeStream(ctx, strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "Reading." || strings.Join(deltas, "") != "Reading." || response.StopReason != providers.StopToolUse {
		t.Fatalf("response = %#v deltas = %q", response, deltas)
	}
	if len(response.ToolCalls) != 2 {
		t.Fatalf("tool calls = %#v", response.ToolCalls)
	}
	if call := response.ToolCalls[0]; call.ID != "toolu_a" || call.Name != "read" || string(call.Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("first call = %#v", call)
	}
	if call := response.ToolCalls[1]; call.ID != "toolu_b" || call.Name != "ls" || string(call.Arguments) != `{}` {
		t.Fatalf("second call = %#v", call)
	}
	if len(response.Reasoning) != 1 || response.Reasoning[0] != (providers.ReasoningBlock{Text: "Read both files.", Signature: "sig-stream"}) {
		t.Fatalf("reasoning = %#v", response.Reasoning)
	}
	if response.Usage.PromptTokens != 105 || response.Usage.CacheReadTokens != 100 || response.Usage.OutputTokens != 42 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestStreamRejectsMalformedToolInput(t *testing.T) {
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(toolStream(`"{\"path\":"`, "tool_use")))
	if !errors.Is(err, providers.ErrMalformedToolCall) || len(response.ToolCalls) != 0 {
		t.Fatalf("response = %#v err = %v", response, err)
	}
}

func TestStreamDropsToolCallTruncatedByMaxTokens(t *testing.T) {
	response, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(toolStream(`"{\"path\":"`, "max_tokens")))
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != providers.StopMaxTokens || response.Text != "Let me read" || len(response.ToolCalls) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestStreamDoesNotCompleteBeforeMessageStop(t *testing.T) {
	partial := frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`)
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(partial)); !errors.Is(err, providers.ErrIncompleteResponse) {
		t.Fatalf("truncated stream error = %v", err)
	}
	complete := partial + frame("message_stop", `{"type":"message_stop"}`)
	response, err := (&Runtime{}).decodeStream(context.Background(), io.MultiReader(strings.NewReader(complete), failIfRead{t}))
	if err != nil || response.Text != "Hello" || response.StopReason != providers.StopEndTurn {
		t.Fatalf("response = %#v err = %v", response, err)
	}
}

func TestStreamErrorEventFails(t *testing.T) {
	stream := frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5}}}`) +
		frame("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	if _, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(stream)); err == nil || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("error = %v", err)
	}
}

func TestGenerateStreamsToolCallsOverSSE(t *testing.T) {
	runtime, sent := newTestRuntime(t, toolStream(`"{\"path\":\"a.go\"}"`, "tool_use"))
	var streamed strings.Builder
	ctx := providers.WithTextStream(context.Background(), func(delta string) error {
		streamed.WriteString(delta)
		return nil
	})
	response, err := runtime.Generate(ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "Read a.go"}}, Tools: testTools})
	if err != nil {
		t.Fatal(err)
	}
	if !decodeSent(t, sent(0)).Stream {
		t.Fatalf("request did not ask for a stream: %s", sent(0))
	}
	if streamed.String() != "Let me read" || len(response.ToolCalls) != 1 || string(response.ToolCalls[0].Arguments) != `{"path":"a.go"}` {
		t.Fatalf("response = %#v streamed = %q", response, streamed.String())
	}
}

type failIfRead struct{ t *testing.T }

func (r failIfRead) Read([]byte) (int, error) {
	r.t.Error("read beyond message_stop")
	return 0, io.ErrUnexpectedEOF
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/providers/ai/anthropiccompat -run 'TestStream|TestGenerateStreams' -count=1`
Expected: FAIL. For example, `TestStreamAssemblesThinkingTextAndParallelToolCalls` reports `tool calls = []`, and `TestStreamRejectsMalformedToolInput` reports `err = <nil>`.

- [ ] **Step 3: Implement the SSE decoder**

Create `internal/providers/ai/anthropiccompat/stream.go`:

```go
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage anthropicUsagePayload `json:"usage"`
	} `json:"message"`
	ContentBlock anthropicBlock `json:"content_block"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage anthropicUsagePayload `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// streamBlock accumulates one content block across its deltas.
type streamBlock struct {
	block     anthropicBlock
	text      strings.Builder
	input     strings.Builder
	signature strings.Builder
}

func (b *streamBlock) final() anthropicBlock {
	block := b.block
	switch block.Type {
	case "text":
		block.Text = b.text.String()
	case "thinking":
		thinking := b.text.String()
		block.Thinking = &thinking
		block.Signature += b.signature.String()
	case "tool_use":
		if b.input.Len() > 0 {
			block.Input = json.RawMessage(b.input.String())
		}
	}
	return block
}

func streamDeltaBlockType(deltaType string) string {
	switch deltaType {
	case "", "text_delta":
		return "text"
	case "input_json_delta":
		return "tool_use"
	case "thinking_delta", "signature_delta":
		return "thinking"
	default:
		return ""
	}
}

func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	blocks := map[int]*streamBlock{}
	var usage anthropicUsagePayload
	stopReason := ""
	completed := false
	err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		if event.Data == "" {
			return nil
		}
		var chunk anthropicStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("anthropic: decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("anthropic: stream error: %s", strings.TrimSpace(chunk.Error.Message))
		}
		eventType := event.Type
		if eventType == "" {
			eventType = chunk.Type
		}
		switch eventType {
		case "message_start":
			usage = chunk.Message.Usage
		case "content_block_start":
			started := &streamBlock{block: chunk.ContentBlock}
			blocks[chunk.Index] = started
			switch chunk.ContentBlock.Type {
			case "text":
				started.text.WriteString(chunk.ContentBlock.Text)
				return providers.StreamText(ctx, chunk.ContentBlock.Text)
			case "thinking":
				if chunk.ContentBlock.Thinking != nil {
					started.text.WriteString(*chunk.ContentBlock.Thinking)
				}
			}
		case "content_block_delta":
			blockType := streamDeltaBlockType(chunk.Delta.Type)
			if blockType == "" {
				return nil
			}
			block := blocks[chunk.Index]
			if block == nil {
				block = &streamBlock{block: anthropicBlock{Type: blockType}}
				blocks[chunk.Index] = block
			}
			switch chunk.Delta.Type {
			case "input_json_delta":
				block.input.WriteString(chunk.Delta.PartialJSON)
			case "thinking_delta":
				block.text.WriteString(chunk.Delta.Thinking)
			case "signature_delta":
				block.signature.WriteString(chunk.Delta.Signature)
			default:
				block.text.WriteString(chunk.Delta.Text)
				return providers.StreamText(ctx, chunk.Delta.Text)
			}
		case "message_delta":
			usage = mergeAnthropicUsage(usage, chunk.Usage)
			if chunk.Delta.StopReason != "" {
				stopReason = chunk.Delta.StopReason
			}
		case "message_stop":
			completed = true
			return providers.ErrSSEComplete
		}
		return nil
	})
	if err != nil {
		return providers.Response{}, err
	}
	if !completed {
		return providers.Response{}, fmt.Errorf("anthropic: %w", providers.ErrIncompleteResponse)
	}
	indices := make([]int, 0, len(blocks))
	for index := range blocks {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	final := make([]anthropicBlock, 0, len(indices))
	for _, index := range indices {
		final = append(final, blocks[index].final())
	}
	return r.assembleResponse(final, stopReason, usage)
}
```

A delta for an index without `content_block_start` creates the block from the delta type. Stage 0's `usage_test.go` stream sends `content_block_delta` without an index or start event and must keep passing.

- [ ] **Step 4: Delete the old stream code**

In `internal/providers/ai/anthropiccompat/adapter.go`, delete `type anthropicStreamDelta struct { ... }` and the whole old `func (r *Runtime) decodeStream(...)`. Then run:

`gofmt -w internal/providers/ai/anthropiccompat && $(go env GOPATH)/bin/goimports -w internal/providers/ai/anthropiccompat/adapter.go && go build ./...`
Expected: no output.

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/providers/ai/anthropiccompat -count=1 -v`
Expected: every test PASSes, including Stage 0's `TestUsageCountsCachedInputAsPrompt` (stream case) and Stage 1's `TestStreamedRefusalIsAValidEmptyReply`. The run ends with `ok  	github.com/Suren878/matrixclaw/internal/providers/ai/anthropiccompat`.

- [ ] **Step 6: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/ai/anthropiccompat/stream.go internal/providers/ai/anthropiccompat/stream_test.go internal/providers/ai/anthropiccompat/adapter.go
git commit -m "feat(anthropic): assemble streamed tool calls, thinking and signatures

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Enable tool use for Anthropic by default

Tool use is forced off for Anthropic in three places:
- `runtimeProfileDefaults` returns `ToolUseDisabled` for `TypeAnthropic`.
- The Anthropic catalog capabilities lack `ToolCalling`, so `ProfileForModel` sets `ToolUseDisabled` anyway.
- `core.runtimeToolUseAllowed`, which reads `ModelCapabilities().ToolCalling`, returns false.

The profile default and the capabilities must both change.

**Files:**
- Modify: `internal/providers/request_normalize.go` (`runtimeProfileDefaults`)
- Modify: `internal/providers/provider_anthropic.go`
- Modify: `internal/providers/catalog.go` (`defaultCapabilitiesForCustomProviderType`)
- Create: `internal/providers/provider_profile_test.go`
- Modify: `internal/providers/ai/anthropiccompat/adapter_test.go` (`newTestRuntime`)

- [ ] **Step 1: Write the failing tests**

Create `internal/providers/provider_profile_test.go`:

```go
package providers

import "testing"

func TestAnthropicProfilesUseNativeToolCalling(t *testing.T) {
	for _, tc := range []struct{ providerID, modelID string }{
		{"anthropic", "claude-sonnet-4-5"},
		{"custom-anthropic-compatible", "glm-4.6"},
		{"", ""},
	} {
		profile := ProfileForModel(tc.providerID, TypeAnthropic, tc.modelID)
		if profile.RuntimeProfile.ToolUseMode != ToolUseNative || !profile.Capabilities.ToolCalling {
			t.Errorf("%q/%q: tool use mode = %q, tool calling = %t", tc.providerID, tc.modelID, profile.RuntimeProfile.ToolUseMode, profile.Capabilities.ToolCalling)
		}
		if profile.RuntimeProfile.ToolSchemaDialect != ToolSchemaJSONSchema {
			t.Errorf("%q/%q: schema dialect = %q", tc.providerID, tc.modelID, profile.RuntimeProfile.ToolSchemaDialect)
		}
	}
}
```

In `internal/providers/ai/anthropiccompat/adapter_test.go`, delete the `ToolUseMode: providers.ToolUseNative,` line from `newTestRuntime`, so that every test using the helper runs with the default profile:

```go
	runtime, err := New(context.Background(), Config{
		APIKey: "test-key", BaseURL: server.URL, Model: "claude-test", HTTPClient: server.Client(),
	})
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/providers ./internal/providers/ai/anthropiccompat -count=1`
Expected: FAIL.
- `TestAnthropicProfilesUseNativeToolCalling` reports `tool use mode = "disabled", tool calling = false`.
- `TestToolRoundTripSendsThinkingToolUseAndGroupedResults` reports `first request = …` without `"tools"`.

- [ ] **Step 3: Enable tool calling**

In `internal/providers/request_normalize.go`, delete this block from `runtimeProfileDefaults`:

```go
	if providerType == TypeAnthropic {
		return RuntimeProfile{
			ToolUseMode:       ToolUseDisabled,
			ToolSchemaDialect: ToolSchemaJSONSchema,
		}
	}
```

In `internal/providers/provider_anthropic.go`, change the capabilities line to:

```go
			Capabilities:    Capabilities{ModelDiscovery: true, ToolCalling: true, ImageInput: true},
```

In `internal/providers/catalog.go`, in `defaultCapabilitiesForCustomProviderType`, change the Anthropic case to:

```go
	case TypeAnthropic:
		return Capabilities{ModelDiscovery: true, ToolCalling: true}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `gofmt -w internal/providers && go test ./internal/providers/... ./internal/core/... -count=1 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 5: Full suite**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/request_normalize.go internal/providers/provider_anthropic.go internal/providers/catalog.go internal/providers/provider_profile_test.go internal/providers/ai/anthropiccompat/adapter_test.go
git commit -m "feat(providers): enable native tool use for Anthropic

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Stage verification

**Files:** none changed.

- [ ] **Step 1: Static checks and the full suite, uncached**

Run: `gofmt -l internal/ && go vet ./... && go test ./... -count=1 2>&1 | grep -v '^ok\|no test files'`
Expected: no output.

- [ ] **Step 2: No leftovers**

Run: `grep -rn 'unsupportedToolUse\|normalizeAnthropicRole\|anthropicStreamDelta\|tool use disabled by runtime profile' internal/`
Expected: no output.

- [ ] **Step 3: Build binaries**

Run: `go build -o ./bin/matrixclaw ./cmd/matrixclaw && go build -o ./bin/matrixclawd ./cmd/matrixclawd`
Expected: exit code 0.

- [ ] **Step 4: Manual check on the test stand (spec: every stage)**

Deploy the stand the usual way; the stand procedure is not in the repo, so ask the owner if unsure. Select an Anthropic provider in the TUI, start a new session and send:
`Read go.mod and list the internal/ directory - do both at once - then tell me the module path and the number of entries.`

Confirm:
- the run completes and the TUI shows two tool calls from one step;
- the daemon log has no `anthropic: status 400`;
- `sqlite3 <db> 'select step, stop_reason, cache_read_tokens, cache_write_tokens from run_steps order by rowid desc limit 3'` shows `tool_use` then `end_turn`, `cache_write_tokens > 0` on the first step and `cache_read_tokens > 0` on the second (only the tool definitions are cached in this stage). If both steps show 0 cache tokens, the tool definitions are below the model's minimum cacheable prompt length; report that rather than treating it as a failure.

If the stand has no Anthropic key, report that instead of reporting the check as passed. No commit in this task.

---

## Self-Review

**1. Spec coverage** (spec §5 anthropiccompat, §3 caching, §Testing, and the contract "Stage 1b provides", as amended by Stage 1):

| Requirement | Where |
|---|---|
| Real `tool_use` / `tool_result` blocks | Task 1 (encode, JSON decode), Task 2 (stream) |
| Parallel tool results in one user message | Stage 1 Task 4 (one assistant message per step, results in call order) + Task 1 `encodeMessages` merge (`TestToolRoundTrip…`, `TestMessagesMergeIntoValidTurns`) |
| `tool_choice: {type: none}` with tools still defined | Task 1 `TestToolChoiceNoneKeepsToolsDefined` |
| `stop_reason` mapping | Stage 1 Task 12 (`anthropicStopReason`) reused by `assembleResponse` for tool replies; `TestStopReasonsWithThinkingAndTools`, stream tests |
| `cache_control` (narrowed by owner decision: tools only; system and message breakpoints deferred to Stage 3, like Stage 1's OpenRouter `cache_control`) | Task 1 `markToolsCacheBreakpoint`, `TestCacheBreakpointMarksOnlyTheTools` (count = 1; none without `CacheKey`) |
| Thinking blocks passed back unchanged (`ReasoningPart.Signature`, `RedactedData`) | Stage 1 Task 4 stores and replays the blocks; Task 1 sends `thinking` / `redacted_thinking` (round trip, empty-thinking test); Task 2 `signature_delta` |
| `is_error` results | Stage 1 `Message.IsError`; Task 1 round trip |
| Streamed tool-call assembly; malformed → retryable error; truncated at `max_tokens` → dropped | Task 2 |
| `message_stop` still required | Task 2 `TestStreamDoesNotCompleteBeforeMessageStop` |
| Images now reach Anthropic once tools are enabled | Task 1 `TestUserImagesBecomeBase64Blocks` |
| Remove `ToolUseDisabled` default for Anthropic, catalog `ToolCalling` | Task 3 |
| `CacheKey`, `MaxOutputTokens`, idle timeouts, normalised usage | Stage 1 Task 3 / Task 12, Stage 0 Tasks 7–8 (used, not changed) |

**2. Placeholder scan:** There is no TBD, TODO or conditional "if Stage 1 did not…" branch. Each edit to `adapter.go` quotes the exact post-Stage-1 code it replaces.

**3. Type and name consistency:**
- Stage 0/1 names used exactly:
  - `anthropicStopReason`, `providers.ResolveStopReason`, `StopReason.AllowsEmptyReply`, `providers.ErrMalformedToolCall`, `providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxTokens, r.providerID, providers.TypeAnthropic, r.model)` (returns `int64`, matching `anthropicRequest.MaxTokens int64`);
  - `anthropicUsage`, `mergeAnthropicUsage`, `anthropicUsagePayload`;
  - `providers.ReasoningBlock{Text, Signature, RedactedData}`, `Message.Reasoning`, `Message.IsError`, `Response.Reasoning`, `Request.CacheKey`, `Request.ToolChoice`, `ToolChoiceNone`.
- New helpers are defined once each:
  - `encodeRequest`, `markToolsCacheBreakpoint`, `assembleResponse`, `decodeResponse`, `decodeToolUse`, `anthropicStreamEvent`, `streamBlock`.
  - Test helpers `startServer`, `newTestRuntime`, `decodeSent`, `blockTypes`, `textReply`, `testTools`, `frame`, `toolStream`, `failIfRead`.
  - None of these collide with Stage 0's `anthropicStreamWithUsage` or Stage 1's test names in the same package.
- `providers.ReasoningBlock` contains only strings, so the `!=` comparisons in the tests compile.
