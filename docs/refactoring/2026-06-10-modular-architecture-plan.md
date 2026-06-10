# MatrixClaw Modular Architecture Refactoring Plan

> **For agentic workers:** This is a master roadmap. Each phase must get its own
> detailed implementation plan (superpowers:writing-plans) before execution —
> do not execute phases directly from this document. Phases are ordered by
> dependency; do not start Phase N+1 while Phase N acceptance criteria fail.

**Goal:** Make MatrixClaw a genuinely modular system where adding a feature
(module, tool, provider, subagent runtime) means writing one package and one
registration line — instead of editing 8 packages.

**Architecture:** Keep the daemon-first design (it is correct). Decompose the
`core.Core` god object into domain services behind the existing store ports,
replace the anemic `modules.Module` interface with a capability-based plugin
contract, and collapse the hand-mirrored HTTP transport (api ↔ daemonclient)
into a single shared schema.

**Tech stack:** Go 1.26, SQLite (modernc), bubbletea v2 TUI, hand-rolled HTTP
API. No new runtime dependencies are required by this plan.

**Audit date:** 2026-06-10, commit `cc37b5c`, ~101 500 lines of Go in the main
tree (excluding `.worktrees/`, `tmpverify/`).

Related: `docs/REFACTORING.md` (2026-05-29 stability plan) stays valid — its
unfinished items are absorbed into Phase 0 here.

---

## 1. Current state assessment

### What is already good — keep it

- **Daemon-first split.** `cmd/matrixclaw` (15 lines) and `cmd/matrixclawd`
  (13 lines) are thin; wiring lives in `internal/daemoncmd` / `internal/clientcmd`.
  Clients render daemon state. This is the right shape — do not touch it.
- **Store ports.** `internal/core/ports.go` already defines narrow store
  interfaces (`SessionStore`, `RunStore`, `SubagentTaskStore`, …) implemented by
  `internal/store`. The hexagonal foundation exists.
- **Tool registry.** `internal/tools/registry.go` has specs, policies,
  duplicate detection. Good extension point.
- **External agents registry.** `internal/externalagents` + `builtins.BuildRegistry`
  is already a pluggable pattern.
- **Command catalog.** `internal/commandcatalog` separates command IDs from UI.

### Problem 1 — Anemic module system, feature fan-out (the core issue)

`internal/modules/registry.go` defines:

```go
type Module interface {
    ID() string
    Name() string
    RegisterTools(*tools.Registry) error
    Context() string
}
```

Tools and prompt context are the *only* things a module can contribute.
Everything else a real feature needs is hardcoded across the tree. Measured
fan-out of the **voice** feature (files mentioning voice, non-test):

| Package | Files touched |
|---|---|
| `internal/modules/localruntime` | 15 |
| `internal/controlplane` | 14 (`modules_voice*.go`, ~4 000 lines) |
| `clients/telegram` | 6 |
| `internal/setup` | 3 |
| `internal/api` | 3 |
| `internal/daemonclient` | 3 |
| `internal/daemoncmd` | 2 |
| `internal/core` | 1 |

Adding the next module of voice's size requires edits in ~8 packages. This is
why "adding functions easily" feels impossible today. Same pattern for storage,
mcp, skills, web_search — each has its own `internal/api/module_*.go`,
`internal/controlplane/modules_*.go`, `internal/daemonclient/*.go` triplet.

### Problem 2 — `core.Core` god object

One struct (`internal/core/core.go`) with one `sync.RWMutex` and ~50 method
files: sessions, runs, execution, subagents, memory, plans, approvals,
permissions, usage, search, deliveries, bindings. 10 684 lines of code.
Hotspots:

- `subagents_async.go` — 1 042 lines
- `execution_provider.go` — 957 lines
- `context.go` — 859 lines
- `types.go` — 689 lines (every domain type in one bag)

Consequences: any change recompiles/retests everything, the mutex couples
unrelated subsystems, and "rewriting a part" means surgery inside one package.

### Problem 3 — Dependency direction leaks out of core

`internal/core` imports **concrete** packages: `tools`, `providers`,
`externalagents`, `webresearch`, `work`. The ports file itself
(`ports.go`) imports `providers` and `tools`. So the domain layer drags every
implementation with it — core cannot be compiled or tested in isolation, and
swapping an implementation means touching core.

### Problem 4 — Hand-mirrored transport (api ↔ daemonclient ↔ runtime interfaces)

