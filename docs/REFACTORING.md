# MatrixClaw — Stability & Refactoring Plan

See also: [docs/refactoring/2026-06-10-modular-architecture-plan.md](refactoring/2026-06-10-modular-architecture-plan.md)
(modularity roadmap) and [docs/refactoring/2026-06-10-core-refactoring-plan.md](refactoring/2026-06-10-core-refactoring-plan.md)
(core decomposition, executed 2026-06-12).

Status snapshot (audit date 2026-05-29, updated 2026-06-16):

- `go build ./...` — required by CI
- `go vet ./...` — required by CI
- Legacy `*_test.go` files were removed on 2026-06-16. New tests should follow
  `docs/TESTING.md`: acceptance/use-case coverage around daemon-visible
  behavior, provider boundaries, client delivery, and persistence.
- `gofmt -l .` — clean (web tool files formatted during the webtools extraction)

The architecture is sound (daemon `matrixclawd` + HTTP API, thin clients via
`internal/daemonclient`, agent loop in `internal/core`, pluggable LLM providers
behind an interface, pure-Go SQLite in `internal/store`). **No rewrite is
needed.** The work is about runtime resilience and closing test gaps.

This plan is ordered to fix stability first, then quality. Each phase is
independent and verifiable with `go build ./...` + `go vet ./...`; add
acceptance/use-case tests from `docs/TESTING.md` when rebuilding coverage.

---

## Phase 0 — Safety net

- [x] Format the 2 `internal/tools` files (done during the webtools extraction).
- [x] Wire `gofmt -l` into CI.
- [x] CI now runs `go build ./...` + `go vet ./...` without the removed legacy
      test suite.

## Phase 1 — Crash resistance (highest leverage) 🔴

- [x] Add `internal/safego` — `Go`/`Run` helpers that recover panics in
      background goroutines and log a stack trace instead of crashing the daemon.
- [ ] Wrap all 12 background goroutines with `safego.Go`:
  - `internal/daemoncmd/supervisor.go:111,168`
  - `internal/daemoncmd/run.go:138`
  - `internal/daemoncmd/client_registry.go:104`
  - `internal/orchestration/stub.go:29`
  - `internal/core/events.go:88`
  - `internal/tools/shell.go:310`
  - `internal/externalagents/codexapp/client.go:171`
  - `internal/modules/localruntime/{piper,whisper,supertonic}_process.go`
  - `clients/telegram/monitor_state.go:47`
- [x] `internal/core` audit (2026-06-12): every core background goroutine runs
      under `safego.Go`, including the subagent terminal-wait resume path
      (`internal/core/subagents.go`), and none captures a request-scoped ctx
      for work that outlives the request.
- [x] `internal/providers/registry.go:44,53,68` — `registerProvider` panics:
      kept intentionally. Verified all 7 callers are `init()`-time registration
      of static built-in provider specs (`provider_openai.go`,
      `provider_anthropic.go`, `provider_gemini.go`, `provider_qwen.go`, and the
      static slice in `provider_openai_compatible_gateways.go`). This is the Go
      `Must`-idiom for compile-time invariants — a panic here is a build-the-
      binary-wrong programmer error, not a runtime/user-input failure. Custom
      user providers flow through `controlplane/provider_custom.go`, a separate
      runtime path that does not call `registerProvider`.

## Phase 2 — Agent loop & cancellation 🟠

- [x] `internal/core/execution_provider.go` `buildProviderConversation` — assessed:
      it passes a nil `AttachmentReader`, so no attachment IO runs, the returned
      error is structurally nil, and `context.Background()` is unused. Documented
      the contract instead of churning; request paths already use the ctx-aware
      `(c *Core).buildProviderConversation` which threads ctx and returns errors.
- [x] `internal/orchestration/stub.go` — fire-and-forget run now logs its error
      (done in Phase 1). `context.Background()` is intentional: the run must
      outlive the returning `StartRun` request, so it is not bound to that ctx.
- [x] `internal/core/subagents.go` — parent auto-resume now waits on the core
      event bus with a bounded context instead of polling child run status every
      250 ms forever.
- [x] `internal/core/execution_request.go` — voice/document delivery checks now
      read explicit client capabilities from runs/session inputs instead of
      hardcoding client names in core.
- [ ] Audit the remaining `context.Background()` uses inside request paths.

## Phase 3 — Concurrency hardening 🟠

- [x] Documented the construct-time-only contract on the `Core.With...` setters
      (`internal/core/core.go`): except `WithSessionLLMs`, they mutate fields
      without `c.mu` and must not be called after a run starts / the Core is
      shared; post-construction mutation goes through the locked `Set*` methods.
- [x] Keep and document `SetMaxOpenConns(1)` for the main, work, automation, and
      skills SQLite stores as the intentional personal-daemon serialization
      contract.

## Phase 4 — Rebuild tests around user-visible behavior 🟡

- [ ] Add acceptance/use-case tests for session lifecycle, model turns,
      approvals, client delivery, subagent flow, persistence, and provider
      errors.
- [ ] Add provider-boundary tests for `internal/providers/ai/*` with mock HTTP,
      covering request shape, streaming, tool calls, auth headers, and error
      mapping.
- [ ] Add scenario tests for `internal/automation` and `internal/api` where they
      participate in user-visible workflows.
- [x] Remove silent error-swallowing in `internal/automation/service.go`
      (`Tick`, failed-fire update/advance, and delivery creation errors are now
      logged or returned).

## Phase 5 — Decompose god files 🟡

- [x] `internal/core/execution_provider.go` → split into `execution_request.go`,
      `execution_prompts.go`, `execution_conversation.go` (2026-06-12).
- [x] Split `internal/core/types.go` by aggregate → 11 `types_*.go` domain
      files (2026-06-12).
- [x] Split `internal/core/context.go` → `context_report.go`,
      `context_compact.go`, `context_markers.go` (2026-06-12).
- [x] Split `internal/core/subagents_async.go` → `subagents_async_tools.go`,
      `subagents_lifecycle.go`, `subagents_worktree.go` (2026-06-12).
- [x] Extract web tools into `internal/webtools`; `internal/tools` is now a
      leaf package (only depends on `internal/safego`), and `internal/core`
      no longer depends on `internal/webresearch` (2026-06-12).
- [ ] Split `internal/controlplane` voice handlers (~2,100 LOC across
      `modules_voice*.go`) by feature.
- [x] Remove the stray `tmpverify/` scratch dir from the tree (already gitignored).
