# Terminal: one read model, shared tool presentation, daemon-owned context

Phase C #7 of `docs/superpowers/plans/2026-10-03-audit-cleanup.md` (terminal
report items 4, 11, 12, 13, 14, 15, 17, 19, 22).

## Current shape

- **Read model, three layers.** `internal/clientruntime/state.go` (`State`,
  RWMutex, maps, `Snapshot()` clones and sorts everything) is used only by the
  TUI. `chat/viewmodel` wraps it (`ReadModel`) and `FromStateSnapshot` clones
  again and re-converts every transcript message to `surfacemessage.Message`.
  `appModel.currentSnapshot()` runs both on each call; `View()` reaches it ~12
  times (header, working status, todo panel, menu), on every 120 ms tick and
  mouse motion.
- **Chat rebuilt per event.** `rebuildChat` (`app_chat_sync.go`) builds a new
  `surfacemodel.Chat` with fresh items for every live event, dropping item
  render caches and the user's expanded/collapsed toggles; `bodyBounds` /
  `editorBounds` / `resizeChat` each recompute `layout()` (renders header,
  status, input section).
- **Optimistic fakes + reloads.** `applyResolvedApproval` and
  `applyAcceptedRunToReadModel` fabricate `LiveEvent`s, then the handlers call
  `loadInitialCmd` anyway — after each send, approval, cancel and run end — and
  every snapshot restarts the event stream.
- **Subagents as fake tool results.** `conversation.go`
  (`mergeSubagentToolResults`) `json.Marshal`s a `core.SubagentTask` into a
  tool result's `Metadata`; `ui/surface/chat/agent.go` and `tool_preview.go`
  parse it back; status/name/terminal logic exists in four places with string
  statuses (`app_layout_views.go` has a fifth display-name fallback).
- **Three tool tables.** `ui/surface/chat/generic.go` (`genericToolParams`,
  `genericPrettyName`), `chat/runtime/app_layout_views.go` (`workingToolPhase`,
  `workingToolDetail`) and `clients/telegram/run_tools.go`
  (`telegramToolAction`) each map tool → verb / main parameter / title, each
  with its own param flattening and shortening helpers. Renderers in
  `file.go`, `bash.go`, `search.go` repeat the same pending → parse → header →
  early state → body steps through 12 empty `*ToolRenderContext` structs;
  `diffPreviewData` re-parses write/edit/multiedit; the permissions dialog and
  the diff preview each keep their own split/unified diff cache.
- **Local context estimate.** `app_usage.go` adds its own token estimate of the
  visible messages plus `assistantPromptTokens()` (from setup.json via
  `Config.Assistant`) to the daemon's `ContextReport`; it imports
  `internal/agent/prompt` and `internal/agent/context`. Provider/model labels
  fall back to `Config.Provider`/`Model` (setup.json), plumbed from
  `internal/clientcmd/tui.go`.
- **Styles.** `ui/surface/styles/default_styles.go` builds the chat palette
  from `charmtone` colours mixed with a few `theme` constants;
  `ui/components/styles.go` and `setup/styles.go` use `theme`; `colorToHex`
  duplicates `colorx.Hex`.
- **`appModel`** (`app.go`): ~37 fields; `m.err` is both error line and
  controlplane result text; dialog navigation runs on `commandsDialogRoot` +
  `returnToCommands`, set/cleared in ~15 places.

## Target shape

### 1. `clients/terminal/chat/readmodel` (replaces `clientruntime.State` and `chat/viewmodel`)

