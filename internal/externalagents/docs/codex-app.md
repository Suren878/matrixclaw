# Codex App-Server Connector

`codexapp` is an experimental connector for the Codex CLI app-server protocol.
It is intentionally isolated under:

```text
internal/externalagents/codexapp
```

The connector is wired into the daemon as an optional external-agent runtime.
It remains isolated from normal provider-backed assistant sessions.

## Why App-Server

Codex has several possible integration surfaces:

```text
codex exec                 one process per prompt
codex exec resume          one process per prompt, but resumes a saved thread
codex app-server           long-running JSON-RPC server
codex interactive TUI      human UI, not a stable daemon protocol
```

For matrixclaw, `codex app-server` is the cleanest option because it exposes
thread and turn methods directly:

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

It also streams notifications:

```text
thread/started
turn/started
item/agentMessage/delta
item/reasoning/textDelta
item/commandExecution/outputDelta
item/fileChange/patchUpdated
turn/diff/updated
turn/completed
error
```

## Process Model

The first implementation starts Codex over stdio:

```bash
codex app-server --listen stdio://
```

matrixclaw writes newline-delimited JSON-RPC messages to stdin and reads
newline-delimited JSON-RPC messages from stdout.

The connector does not use WebSocket yet. Stdio keeps the first bridge simple
and private to the matrixclawd process.

## Handshake

The initial handshake is:

```json
{
  "id": "1",
  "method": "initialize",
  "params": {
    "clientInfo": {
      "name": "matrixclaw",
      "title": "matrixclaw",
      "version": "0"
    },
    "capabilities": {
      "experimentalApi": true
    }
  }
}
```

Then the client sends:

```json
{
  "method": "initialized"
}
```

## Start Thread

Creating a Codex-backed matrixclaw session starts a Codex thread:

```json
{
  "id": "2",
  "method": "thread/start",
  "params": {
    "model": "gpt-5.4",
    "cwd": "/path/to/project",
    "approvalPolicy": "never",
    "sandbox": "danger-full-access"
  }
}
```

The response contains:

```text
thread.id
thread.sessionId
model
modelProvider
cwd
```

Store `thread.id` as `external_thread_id`.

## Start Turn

Each user message becomes a turn:

```json
{
  "id": "3",
  "method": "turn/start",
  "params": {
    "threadId": "thread_id_from_start",
    "input": [
      {
        "type": "text",
        "text": "Hello",
        "text_elements": []
      }
    ]
  }
}
```

The response gives the turn id. The final answer arrives through notifications.

## Resume Thread

When matrixclawd restarts, it should resume an existing Codex thread:

```json
{
  "id": "4",
  "method": "thread/resume",
  "params": {
    "threadId": "saved_external_thread_id",
    "cwd": "/path/to/project"
  }
}
```

The current app-server schema says thread resume can load by thread id, history,
or path. Prefer thread id.

## Approval Policy

matrixclaw maps the session permission mode into Codex app-server policy:

```text
full-auto    -> approvalPolicy: never,      sandbox: danger-full-access
accept-edits -> approvalPolicy: on-request, sandbox: workspace-write
default      -> approvalPolicy: on-request, sandbox: read-only
```

The Codex adapter also has a trusted-local fallback when no policy is supplied:

```text
approvalPolicy: never
sandbox: danger-full-access
```

The fallback is intentionally scoped to the optional Codex external-agent
module. The Codex app-server sandbox depends on `bubblewrap`; on some hosts it
fails before the model can even run safe commands such as `ls`.

Later stages still need:

```text
approval request notifications
matrixclaw approval UI
```

## Known Protocol Notes

The generated TypeScript schema showed `thread.status` as a string-like shape,
but the live app-server returned an object. The Go connector keeps status as
`any` to avoid brittle decoding.

The Codex app-server command is marked experimental by Codex CLI. Keep the
adapter isolated and tolerate extra fields or shape changes wherever possible.

## Current Bridge

The runtime bridge is implemented in:

```text
internal/externalagents/codexapp/runtime.go
```

It exposes the generic `externalagents.RuntimeAgent` interface:

```text
StartSession
ResumeSession
Send
Interrupt
Close
```

`Send` calls `turn/start` and converts Codex notifications into generic
external-agent events:

```text
item/agentMessage/delta -> message.delta
item/reasoning/*Delta   -> reasoning.delta
item/started            -> tool.started
item/*/outputDelta      -> tool.output.delta
item/completed          -> tool.completed
item/fileChange/*       -> diff.updated / tool output
turn/completed          -> turn.completed
closed/error stream     -> turn.failed
```

The core can execute a run for a session that has an
`external_agent_sessions` attachment. That branch lives in:

```text
internal/core/external_agent_execution.go
```

The daemon registers the Codex runtime during bootstrap. The API can list
external agents and create external-agent sessions; setup UI wiring is still
pending.