Every daemon endpoint exists in four places, all written by hand:

1. handler in `internal/api/*.go` (35 files)
2. client method in `internal/daemonclient/*.go` (20 files)
3. interface in `internal/clientruntime/controlplane_runtime.go` (694 lines)
4. another interface in `internal/controlplane/dispatcher.go`
   (`SessionRuntime`, `VoiceModuleRuntime`, `BrowserModuleRuntime`, … ~15
   interfaces)

Request/response types are partially duplicated between api and daemonclient.
Adding one endpoint = 4 synchronized edits and zero compile-time guarantee that
client and server agree.

### Problem 5 — `internal/controlplane` is a UI framework + all feature logic (11 083 lines)

It contains a generic picker/command framework (`picker_*.go`, `dispatcher.go`,
`view.go`) **and** every feature's command logic (`modules_voice_*` — 9 files,
`provider_custom_*` — 7 files, storage, tasks, sessions…). The framework is
reusable; the feature logic belongs to the features.

### Problem 6 — `internal/setup` mixes four responsibilities (8 741 lines)

Config schema + file store, provider form UI logic (`provider_form*.go`),
process/systemd management (`runtime_*.go`), and Telegram validation. Modules'
config types also live here (`voice_modules.go`, `web_search.go`, `mcp.go`),
which is part of the fan-out problem: a module's config can't live with the
module.

### Problem 7 — Test coverage is inverted relative to risk

| Package | Code lines | Test lines |
|---|---|---|
| `internal/providers` | 5 620 | **0** |
| `internal/work` | 619 | **0** |
| `internal/tools` | 4 110 | 99 |
| `internal/setup` | 5 504 | 195 |
| `internal/core` | 10 684 | 2 483 |
| `internal/controlplane` | 11 083 | 2 095 |

The provider adapters (network IO, streaming, auth) and shell/file tools — the
most fragile, most security-sensitive code — have effectively no tests. Any
refactoring there today is blind.

### Problem 8 — Working-tree and process hygiene

- 4 stale worktrees in `.worktrees/` (`browser-module`, `subagents-v2`,
  `refactor-terminal-ui-stack`, `stabilize-runtime-stability`) with diverged
  full copies of the tree.
- Uncommitted changes in `clients/telegram` (incl. deleted `live_monitor.go`)
  and 3 unpushed commits on `main`.
- No `.golangci.yml` (CI runs golangci-lint with `only-new-issues` and default
  config — weak baseline). CI has no `-race`, no coverage tracking, no
  `gofmt -l` gate.
- `CONTRIBUTING.md` is 25 lines; there is no `ARCHITECTURE.md`; module authors
  have no guide.

---

## 2. Target architecture

```
cmd/matrixclawd ─→ daemoncmd (wiring only)
                      │ registers
                      ▼
              ┌─ module host ──────────────────────────────┐
              │  capability discovery via type assertions:  │
              │  Tools · Routes · Commands · Config ·       │
              │  Lifecycle · PromptContext                  │
              └──────┬──────────────────────────────────────┘
                     │ contributes into
        ┌────────────┼─────────────┬───────────────┐
        ▼            ▼             ▼               ▼
   tools.Registry  api mux   controlplane     setup config
                              catalog          sections
                     ▲
                     │ calls
              core services (session / execution / subagent /
              memory / plan / approval) — depend only on ports
                     │
                     ▼
              internal/store (SQLite adapters)

clients (terminal TUI, telegram, future) ─→ daemonclient (generated
from shared apischema) ─→ HTTP API
```

Definition of done for the whole plan — **the modularity test**: a new module
(e.g. a hypothetical `weather` module) with a tool, a config section, a
`/modules` panel, and an HTTP endpoint is implemented as **one package under
`internal/modules/weather` plus one line in `daemoncmd`**, with zero edits to
`api`, `controlplane`, `daemonclient`, or `setup`.

---

## 3. Working rules for every phase (the advice)

1. **Strangler fig, not big bang.** Old and new paths coexist; the old path is
   deleted only after the new one carries all traffic. Never branch the whole
   repo for a months-long rewrite — the 4 stale worktrees show how that ends.
2. **Characterization tests before moving code.** When code lacks tests
   (providers, tools, setup), first pin current behavior with golden/httptest
   fixtures, then refactor. The test must pass before *and* after.