```go
type Model struct { /* unexported; touched only from the Bubble Tea loop */ }
func New(snapshot core.ClientSnapshot) *Model
func (m *Model) Apply(event daemonclient.LiveEvent) (Change, error)
type Change struct {
    Messages []string  // message IDs created/updated
    Tools    []string  // tool call IDs whose state changed
    Run, Todo, Approvals, Subagents, Inputs, Session bool
}
// Read-only accessors, no copies (callers must not mutate):
SessionID() string; Session() *core.Session; Capabilities() core.SessionCapabilities
Context() *core.ContextReport; Todo() *todo.List; Run() *core.Run; Timing() *core.RunTiming
Messages() []surfacemessage.Message          // converted once, on upsert
Message(id) (surfacemessage.Message, uint64)  // with its revision
Tool(callID) ToolState                         // lifecycle, approval, result, subagent, revision
Approvals() []surfacepermission.PermissionRequest // ordered by path, id
Subagents() []Subagent; PendingInputs() []core.SessionInput
```

- Each upsert bumps a model-wide revision stored on the message / tool state;
  chat reconciliation compares revisions instead of contents.
- `Subagent` is the only subagent type outside the read model, built by one
  helper `subagentFrom(core.SubagentTask) Subagent` (name fallback, typed
  `SubagentState`, terminal(), summary/error). C1's task-model merge touches
  only that helper and the two decode sites (snapshot, `subagent.updated`).
- Tool items get their `*Subagent` directly (looked up by parent tool call);
  `mergeSubagentToolResults/Updates`, `parseSubagentTaskMetadata` and the
  string status switches go.
- `internal/clientruntime/state.go` and `chat/viewmodel` are deleted
  (`ToSurfaceMessage`, permission decode move into `readmodel/convert.go`).

### 2. Chat items reconciled by ID; layout once per frame

`chat/runtime/conversation.go` keeps building the item *list* (grouping read
calls, splitting assistant segments, info rows) but asks a keyed cache for
each item: same ID and same input revision → reuse the existing item (render
cache intact); changed → new item that inherits the old item's expanded state.
`surfacemodel.Chat` gets `SetItems` that keeps viewport/selection by ID instead
of being re-created. `appModel.relayout()` runs once at the end of `Update`,
stores `m.frame appLayout` and resizes the chat; `View` and mouse hit-testing
read `m.frame`.

### 3. Live events only; reload on (re)connect or session switch

No fabricated events. Send / approve / cancel update nothing locally except
`busy` and the editor; the daemon's `message.created`, `run.updated`,
`approval.resolved` events drive the model. A snapshot load happens on start,
reconnect, and controlplane results with `ReloadSnapshot` (session switch,
`/new`, `/clear`, model changes); the stream is resubscribed from the event ID
seen when the load was requested, so nothing in between is lost (all events
are idempotent upserts).

### 4. `internal/toolview` (shared by the TUI and Telegram)

```go
type Call struct {
    Name   string     // normalized tool id
    Title  string     // "Run", "Edit", "Search Web"
    Verb   string     // "Executing command", "Searching web"
    Detail string     // primary parameter, whitespace-collapsed, untruncated
    Params []Param    // secondary key=value pairs, in display order
}
func Describe(name string, input json.RawMessage) Call
func Shorten(text string, maxRunes int) string
type FileChange struct { Title, Path, Old, New string; Additions, Removals, EditsApplied, EditsTotal int }
func FileChangeOf(name string, input, metadata json.RawMessage) (FileChange, bool)
```

One table (`var specs = map[string]spec{...}` plus the `mcp_browser_` prefix
rule and a generic fallback). Telegram's `run_tools.go` becomes
`toolview.Describe` + its own 180-rune shortening; the TUI working line uses
`Verb + Detail`; the generic renderer uses `Title + Detail + Params`.
`FileChangeOf` is the only reader of write/edit/multiedit metadata (C2's
metadata merge then touches one function).

In the TUI, `toolRendererFor` becomes `var renderers = map[string]toolRenderer`
of functions; glob/grep/ls fold into the generic renderer (their header comes
from `toolview`); write/edit/multiedit share one `renderFileChange`; the
permissions dialog and diff preview share one `diffPane` (mode, x-offset,
cached split/unified render). The 12 `*ToolRenderContext` types go.

### 5. Context from the daemon only

