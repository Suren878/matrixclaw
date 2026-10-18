# External Agents: Package Boundary

User-facing setup, permissions and limits are in
[`docs/EXTERNAL_AGENTS.md`](../../../docs/EXTERNAL_AGENTS.md). This folder is
for contributors working on the adapters.

MatrixClaw owns the session (transcript, runs, approvals, client bindings). An
adapter owns the external runtime's thread and protocol and is attached to a
session, never a source of truth. Adapters must stay removable without touching
normal assistant sessions.

## Layout

| Path | Contents |
| --- | --- |
| `internal/externalagents` | Generic contract: `RuntimeAgent`, `Permissions`, `Registry`, `Descriptor`, `Event` kinds, `AttachmentStore`, binary lookup (`binary.go`). |
| `internal/externalagents/reply` | Builds the transcript reply of a turn from its events (`reply.Apply`), with bounded tool input, output and reasoning. |
| `internal/externalagents/builtins` | The only place that names concrete adapters: `Factories()` and `BuildRegistry()` from `setup.ModulesConfig`. |
| `internal/externalagents/codexapp` | Codex app-server adapter, see [codex-app.md](codex-app.md). |
| `internal/externalagents/claudecode` | Claude Code adapter. |
| `internal/core/external_agents.go` | Session creation and the agent's thread. |
| `internal/core/external_agent_execution.go` | Runs a turn for a session with an attachment: fills the session's `Permissions`, saves the reply as it grows, ends the run. |
| `internal/store/sqlite_external_agents.go` | `external_agent_sessions` table (schema in `internal/store/migrations/`). |

## Boundary Rules

- Adapter-specific code stays in its own package (`codexapp`, `claudecode`).
  Only `builtins` imports them; core, setup, store, API and clients use the
  generic interfaces in `internal/externalagents`.
- Allowed generic touch points: the registry, the attachment store, session-kind
  routing in core, descriptor rendering in setup/modules, the new-session
  choice, and normalized event rendering.
- Shared types use generic names (`external_agent_id`, `external_thread_id`,
  `external_session_id`, `metadata_json`). Never add adapter-specific fields to
  session, run or provider types: sessions use `kind=external_agent`, and the
  adapter is chosen by `external_agent_sessions.agent_id`.
- Adapter state that has no generic field goes in `metadata_json`.
- Normal builds and tests must pass without any agent CLI installed.

## Adding an Adapter

1. Implement `externalagents.RuntimeAgent` in a new package (`Models` may
   return nil).
2. Resolve the binary with `externalagents.LookupBinary` / `NewBinaryProbe` so
   `Available` reports path, version and install state consistently.
3. Add a `Factory` with its canonical ID and aliases in `builtins/registry.go`.
4. Translate the runtime's output into the generic event kinds below.
5. Map `Permissions` (the session's permission mode and whether it is a
   read-only subagent) to the runtime's own settings. Core sets them on
   `StartSessionRequest` and on the `ExternalSession` of every turn, read from
   the session then; nothing permission-related is stored in the attachment.

## Events

Adapters emit `turn.started`, `turn.heartbeat`, `message.delta`,
`reasoning.delta`, `tool.started`, `tool.output.delta`, `tool.completed`,
`diff.updated`, and end every turn with exactly one `turn.completed` or
`turn.failed`. Core saves the `ExternalThreadID` / `ExternalSessionID` carried
by `turn.started` to the attachment, so that is where a runtime reports a new
or changed thread. `turn.heartbeat` only records activity. There is no approval
event: adapters must not block waiting for a user decision.

## Claude Code Notes

Each turn runs `claude -p --output-format stream-json --verbose
--include-partial-messages [--resume <session>] [--model <m>]
--permission-mode <mode> -- <text>` and parses the stream-json output.
`StartSession` spawns nothing; the Claude session ID reported on the first turn
is saved to the attachment and passed to `--resume` afterwards.
`bypassPermissions` is never used because Claude refuses it when the daemon runs
as root; `full_auto` maps to `auto` instead. `Interrupt` does nothing: the
canceled turn context kills the CLI.