3. **Mechanical vs. semantic commits.** A commit either moves/renames code with
   zero behavior change, or changes behavior — never both. Reviewers (and
   future you) can skim mechanical commits and scrutinize semantic ones.
4. **Small PRs against `main`.** Target ≤ ~400 lines of meaningful diff. Each
   PR leaves `go build ./... && go vet ./... && go test ./...` green and the
   daemon runnable. Release cadence continues throughout.
5. **Compatibility freeze on the wire.** The daemon HTTP API and the SQLite
   schema are public interfaces (Telegram client, possible external users).
   During refactoring: additive API changes only; schema changes only through
   `internal/store/migrations`.
6. **One pilot first.** For every systemic change (module capabilities,
   transport schema), migrate exactly one feature end-to-end, get the pattern
   reviewed, then batch-migrate the rest. Voice is the designated pilot — it
   has the worst fan-out, so it proves the most.
7. **Interfaces are defined by the consumer.** Core defines the ports it needs;
   modules implement capabilities the host asserts. Avoid "util" interfaces
   defined next to implementations.
8. **Each phase gets its own detailed plan** (`docs/refactoring/` +
   superpowers:writing-plans format with TDD steps) before any code is written.
   This document only fixes scope, order, and acceptance criteria.

---

## 4. Phases

### Phase 0 — Hygiene and safety net (prerequisite for everything)

**Scope:**

- Resolve the dirty working tree: review uncommitted `clients/telegram`
  changes (commit or discard deliberately — `live_monitor.go` deletion looks
  intentional but verify), push the 3 local commits.
- Triage the 4 worktrees: extract anything worth keeping into small PRs
  (`refactor-terminal-ui-stack` and `subagents-v2` likely overlap with this
  plan — mine them for ideas, then delete), prune `.worktrees/` and stale
  branches. Remove `tmpverify/` from the tree, gitignore it.
- Add a real `.golangci.yml` (enable: `govet`, `errcheck`, `staticcheck`,
  `revive`, `gocyclo` warn-only, `misspell`); fix or `nolint`-annotate the
  existing findings so CI can drop `only-new-issues`.
- CI (`.github/workflows/test.yml`): add `gofmt -l` gate,
  `go test -race ./...`, and a coverage report step (upload artifact or
  Codecov) to make the Phase 1 coverage gains visible.
- Finish the open items of `docs/REFACTORING.md`: wrap the remaining background
  goroutines with `safego.Go`, finish the `context.Background()` audit.

**Acceptance:** clean `git status`, ≤ 1 worktree, CI green with race + lint on
full config, coverage number published per package.

**Effort:** ~1 week of calendar time, low risk.

### Phase 1 — Characterization tests for refactoring targets

**Scope (test-only, no production changes):**

- `internal/providers/ai/*`: one shared **conformance suite** driven by
  `httptest.Server` golden fixtures (request shape, streaming chunks, tool
  calls, auth headers, error mapping) — instantiated for `openaicompat`,
  `anthropiccompat`, `gemini`, `openaicodex`. This suite later becomes the
  contract every new provider must pass.
- `internal/tools`: table tests for `registry.go` policy resolution; sandboxed
  tests for `shell.go` (timeouts, output truncation, cancellation) and the
  file tools (path escapes, size limits). These are security-relevant.
- `internal/setup`: config load/save/migrate round-trip tests with fixture
  JSON files; `runtime_env.go` parsing tests.
- `internal/work`: SQLite store CRUD tests (currently 0).

**Acceptance:** `providers` ≥ 60 % stmt coverage, `tools` ≥ 60 %, `setup`
core config paths ≥ 70 %, `work` ≥ 70 %. No production diffs in this phase.

**Effort:** 1–2 weeks. Zero user-facing risk; pays for itself in every later phase.

### Phase 2 — Core decomposition

**Scope:**

- **Split `types.go`** (689 lines) into domain files colocated with their
  logic: `session_types.go`, `run_types.go`, `message_types.go`, etc.
  Mechanical commit.
- **Extract domain services.** Introduce unexported services owned by `Core`,
  each with its own narrow dependencies and, where needed, its own lock:
  `sessionService`, `executionService` (runs + provider turns),
  `subagentService`, `memoryService`, `planService`, `approvalService`.
  `Core` stays as the public facade (its method set and behavior do not
  change), delegating to services — strangler style. The single `Core.mu`
  is dissolved into per-service locks last, guarded by `-race` CI.
