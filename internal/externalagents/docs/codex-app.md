# Codex App-Server Connector

`codexapp` is an experimental connector for the Codex CLI app-server protocol.
It is intentionally isolated under:

```text
internal/externalagents/codexapp
```

The connector is not wired into matrixclaw runtime yet.

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

First message in a Codex-backed matrixclaw session should create a Codex thread:

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

For the first matrixclaw integration stage:

```text
approvalPolicy: never
sandbox: danger-full-access
```

This is intentionally scoped to the optional Codex external-agent module.
The Codex app-server sandbox depends on `bubblewrap`; on some hosts it fails
before the model can even run safe commands such as `ls`. MatrixClaw starts
Codex in a trusted local execution mode so the integration is usable, while
MatrixClaw's own tool approvals remain separate.

Later stages can support:

```text
on-request
workspace-write
approval request notifications
matrixclaw approval UI
```

## Tests

Normal tests use a fake in-memory JSON-RPC server:

```bash
go test ./internal/externalagents/codexapp
```

Live handshake:

```bash
MATRIXCLAW_CODEXAPP_LIVE=1 \
go test ./internal/externalagents/codexapp -run TestLiveInitialize -count=1
```

Live thread and turn:

```bash
MATRIXCLAW_CODEXAPP_LIVE_TURN=1 \
go test ./internal/externalagents/codexapp -run TestLiveThreadTurn -count=1 -v
```

The live turn sends one short message to the locally authenticated Codex
account.

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
turn/completed          -> turn.completed
closed/error stream     -> turn.failed
```

The core can execute a run for a session that has an
`external_agent_sessions` attachment. That branch lives in:

```text
internal/core/external_agent_execution.go
```

The daemon does not wire Codex into setup/session creation yet.
