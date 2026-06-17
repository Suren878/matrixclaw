# External Agents

External agents are optional runtimes attached to MatrixClaw sessions. They are
not normal LLM providers: MatrixClaw still owns the product session, local
history, client handoff, approvals, deliveries, and normalized event display.
The external runtime owns its own process or thread protocol.

Current built-in adapters:

- Codex app-server, detected from the `codex` binary.
- Claude Code CLI, detected from the `claude` binary.

Manage adapters from:

```text
/modules agents
matrixclaw agents
```

Enabled adapters appear in the new-session picker and can also be used as
external subagent runtimes by `delegate_task`.

## Session Model

MatrixClaw stores external-agent sessions with a generic session kind and a
separate attachment:

```text
sessions.kind       = external_agent
sessions.runtime_id = external_agent

external_agent_sessions
  session_id
  agent_id
  external_thread_id
  external_session_id
  cwd
  model
  metadata_json
```

The concrete adapter is selected by `external_agent_sessions.agent_id`.
Canonical built-in IDs are `codex-app` and `claude-code`; user-facing aliases
such as `codex` and `claude` are accepted at command/API boundaries.

New code should create external-agent sessions with `runtime_id:
"external_agent"` and `external_agent_id` set to the adapter ID or alias.
This keeps adapter details out of the main session table and lets the daemon
resume or detach external runtimes without changing normal assistant sessions.

## Adapter Boundary

The generic runtime interface is implemented under `internal/externalagents`.
Adapter-specific protocol code stays in its own package, for example
`internal/externalagents/codexapp`.

Adapters provide:

- availability and descriptor metadata for setup and UI.
- `StartSession` and `ResumeSession` for external runtime threads.
- `Send`, `Interrupt`, and `Close` for turn execution.
- normalized event streams for MatrixClaw clients.

The daemon composition root registers built-in adapters through
`internal/externalagents/builtins`. Core runtime packages should depend on the
generic external-agent interfaces, not on a specific adapter package.

## Event Normalization

External runtime events are translated into MatrixClaw event kinds such as:

```text
message.delta
reasoning.delta
tool.started
tool.output.delta
tool.completed
diff.updated
turn.started
turn.completed
turn.failed
```

Clients render these normalized events the same way they render normal
MatrixClaw run events. Adapter raw protocol payloads may be kept for
diagnostics, but UI and core code should not depend on them.

## Codex App-Server

The Codex adapter starts Codex over stdio:

```bash
codex app-server --listen stdio://
```

The connector speaks newline-delimited JSON-RPC. It initializes the server, then
uses thread and turn methods such as:

```text
thread/start
thread/resume
turn/start
turn/steer
turn/interrupt
thread/read
thread/list
model/list
```

MatrixClaw stores the Codex thread ID as `external_thread_id`. On daemon restart
the adapter resumes by thread ID and continues routing normalized notifications
back through the owning MatrixClaw session.

Codex permission settings are mapped from the MatrixClaw session permission
mode:

```text
full-auto     -> approvalPolicy: never,      sandbox: danger-full-access
accept-edits  -> approvalPolicy: on-request, sandbox: workspace-write
default       -> approvalPolicy: on-request, sandbox: read-only
```

## Subagents

MatrixClaw assistant sessions receive a `delegate_task` tool for bounded child
work. Child sessions are hidden from the normal session list, receive an
isolated prompt built from the delegated goal/context, and return a compact
summary to the parent run.

Allowed runtimes are:

```text
matrixclaw
codex
claude
auto
```

`auto` defaults to the native MatrixClaw child runtime unless enabled external
runtimes make another choice explicit. External-agent sessions cannot delegate
again.

For implementation details and removal boundaries, see
`internal/externalagents/docs/`.