- **Fix dependency direction.** Remove `providers` and `tools` imports from
  `core/ports.go`: core declares the minimal interfaces it consumes
  (`ToolExecutor` already exists; add an LLM-turn port mirroring what
  `sessionllm` provides) and `daemoncmd` adapts concrete packages to them.
  `webresearch` and `externalagents` references move behind the same pattern.
  Result: `go list -deps ./internal/core | grep matrixclaw` shows only
  `core`, `safego`, `version` (and `work` until Phase 4 absorbs it).
- **Split god files** along the new service seams: `subagents_async.go`
  (1 042) → task lifecycle / approval bridging / parent-resume;
  `execution_provider.go` (957) → conversation building / turn loop /
  usage accounting; `context.go` (859) → assembly vs. trimming.

**Acceptance:** no public API change (`internal/api` and clients compile
untouched); `internal/core` imports no concrete sibling implementations; no
file in `internal/core` over ~500 lines; existing core tests pass unmodified.

**Effort:** 2–3 weeks. Medium risk — mitigated by Phase 1 tests, `-race`, and
mechanical/semantic commit discipline.

### Phase 3 — Capability-based module system (the centerpiece)

**Scope:**

- Extend the module contract with **optional capabilities** discovered via
  type assertion (Go's idiomatic plugin pattern — cf. `http.Pusher`,
  `fs.ReadDirFile`):

```go
package modules

// Module stays minimal; everything else is optional.
type Module interface {
    ID() string
    Name() string
}

type ToolProvider interface{ RegisterTools(*tools.Registry) error }
type PromptContextProvider interface{ Context() string }
type RouteProvider interface{ Routes() []api.Route }          // mounted under /v1/modules/<id>/
type CommandProvider interface{ Commands() []command.Descriptor } // pickers/panels for /modules
type Configurable interface {
    ConfigSection() config.Section   // schema + defaults, stored under modules.<id>
    ApplyConfig(raw json.RawMessage) error
}
type Lifecycle interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Health(ctx context.Context) HealthStatus
}
```

- Build the **module host** in `daemoncmd`: iterates registered modules,
  asserts capabilities, and wires each into `tools.Registry`, the api mux,
  the controlplane catalog, and the setup config. `command.Descriptor` is the
  hard part: design it from the existing picker framework primitives
  (`PickerKind`, options, forms in `controlplane/picker_*.go`) so module
  panels are *data*, not code in controlplane.
- **Pilot: migrate voice end-to-end.** Move `internal/controlplane/modules_voice_*`
  (9 files), `internal/api/module_voice.go`, `internal/setup/voice_modules.go`
  and the voice parts of `daemonclient` behind the capabilities of
  `internal/modules/voice` (+ `localruntime` as its internal engine).
- Batch-migrate: storage, mcp, skills, web_search/webresearch, delivery.
- `internal/setup` shrinks to: daemon/config core, provider configs, client
  bootstrap. Module config sections move to module packages.

**Acceptance:** the modularity test from §2 passes (prove it by writing a
throwaway `internal/modules/example` in a PR and deleting it after review);
`controlplane` loses all `modules_*` feature files; `api` loses `module_*.go`;
adding voice-sized functionality touches 1 package + 1 line.

**Effort:** 3–4 weeks. Highest design risk — the `command.Descriptor` design
review is the gate; do not start batch migration before the voice pilot is
merged and exercised from both TUI and Telegram.

### Phase 4 — Transport unification

**Scope:**

- Create `internal/apischema`: all request/response DTOs shared by server and
  client, one file per resource. `internal/api` handlers and
  `internal/daemonclient` both consume it — compile-time agreement.
- Make `daemonclient` thin and table-driven (method = path + verb + DTO pair);
  module endpoints from Phase 3 get a generic
  `client.Module(id).Call(route, in, out)` so daemonclient never grows
  per-module files again.
- Collapse the interface zoo: `internal/clientruntime` 694-line runtime +
  ~15 single-implementation interfaces in `controlplane/dispatcher.go` become
  one `Runtime` per bounded area (sessions, runs, modules, providers,
  automation). Delete interfaces with a single caller and a single
  implementation — they are indirection without seams (YAGNI).
- Absorb `internal/work` store behind core ports (leftover from Phase 2).

**Acceptance:** adding an endpoint = DTO in `apischema` + handler + one client
table row (2–3 edits, type-checked); no per-module files in `daemonclient`;
`clientruntime` < 300 lines.

