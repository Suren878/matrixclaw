# Audit cleanup (2026-10-03)

After the long-running-agent refactor, a read-only audit of the whole codebase
(five areas) listed bugs, dead code, legacy and simplification candidates. The
full reports live outside the repo (session scratchpad `audit/*.md`); this file
fixes what we do with them.

## Rules

- Delete, don't deprecate: no shims, aliases, compat branches or dead code.
- Code that translates data a user already has on disk (SQLite schema
  migrations in `internal/store`, read-time rewrites of old `setup.json` keys
  and values) stays until v0.2.0 is released; it is removed in phase D. Aliases
  and fallbacks for anything that is not persisted (in-memory tokens, request
  parameters, model-facing input) go now.
- Any change to an HTTP/JSON contract is checked against every client:
  `clients/terminal`, `clients/telegram`, `internal/daemonclient`,
  `internal/telephony/gateway` and the Swift app in `clients/ios`.
- Doc comments <= 4 lines, no history; tests only on observable behaviour.
- Each commit passes `gofmt`, `go vet ./...`, `go test ./...`.

## Phase A + B: bugs, dead code, legacy (one worker per area) — done

Item numbers refer to the area's audit report.

1. **core + store + agent** (`audit/core-engine.md`): bugs 7; dead/legacy 3, 8,
   9, 11, 18, 23; simplify 2 (drop `internal/orchestration` and go-workflows),
   5 (run-scoped queries and indexes), 6 (one way to seal an assistant turn
   and end a run), 13 (one accepted-run constructor), 19 (`Terminal()`
   methods), 12 (cancel via context cause) if it stays small.
2. **api + controlplane + daemon** (`audit/controlplane-api.md`): bugs 1, 2, 3,
   4 (with core report item 1: locked external-agent swap), 5, 21; dead 7, 16,
   20, 22; simplify 10, 13, 14, 17.
3. **setup + modules + telegram + telephony** (`audit/telegram-modules-setup.md`):
   bug 1 is fixed (bd8f24a); bugs 2, 3, 8, 12; legacy 4 (persist only the
   user's own system prompt), 9 (write-only keys and forced flags only);
   dead 10, 19; simplify 11, 13, 14, 16, 17 (inside the area), 18, 20, 22.
4. **providers + tools + web + external agents + skills + automation**
   (`audit/providers-tools.md`): bugs 4, 6, 7, 14, 23, 5 (one typed provider
   error and one retry loop); legacy/dead 1, 8, 9, 11, 12, 13, 15, 16, 19
   (dead states and column only), 21; simplify 10, 20, 24.
5. **terminal** (`audit/terminal.md`): bugs 1, 2, 3, 20; dead 5, 6, 7, 8, 9, 10,
   18, 21; simplify 16.

## Phase C: structural rewrites — done

Each item has a design note with an as-built section in
`docs/superpowers/specs/2026-10-03-*.md`.

1. One run state machine in core (`transition(run, to, cause)`), one task model
   for shell and subagent tasks, child results and approvals delivered through
   the engine inbox, one recovery entry point (core report 4, 14, 15, 16, 17).
2. Tool contract: core alone decides approval from the Spec, executors offer
   an optional preview, `Result.Status` is the only status field, one file
   change metadata type (providers report 2, 17, 18).
3. Web stack: `web_search` + `web_fetch` returning bounded markdown; research
   becomes a read-only `agent` child; `internal/webresearch` and the `work`
   store go (providers report 3).
4. Setup as one typed config behind `Service.Update`, no `Draft` outside the
   wizard, form specs out of the persistence package (setup report 6).
5. Module lifecycle (`Tools/Context/Status/Apply/Close`) owned by the daemon,
   one process supervisor for local runtimes, realtime voice providers as
   codecs over a shared websocket session (setup report 5, 7, 11, 15, 21).
6. Controlplane talks to one daemon port; module settings rendered from
   daemon-provided descriptors; one picker view model (controlplane report 6,
   8, 9); API on Go 1.22 mux patterns (11).
7. Terminal: one event-sourced read model updated in place, a shared tool
   presentation package with Telegram, context usage from the daemon only
   (terminal report 4, 11-15, 17, 19, 22).
8. Systemic: one `FirstNonEmpty`/`cmp.Or`, trimming only at the API boundary,
   one row scanner type in the store.

## Phase D: after v0.2.0 is released

Collapse `internal/store` migrations into a canonical `001_init.sql` with
`PRAGMA user_version`; drop read-time `setup.json` rewrites and persisted-value
aliases (session runtime `codex`, web search `api_key`, browser provider and
runtime-mode aliases).