Daemon: new event `session.updated` with payload
`core.SessionUpdate{Session, Capabilities, Context ContextReport}`, published
by a coalescing per-core notifier after events that change what the model sees
(`run.updated`, terminal `tool.updated`, boundary `message.created`). One hook
in `publishEvent`, so C1's run-transition rewrite does not conflict. This also
refreshes an auto-titled session without a reload. iOS decodes unknown event
types as `.unknown`; Telegram and the voice manager ignore it.

TUI: header = `Context: ~<TokenEstimate> / <WindowTokens> · model · provider`
from the read model only. `app_usage.go` estimation,
`Config.Provider/Model/Assistant`, `normalizeAssistantProfile`, and the
`clientcmd` plumbing (`activeProviderInfo` if unused elsewhere) are deleted;
the terminal no longer imports `internal/agent/prompt` or `internal/agent/context`.

### 6. One style token set; `appModel` split

- `theme` gains the semantic tokens the chat uses (accent, secondary, fg base /
  muted / subtle, bg base / subtle / overlay, border, success, error, warning,
  info, diff colours), with today's hex values, so the rendering goldens stay
  byte-identical. `surface/styles` and `components` build only from `theme`;
  `charmtone` and `colorToHex` go. `setup/styles.go` (C4's area) already uses
  `theme` and is left untouched.
- `appModel` → `stream` (ctx/cancel, ids, read model, loading), `composer`
  (input, busy, busy mode, focus, transient notes), `dialogs` (overlay plus a
  `nav` stack of return targets replacing the two booleans, controlplane seq,
  suppressed approvals) and `status{kind, text}` replacing the dual-use `m.err`.

## Deleted

`internal/clientruntime/state.go`, `chat/viewmodel/*`, `app_snapshot.go`,
`app_usage.go` estimation, fake-event code, subagent metadata round-trip, three
tool tables and their helpers, 12 renderer structs, the second diff cache,
`charmtone` palette, `firstNonEmpty*` helpers (→ `cmp.Or`). Rough size: ~1,600
lines removed, ~900 added.

## Commit order (each green)

1. `test(terminal)`: ANSI rendering goldens (`testdata/*.golden`, `-update`
   flag) for: empty session, transcript with user/assistant/bash/edit/read
   group/web_search/subagent rows, permission dialog, todo panel, working
   status, header. Baseline before any change.
2. `feat(core)`: `session.updated` event + `daemonclient` decoder; core test.
3. `refactor(terminal)`: context usage from the daemon only.
4. `refactor`: `internal/toolview`; Telegram and the TUI working line use it.
5. `refactor(terminal)`: renderer table, file-change helper, shared diff pane.
6. `refactor(terminal)`: `readmodel` package, subagent helper; delete State/viewmodel.
7. `refactor(terminal)`: chat items reconciled by ID; layout once per frame.
8. `refactor(terminal)`: no fake events; reload only on (re)connect/switch.
9. `refactor(terminal)`: style tokens in `theme`.
10. `refactor(terminal)`: split `appModel`, `status`, dialog nav stack.
11. `docs`: ARCHITECTURE + as built.

## Risks and tests

- *Missed live updates without reloads* (e.g. state only the snapshot had):
  tests feed event sequences (send → message.created → run.updated running →
  tool.updated → approval → run.updated completed) into the app and assert the
  rendered screen; a reconnect test asserts resubscription from the request-time
  event ID.
- *Reconciliation keeping stale items*: tests that streaming updates change the
  rendered text, unchanged items keep their cache (same pointer), expanded
  state survives an event.
- *Visual regressions*: goldens from step 1 must stay identical through steps
  5–10 except where a change is intended (then the diff is reviewed and noted).
- *Daemon event cost/deadlocks*: the notifier runs off the publishing goroutine
  and coalesces; core test asserts one `session.updated` after a burst, with the
  new context estimate.
- *User-visible wording*: Telegram progress lines take the shared verbs (e.g.
  "Fetching web page" instead of "Fetching page"); the header context number
  now moves at step ends rather than per streamed token.