**Effort:** 1–2 weeks, mostly mechanical after Phase 3.

### Phase 5 — Controlplane slimming

**Scope:**

- Extract the generic picker/command framework into
  `internal/controlplane/framework` (or `internal/cmdui`): dispatcher, picker
  builder/view/validation, presentation text, result types.
- Remaining feature commands that are not module-owned (sessions, providers,
  plan, memory, tasks, automation) become self-registering command packages
  using the same `command.Descriptor` as modules — controlplane core stops
  knowing feature names.
- Split `provider_custom_*` (7 files) into a `providersetup` command package.

**Acceptance:** `internal/controlplane` (framework + dispatcher) < 3 000
lines; `commandcatalog` no longer needs editing to add a module command;
TUI and Telegram render module panels identically through the framework.

**Effort:** 2 weeks.

### Phase 6 — Subagent and provider extensibility

**Scope:**

- **Unify subagent runtimes with external agents.** Today
  `core.SubagentRuntime` normalization (`subagents.go:586`) hardcodes
  matrixclaw/codex/claude-code, while `externalagents.Registry` is already
  pluggable. Define one `SubagentRuntime` port in core (spawn, prompt, status,
  approval bridge, summary) and register implementations — including future
  ones (e.g. arbitrary CLI agents) — the way `builtins.BuildRegistry` does.
- Subagent definitions as data: per-project/per-user agent presets (name,
  runtime, model, system prompt, tool policy) loaded from config — the
  "make subagents easily" feature falls out of the port + `Configurable`
  capability.
- **Provider conformance:** the Phase 1 suite becomes the documented contract;
  `internal/providers/factory` is the single place mapping provider type →
  adapter; adding a provider = adapter package + factory row + passing suite.

**Acceptance:** a new subagent runtime is one package implementing the port +
one registration; a new provider is one adapter passing the conformance suite;
`subagents_*.go` no longer contains runtime-specific branches.

**Effort:** 2 weeks. Builds directly on Phases 2–3.

### Phase 7 — Open-source polish

**Scope:**

- `docs/ARCHITECTURE.md`: the §2 diagram, package map, dependency rules
  (enforce with a `go list`-based test so the rules can't rot).
- `docs/MODULES.md`: module author guide with the `example` module from
  Phase 3 as a tutorial.
- Expand `CONTRIBUTING.md`: PR size rules, mechanical/semantic commit
  convention, test expectations, how to run the conformance suites.
- Issue/PR templates, `good first issue` seeding (module ideas, provider
  adapters — these become parallelizable for contributors precisely because
  of Phases 3 and 6).
- Godoc pass on exported surfaces of `core`, `modules`, `apischema`, `tools`.

**Acceptance:** an outside contributor can build a toy module following docs
alone; dependency-rule test in CI.

**Effort:** 1 week, parallelizable with Phase 6.

---

## 5. Sequencing and effort summary

| Phase | Theme | Effort | Risk | Unblocks |
|---|---|---|---|---|
| 0 | Hygiene, CI, worktrees | ~1 wk | low | everything |
| 1 | Characterization tests | 1–2 wk | none | 2, 3, 6 |
| 2 | Core decomposition | 2–3 wk | medium | 3, 6 |
| 3 | Module capabilities + voice pilot | 3–4 wk | high (design) | 4, 5, 6 |
| 4 | Transport unification | 1–2 wk | low | 5 |
| 5 | Controlplane slimming | 2 wk | low | — |
| 6 | Subagents/providers extensibility | 2 wk | medium | contributors |
| 7 | Docs and onboarding | 1 wk | none | contributors |

Total ≈ 3–4 calendar months at part-time pace. Phases 0–1 can start today;
Phase 3's `command.Descriptor` design deserves a brainstorming session before
its detailed plan is written.

## 6. What this plan deliberately does NOT do

- **No rewrite of the TUI stack** (`clients/terminal/ui/surface` is large but
  isolated and works; the abandoned `refactor-terminal-ui-stack` worktree
  suggests this rabbit hole was already visited).
- **No microservices, no plugin .so loading, no embedded scripting.** Go
  compile-time modules with capability interfaces give the needed
  extensibility at zero operational cost.
- **No ORM / framework adoption.** The hand-written store with migrations is
  fine; transport stays hand-rolled-but-shared rather than OpenAPI-generated
  (revisit only if external API consumers appear).
- **No breaking the daemon HTTP API or DB schema** without a migration.
